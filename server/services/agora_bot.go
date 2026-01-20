package services

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	agoraservice "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
)

// AgoraBot subscribes to audio and forwards to Anam WebSocket
// Supports dual-subscription: always subscribed to primary (original user),
// optionally subscribed to translation (Palabra) with priority when active
type AgoraBot struct {
	appID         string
	channel       string
	botUID        string // UID 4000+ (Anam avatar)
	token         string
	primaryUID    string // Original user UID - always subscribed, never unsubscribe
	translationUID string // Palabra UID when translation active, empty otherwise
	translationActive bool // True when we've received audio from translationUID
	anamClient    *AnamClient
	conn          *agoraservice.RtcConnection
	stopChan      chan struct{}
	targetLeftChan chan struct{} // Signals when primary UID leaves channel
	isConnected   bool
	isSpeaking    bool   // Track if currently sending speech to Anam
	silenceFrames int    // Count consecutive silent frames (for voice_end)
	frameCount    int    // Total frames forwarded (for logging)
	pcmFile       *os.File // Debug: record PCM audio for Audacity

	// Voice Activity Detection (VAD) state
	audioBuffer   [][]byte // Ring buffer for pre-roll (stores last 10 frames = ~100ms)
	bufferIndex   int      // Current position in ring buffer
	rmsThreshold  int64    // RMS threshold for voice detection (default: 100)
	speechFrames  int      // Count frames above threshold before triggering speech
	sendingAudio  bool     // Currently sending audio to Anam

	// Idle detection
	lastAudioTime time.Time // Time when audio was last forwarded to Anam

	// VAD bypass for initial audio flow
	streamStartTime time.Time // When streaming started - bypass VAD for first few seconds
}

// VAD bypass duration - send all audio for this long after starting/switching
const vadBypassDuration = 3 * time.Second

// NewAgoraBot creates a new Agora bot that subscribes to audio and forwards to Anam
// primaryUID is the original user's UID - the bot will always stay subscribed to this
func NewAgoraBot(appID, channel, botUID, token, primaryUID string, anamClient *AnamClient) *AgoraBot {
	return &AgoraBot{
		appID:          appID,
		channel:        channel,
		botUID:         botUID,
		token:          token,
		primaryUID:     primaryUID,
		translationUID: "",    // No translation initially
		translationActive: false,
		anamClient:     anamClient,
		stopChan:       make(chan struct{}),
		targetLeftChan: make(chan struct{}),
		isConnected:    false,
		audioBuffer:    make([][]byte, 10), // 10 frames = ~100ms pre-roll
		rmsThreshold:   50,                 // Lower RMS threshold for voice detection (was 100)
		sendingAudio:   false,
		lastAudioTime:  time.Now(), // Initialize to now
		streamStartTime: time.Now(), // Initialize for VAD bypass
	}
}

