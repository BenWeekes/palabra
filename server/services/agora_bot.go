package services

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	agoraservice "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
)

// AgoraBot subscribes to Palabra audio (UID 3000) and forwards to Anam WebSocket
type AgoraBot struct {
	appID         string
	channel       string
	botUID        string // UID 4000+ (Anam avatar)
	token         string
	targetUID     string // UID 3000+ (Palabra audio to subscribe to)
	anamClient    *AnamClient
	conn          *agoraservice.RtcConnection
	stopChan      chan struct{}
	targetLeftChan chan struct{} // Signals when target UID leaves channel
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
}

// NewAgoraBot creates a new Agora bot that subscribes to audio and forwards to Anam
func NewAgoraBot(appID, channel, botUID, token, targetUID string, anamClient *AnamClient) *AgoraBot {
	return &AgoraBot{
		appID:          appID,
		channel:        channel,
		botUID:         botUID,
		token:          token,
		targetUID:      targetUID,
		anamClient:     anamClient,
		stopChan:       make(chan struct{}),
		targetLeftChan: make(chan struct{}),
		isConnected:    false,
		audioBuffer:    make([][]byte, 10), // 10 frames = ~100ms pre-roll
		rmsThreshold:   100,                // RMS threshold for voice detection
		sendingAudio:   false,
		lastAudioTime:  time.Now(), // Initialize to now
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
			fmt.Printf("[AgoraBot] 👤 User joined channel: UID %s (Bot listening for UID %s)\n", uid, b.targetUID)

			// Explicitly subscribe to Palabra audio when it joins
			if uid == b.targetUID {
				fmt.Printf("[AgoraBot] 🎯 Target UID %s joined! Bot will now subscribe and forward audio to Anam\n", uid)
				fmt.Printf("[AgoraBot] Target UID %s joined! Explicitly subscribing to audio...\n", uid)

				// Get local user and subscribe
				localUser := con.GetLocalUser()
				if localUser != nil {
					ret := localUser.SubscribeAudio(uid)
					if ret == 0 {
						fmt.Printf("[AgoraBot] Successfully subscribed to audio from UID %s\n", uid)
					} else {
						fmt.Printf("[AgoraBot] ERROR: Failed to subscribe to audio from UID %s, ret=%d\n", uid, ret)
					}
				} else {
					fmt.Printf("[AgoraBot] ERROR: localUser is nil, cannot subscribe\n")
				}
			}
		},
		OnUserLeft: func(con *agoraservice.RtcConnection, uid string, reason int) {
			fmt.Printf("[AgoraBot] User left: %s (reason: %d)\n", uid, reason)
			// If our target UID (Palabra bot) leaves, signal to stop
			if uid == b.targetUID {
				fmt.Printf("[AgoraBot] ⚠️ Target UID %s left channel - signaling shutdown\n", uid)
				select {
				case <-b.targetLeftChan:
					// Already closed
				default:
					close(b.targetLeftChan)
				}
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
			// DEBUG: Log EVERY audio callback
			fmt.Printf("[AgoraBot] Audio callback fired - UID: %s, BufferSize: %d, Target: %s\n", userId, len(frame.Buffer), b.targetUID)

			// Only forward audio from Palabra UID
			if userId == b.targetUID {
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

				// Check if voice detected (RMS above threshold)
				voiceDetected := rms > b.rmsThreshold

				if voiceDetected {
					// Voice detected!
					if !b.sendingAudio {
						// START sending audio to Anam
						// First, send pre-roll buffer (last 100ms) to catch the beginning
						fmt.Printf("[AgoraBot] 🎤 VOICE DETECTED (RMS=%d) - Starting audio stream with 100ms pre-roll\n", rms)

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
						fmt.Printf("[AgoraBot] 📊 Sending voice: %d frames total, RMS=%d\n", b.frameCount, rms)
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
	fmt.Printf("[AgoraBot] Bot ready - subscribed to UID %s\n", b.targetUID)

	// NOTE: No test silence sender - only forward real audio from Palabra
	fmt.Printf("[AgoraBot] Waiting for audio from Palabra UID %s\n", b.targetUID)

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