// Start connects the bot to Agora and subscribes to target UID
func (b *AgoraBot) Start() error {
	// Initialize Agora service
	svcCfg := agoraservice.NewAgoraServiceConfig()
	svcCfg.AppId = b.appID
	svcCfg.LogPath = "./agora_rtc_log/agorasdk.log"
	svcCfg.ConfigDir = "./agora_rtc_log"
	svcCfg.DataDir = "./agora_rtc_log"

	agoraservice.Initialize(svcCfg)
	fmt.Printf("[AgoraBot] Agora service initialized\n")

	// Create RTC connection config WITHOUT auto-subscribe
	// Bot will manually subscribe ONLY to target UID (Palabra 3000)
	conCfg := &agoraservice.RtcConnectionConfig{
		AutoSubscribeAudio: false, // CRITICAL: Don't auto-subscribe to all users
		AutoSubscribeVideo: false,
		ClientRole:         agoraservice.ClientRoleBroadcaster,
		ChannelProfile:     agoraservice.ChannelProfileLiveBroadcasting,
	}

	// Create publish config (needed even if not publishing)
	publishConfig := agoraservice.NewRtcConPublishConfig()
	publishConfig.AudioPublishType = agoraservice.AudioPublishTypePcm
	publishConfig.IsPublishAudio = false // Not publishing, only subscribing
	publishConfig.IsPublishVideo = false
	publishConfig.AudioScenario = agoraservice.AudioScenarioDefault

	// Create connection
	b.conn = agoraservice.NewRtcConnection(conCfg, publishConfig)
	if b.conn == nil {
		return fmt.Errorf("failed to create RTC connection")
	}

	fmt.Printf("[AgoraBot] RTC connection created\n")

	// Open PCM file for debugging (can be imported to Audacity as Raw PCM: 24kHz, mono, 16-bit signed LE)
	pcmFile, err := os.Create("/tmp/anam_audio_24khz.pcm")
	if err != nil {
		fmt.Printf("[AgoraBot] WARNING: Could not create PCM debug file: %v\n", err)
	} else {
		b.pcmFile = pcmFile
		fmt.Printf("[AgoraBot] Recording PCM to /tmp/anam_audio_24khz.pcm (import to Audacity: Raw, 24000Hz, mono, 16-bit signed LE)\n")
	}

	// Create connection signal channel (to wait for connection before registering observers)
	connSignal := make(chan struct{})

	// Register connection observer
	connObserver := &agoraservice.RtcConnectionObserver{
		OnConnected: func(con *agoraservice.RtcConnection, info *agoraservice.RtcConnectionInfo, reason int) {
			fmt.Printf("[AgoraBot] ✅ Bot (UID %s) connected to channel: %s\n", b.botUID, info.ChannelId)
			connSignal <- struct{}{} // Signal that connection is ready
		},
		OnDisconnected: func(con *agoraservice.RtcConnection, info *agoraservice.RtcConnectionInfo, reason int) {
			fmt.Printf("[AgoraBot] ❌ Bot (UID %s) disconnected from channel: %s\n", b.botUID, info.ChannelId)
		},
		OnUserJoined: func(con *agoraservice.RtcConnection, uid string) {
			fmt.Printf("[AgoraBot] 👤 User joined channel: UID %s (primary=%s, translation=%s)\n", uid, b.primaryUID, b.translationUID)

			localUser := con.GetLocalUser()
			if localUser == nil {
				fmt.Printf("[AgoraBot] ERROR: localUser is nil, cannot subscribe\n")
				return
			}

			// Subscribe to primary UID (original user) when they join
			if uid == b.primaryUID {
				fmt.Printf("[AgoraBot] 🎯 Primary UID %s joined! Subscribing to original audio\n", uid)
				ret := localUser.SubscribeAudio(uid)
				if ret == 0 {
					fmt.Printf("[AgoraBot] ✅ Subscribed to primary audio from UID %s\n", uid)
				} else {
					fmt.Printf("[AgoraBot] ERROR: Failed to subscribe to primary UID %s, ret=%d\n", uid, ret)
				}
			}

			// Subscribe to translation UID (Palabra) when it joins, if translation is pending
			if b.translationUID != "" && uid == b.translationUID {
				fmt.Printf("[AgoraBot] 🌐 Translation UID %s joined! Subscribing to translated audio\n", uid)
				ret := localUser.SubscribeAudio(uid)
				if ret == 0 {
					fmt.Printf("[AgoraBot] ✅ Subscribed to translation audio from UID %s\n", uid)
				} else {
					fmt.Printf("[AgoraBot] ERROR: Failed to subscribe to translation UID %s, ret=%d\n", uid, ret)
				}
			}
		},
		OnUserLeft: func(con *agoraservice.RtcConnection, uid string, reason int) {
			fmt.Printf("[AgoraBot] User left: %s (reason: %d)\n", uid, reason)

			// If primary UID (original user) leaves, signal to stop
			if uid == b.primaryUID {
				fmt.Printf("[AgoraBot] ⚠️ Primary UID %s left channel - signaling shutdown\n", uid)
				select {
				case <-b.targetLeftChan:
					// Already closed
				default:
					close(b.targetLeftChan)
				}
			}

			// If translation UID leaves, mark translation as inactive
			if b.translationUID != "" && uid == b.translationUID {
				fmt.Printf("[AgoraBot] 🌐 Translation UID %s left - falling back to primary audio\n", uid)
				b.translationActive = false
			}
		},
	}

	b.conn.RegisterObserver(connObserver)

	// Connect to channel FIRST
	b.conn.Connect(b.token, b.channel, b.botUID)
	fmt.Printf("[AgoraBot] Connecting to channel %s as UID %s...\n", b.channel, b.botUID)

	// Wait for connection to complete (like the working example)
	<-connSignal
	fmt.Printf("[AgoraBot] Connection established! Now registering audio observer...\n")

	// Get localUser AFTER connection (critical!)
	localUser := b.conn.GetLocalUser()
	if localUser != nil {
		// Set audio parameters (from working example)
		localUser.SetPlaybackAudioFrameBeforeMixingParameters(1, 16000)
		fmt.Printf("[AgoraBot] Audio parameters set\n")
	}

	// Register audio frame observer AFTER connection
	audioObserver := &agoraservice.AudioFrameObserver{
		OnPlaybackAudioFrameBeforeMixing: func(localUser *agoraservice.LocalUser, channelId string, userId string, frame *agoraservice.AudioFrame, vadResultState agoraservice.VadState, vadResultFrame *agoraservice.AudioFrame) bool {
			// DEBUG: Log all audio callbacks to diagnose issues
			fmt.Printf("[AgoraBot] Audio callback - UID: %s, primary: %s, translation: %s, translationActive: %v\n",
				userId, b.primaryUID, b.translationUID, b.translationActive)

			// DUAL-SUBSCRIPTION PRIORITY LOGIC:
			// - If we receive audio from translationUID → use it (set translationActive = true)
			// - If we receive audio from primaryUID AND (no translation OR translation not yet active) → use it
			// This ensures seamless audio: original plays until translation kicks in, then falls back when translation stops

			// Determine if we should process this audio
			shouldProcess := false
			audioSource := ""

			if b.translationUID != "" && userId == b.translationUID {
				// Translation audio - always prioritize when available
				shouldProcess = true
				audioSource = "translation"
				if !b.translationActive {
					fmt.Printf("[AgoraBot] 🌐 Translation audio detected from UID %s - switching to translated audio\n", userId)
					b.translationActive = true
				}
			} else if userId == b.primaryUID {
				// Primary (original) audio - use if no translation or translation not yet active
				if b.translationUID == "" || !b.translationActive {
					shouldProcess = true
					audioSource = "primary"
				}
				// If translation is active, ignore primary audio (translation takes priority)
			}

			if shouldProcess {
				// CRITICAL: Anam expects 24kHz audio, but Agora gives us 16kHz
				// We need to upsample from 16kHz to 24kHz (ratio 3:2)

				if frame.SamplesPerSec != 16000 {
					fmt.Printf("[AgoraBot] WARNING: Unexpected sample rate %d Hz (expected 16000 Hz)\n", frame.SamplesPerSec)
				}

				// Convert PCM bytes to int16 samples
				inputSamples := make([]int16, len(frame.Buffer)/2)
				for i := 0; i < len(inputSamples); i++ {
					inputSamples[i] = int16(frame.Buffer[i*2]) | int16(frame.Buffer[i*2+1])<<8
				}

				// Calculate RMS (volume level)
				_, rms := isFrameSilent(inputSamples)

				// Upsample to 24kHz
				outputSamples := upsample16to24(inputSamples)

				// Convert back to bytes
				outputBytes := make([]byte, len(outputSamples)*2)
				for i, sample := range outputSamples {
					outputBytes[i*2] = byte(sample)
					outputBytes[i*2+1] = byte(sample >> 8)
				}

				// VOICE ACTIVITY DETECTION (VAD)
				// Store frame in ring buffer (for pre-roll)
				b.audioBuffer[b.bufferIndex] = outputBytes
				b.bufferIndex = (b.bufferIndex + 1) % len(b.audioBuffer)

				// Check if in VAD bypass period (first 3 seconds after start/switch)
				inBypassPeriod := time.Since(b.streamStartTime) < vadBypassDuration

				// Check if voice detected (RMS above threshold) OR in bypass period
				voiceDetected := rms > b.rmsThreshold || inBypassPeriod

				if voiceDetected {
					// Voice detected (or in bypass period)!
					if !b.sendingAudio {
						// START sending audio to Anam
						// First, send pre-roll buffer (last 100ms) to catch the beginning
						if inBypassPeriod {
							fmt.Printf("[AgoraBot] 🎤 VAD BYPASS - Starting audio stream immediately (RMS=%d)\n", rms)
						} else {
							fmt.Printf("[AgoraBot] 🎤 VOICE DETECTED (RMS=%d) - Starting audio stream with 100ms pre-roll\n", rms)
						}

						// Send buffered frames (last 10 frames = ~100ms)
						sentPreroll := 0
						for i := 0; i < len(b.audioBuffer); i++ {
							idx := (b.bufferIndex + i) % len(b.audioBuffer)
							if b.audioBuffer[idx] != nil {
								prerollB64 := base64.StdEncoding.EncodeToString(b.audioBuffer[idx])
								b.anamClient.SendAudioWithSampleRate(prerollB64, 24000)
								sentPreroll++
							}
						}
						fmt.Printf("[AgoraBot] 📤 Sent %d pre-roll frames (~%dms)\n", sentPreroll, sentPreroll*10)

						b.sendingAudio = true
						b.isSpeaking = true
					}

					// Reset silence counter
					b.silenceFrames = 0

					// Send current frame
					audioB64 := base64.StdEncoding.EncodeToString(outputBytes)
					err := b.anamClient.SendAudioWithSampleRate(audioB64, 24000)
					if err != nil {
						fmt.Printf("[AgoraBot] ❌ Error forwarding audio: %v\n", err)
					}

					// Update last audio time for idle detection
					b.lastAudioTime = time.Now()

					// Log every 100 frames (~1 second)
					b.frameCount++
					if b.frameCount%100 == 0 {
						fmt.Printf("[AgoraBot] 📊 Sending %s voice: %d frames total, RMS=%d\n", audioSource, b.frameCount, rms)
					}

				} else if b.sendingAudio {
					// Currently sending but this frame is silent
					b.silenceFrames++

					// Continue sending for 500ms after voice stops (to avoid cutting off)
					if b.silenceFrames < 50 {
						// Still in tail period - keep sending
						audioB64 := base64.StdEncoding.EncodeToString(outputBytes)
						b.anamClient.SendAudioWithSampleRate(audioB64, 24000)
						b.frameCount++
					} else {
						// 500ms of silence - STOP sending
						fmt.Printf("[AgoraBot] 🔇 SILENCE for 500ms (RMS=%d) - Stopping audio stream (sent %d frames total)\n", rms, b.frameCount)
						b.anamClient.SendVoiceEnd()
						b.sendingAudio = false
						b.isSpeaking = false
						b.silenceFrames = 0
						b.frameCount = 0
					}
				}

				// DEBUG: Write ALL audio to PCM file (for debugging)
				if b.pcmFile != nil {
					b.pcmFile.Write(outputBytes)
				}
			}
			return true
		},
	}

	// Register audio observer AFTER connection (from working example)
	b.conn.RegisterAudioFrameObserver(audioObserver, 0, nil)
	fmt.Printf("[AgoraBot] Audio frame observer registered\n")

	b.isConnected = true
	b.streamStartTime = time.Now() // Start VAD bypass timer
	fmt.Printf("[AgoraBot] Bot ready - subscribed to primary UID %s (VAD bypass for %v)\n", b.primaryUID, vadBypassDuration)

	// NOTE: Bot is subscribed to primary (original user) and will add translation when requested
	fmt.Printf("[AgoraBot] Waiting for audio from primary UID %s (will add translation UID when set)\n", b.primaryUID)

	return nil
}

// sendPeriodicSilence sends silence to Anam every 2 seconds to keep connection alive
func (b *AgoraBot) sendPeriodicSilence() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// 1 second of silence at 16kHz PCM16
	silenceBytes := make([]byte, 32000) // 16000 samples * 2 bytes
	silenceB64 := base64.StdEncoding.EncodeToString(silenceBytes)

	for {
		select {
		case <-b.stopChan:
			fmt.Printf("[AgoraBot] Stopping silence sender\n")
			return
		case <-ticker.C:
			if b.anamClient != nil && b.anamClient.IsConnected() {
				err := b.anamClient.SendAudio(silenceB64)
				if err != nil {
					fmt.Printf("[AgoraBot] Error sending test silence to Anam: %v\n", err)
				} else {
					fmt.Printf("[AgoraBot] Sent test silence to Anam to keep connection alive\n")
				}
			}
		}
	}
}

// SetTranslationUID sets or clears the translation UID for dual-subscription mode
// When set: bot subscribes to translation UID (if in channel) and prioritizes its audio
// When cleared (empty string): bot falls back to primary UID audio only
// Note: Bot always stays subscribed to primary UID - never unsubscribes from it
func (b *AgoraBot) SetTranslationUID(translationUID string) error {
	if !b.isConnected || b.conn == nil {
		return fmt.Errorf("bot not connected")
	}

	localUser := b.conn.GetLocalUser()
	if localUser == nil {
		return fmt.Errorf("localUser is nil, cannot set translation UID")
	}

	oldTransUID := b.translationUID

	if translationUID == "" {
		// Clearing translation - fall back to primary
		if oldTransUID != "" {
			fmt.Printf("[AgoraBot] 🔄 Clearing translation UID %s - falling back to primary UID %s\n", oldTransUID, b.primaryUID)
			// Unsubscribe from old translation UID
			ret := localUser.UnsubscribeAudio(oldTransUID)
			if ret != 0 {
				fmt.Printf("[AgoraBot] WARNING: Failed to unsubscribe from translation UID %s (ret=%d)\n", oldTransUID, ret)
			}
		}
		b.translationUID = ""
		b.translationActive = false
		fmt.Printf("[AgoraBot] ✅ Now using primary audio only (UID %s)\n", b.primaryUID)
	} else {
		// Setting new translation UID
		if oldTransUID == translationUID {
			fmt.Printf("[AgoraBot] Translation UID unchanged (%s)\n", translationUID)
			return nil
		}

		// Unsubscribe from old translation UID if different
		if oldTransUID != "" && oldTransUID != translationUID {
			fmt.Printf("[AgoraBot] 🔄 Switching translation UID: %s → %s\n", oldTransUID, translationUID)
			ret := localUser.UnsubscribeAudio(oldTransUID)
			if ret != 0 {
				fmt.Printf("[AgoraBot] WARNING: Failed to unsubscribe from old translation UID %s (ret=%d)\n", oldTransUID, ret)
			}
		} else {
			fmt.Printf("[AgoraBot] 🌐 Setting translation UID: %s\n", translationUID)
		}

		b.translationUID = translationUID
		b.translationActive = false // Will become true when we receive audio from this UID

		// Try to subscribe (may fail if UID not yet in channel - that's OK, OnUserJoined will handle it)
		ret := localUser.SubscribeAudio(translationUID)
		if ret == 0 {
			fmt.Printf("[AgoraBot] ✅ Subscribed to translation UID %s (waiting for audio)\n", translationUID)
		} else {
			fmt.Printf("[AgoraBot] ⏳ Translation UID %s not yet in channel (will subscribe when it joins)\n", translationUID)
		}
	}

	// Reset VAD state for clean audio handling
	b.sendingAudio = false
	b.speechFrames = 0
	b.silenceFrames = 0
	b.isSpeaking = false
	b.streamStartTime = time.Now() // Restart VAD bypass timer for new audio source

	return nil
}

// SwitchAudioSource is deprecated - use SetTranslationUID instead
// Kept for backwards compatibility with existing IPC messages
func (b *AgoraBot) SwitchAudioSource(newUID string) error {
	// If newUID matches primary, clear translation (fall back to primary)
	if newUID == b.primaryUID {
		return b.SetTranslationUID("")
	}
	// Otherwise, set as translation UID
	return b.SetTranslationUID(newUID)
}

// GetTargetUID returns the current active audio source UID
// Returns translation UID if active, otherwise primary UID
func (b *AgoraBot) GetTargetUID() string {
	if b.translationUID != "" && b.translationActive {
		return b.translationUID
	}
	return b.primaryUID
}

// GetPrimaryUID returns the primary (original user) UID
func (b *AgoraBot) GetPrimaryUID() string {
	return b.primaryUID
}

// GetTranslationUID returns the translation UID (empty if not set)
func (b *AgoraBot) GetTranslationUID() string {
	return b.translationUID
}

// IsTranslationActive returns true if translation audio is being prioritized
func (b *AgoraBot) IsTranslationActive() bool {
	return b.translationActive
}

// Stop disconnects the bot and releases resources
func (b *AgoraBot) Stop() error {
	if !b.isConnected {
		return nil
	}

	close(b.stopChan)

	if b.pcmFile != nil {
		b.pcmFile.Close()
		fmt.Printf("[AgoraBot] PCM debug file closed: /tmp/anam_audio_24khz.pcm\n")
	}

	if b.conn != nil {
		b.conn.Disconnect()
		b.conn.Release()
		fmt.Printf("[AgoraBot] Disconnected from channel\n")
	}

	agoraservice.Release()
	fmt.Printf("[AgoraBot] Agora service released\n")

	b.isConnected = false
	return nil
}

// IsConnected returns whether the bot is connected
func (b *AgoraBot) IsConnected() bool {
	return b.isConnected
}

// isFrameSilent checks if an audio frame is silent using RMS energy
func isFrameSilent(samples []int16) (bool, int64) {
	if len(samples) == 0 {
		return true, 0
	}

	// Calculate RMS (Root Mean Square) energy
	var sum int64
	for _, sample := range samples {
		sum += int64(sample) * int64(sample)
	}
	rms := sum / int64(len(samples))

	// CRITICAL: Lowered threshold based on testing
	// Palabra audio seems to have lower amplitude than typical speech
	// Was 1000, now 100 to avoid filtering actual speech
	const silenceThreshold int64 = 100

	return rms < silenceThreshold, rms
}

// upsample16to24 upsamples PCM16 audio from 16kHz to 24kHz using linear interpolation
// Input: 160 samples @ 16kHz (10ms of audio)
// Output: 240 samples @ 24kHz (10ms of audio)
func upsample16to24(input []int16) []int16 {
	inputLen := len(input)
	outputLen := (inputLen * 3) / 2 // 3:2 ratio

	output := make([]int16, outputLen)

	// For every 2 input samples, create 3 output samples
	for i := 0; i < inputLen-1; i++ {
		outputIdx := (i * 3) / 2

		// First output sample = input sample
		output[outputIdx] = input[i]

		// If we have room for interpolated samples
		if outputIdx+1 < outputLen {
			// Interpolate between input[i] and input[i+1]
			// For 3:2, we insert one sample at 2/3 position
			output[outputIdx+1] = int16((int32(input[i])*1 + int32(input[i+1])*2) / 3)
		}

		if outputIdx+2 < outputLen && i%2 == 0 {
			// Every other pair gets a third sample
			output[outputIdx+2] = int16((int32(input[i])*1 + int32(input[i+1])*1) / 2)
		}
	}

	// Last sample
	if inputLen > 0 {
		output[outputLen-1] = input[inputLen-1]
	}

	return output
}

// GetIdleDuration returns how long since audio was last sent to Anam
func (b *AgoraBot) GetIdleDuration() time.Duration {
	return time.Since(b.lastAudioTime)
}

// TargetLeftChan returns a channel that closes when target UID leaves
func (b *AgoraBot) TargetLeftChan() <-chan struct{} {
	return b.targetLeftChan
}
