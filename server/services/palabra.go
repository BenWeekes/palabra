package services

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/samyak-jain/agora_backend/utils/rtctoken"
	"github.com/spf13/viper"
)

// PalabraStartRequest represents the request to start translation
type PalabraStartRequest struct {
	Channel          string   `json:"channel"`
	SourceUID        string   `json:"sourceUid"`
	SourceName       string   `json:"sourceName"`       // NEW: User's display name
	SourceLanguage   string   `json:"sourceLanguage"`
	TargetLanguages  []string `json:"targetLanguages"`
	Mode             string   `json:"mode,omitempty"`
}

// PalabraStopRequest represents the request to stop translation
type PalabraStopRequest struct {
	TaskID string `json:"taskId"`
}

// PalabraTranslation represents a translation stream
type PalabraTranslation struct {
	LocalUID       string                 `json:"local_uid"`
	Token          string                 `json:"token"`
	TargetLanguage string                 `json:"target_language"`
	Options        map[string]interface{} `json:"options"`
}

// PalabraAPIRequest represents the payload sent to Palabra API
type PalabraAPIRequest struct {
	AgoraAppID        string               `json:"agoraAppId"`
	Channel           string               `json:"channel"`
	RemoteUID         string               `json:"remote_uid"`
	LocalUID          string               `json:"local_uid"`
	Token             string               `json:"token"`
	SpeechRecognition map[string]interface{} `json:"speech_recognition"`
	Translations      []PalabraTranslation `json:"translations"`
}

// PalabraAPIResponse represents the response from Palabra API
type PalabraAPIResponse struct {
	OK   bool                `json:"ok"`
	Data PalabraResponseData `json:"data"`
}

// PalabraResponseData represents the data field in Palabra API response
type PalabraResponseData struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// PalabraStreamInfo represents info about a translation stream
type PalabraStreamInfo struct {
	UID      string `json:"uid"`
	Language string `json:"language"`
}

// PalabraStartResponse represents the response for start translation
type PalabraStartResponse struct {
	Success bool                `json:"success"`
	TaskID  string              `json:"taskId"`
	Streams []PalabraStreamInfo `json:"streams"`
	Mode    string              `json:"mode,omitempty"`
	Error   string              `json:"error,omitempty"`
}

// PalabraStopResponse represents the response for stop translation
type PalabraStopResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

const (
	palabraAPIURL = "https://api.palabra.ai/agora/translations"
	taskUIDBase   = 200
	transUIDBase  = 3000
	anamUIDBase   = 4000 // NEW: Base UID for Anam avatar streams
)

// TaskInfo represents an active translation task
type TaskInfo struct {
	TaskID      string
	Streams     []PalabraStreamInfo
	SourceUID   string
	Channel     string
	Language    string
	Mode        string
}

// AvatarSession represents a standalone avatar session (persistent avatar mode)
type AvatarSession struct {
	SessionID      string `json:"sessionId"`
	Channel        string `json:"channel"`
	SourceUID      string `json:"sourceUid"`
	AnamUID        uint32 `json:"anamUid"`
	BotUID         uint32 `json:"botUid"`
	BotProcessID   string `json:"botProcessId"`
	HasTranslation bool   `json:"hasTranslation"`   // true if Palabra is active on this avatar
	PalabraTaskID  string `json:"palabraTaskId"`    // set when translation active
	PalabraUID     uint32 `json:"palabraUid"`       // Palabra stream UID when translating
}

// AvatarStartRequest represents the request to start an avatar
type AvatarStartRequest struct {
	Channel   string `json:"channel"`
	SourceUID string `json:"sourceUid"`
}

// AvatarStartResponse represents the response for start avatar
type AvatarStartResponse struct {
	Success   bool   `json:"success"`
	SessionID string `json:"sessionId,omitempty"`
	AnamUID   uint32 `json:"anamUid,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AvatarStopRequest represents the request to stop an avatar
type AvatarStopRequest struct {
	Channel   string `json:"channel"`
	SourceUID string `json:"sourceUid"`
}

// AvatarStopResponse represents the response for stop avatar
type AvatarStopResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

var (
	// Per-channel counters for Anam UIDs (channel -> next available UID)
	channelAnamCounters = make(map[string]uint32)
	// Per-channel counters for Translation UIDs (channel -> next available UID)
	channelTransCounters = make(map[string]uint32)
	// Task deduplication: map key is "channel:sourceUid:targetLanguage"
	activeTasksByKey = make(map[string]*TaskInfo)
	// Avatar sessions: map key is "channel:sourceUid"
	avatarSessions = make(map[string]*AvatarSession)
)

// getNextAnamUID returns the next available Anam UID for a channel
func getNextAnamUID(channel string) uint32 {
	if _, exists := channelAnamCounters[channel]; !exists {
		channelAnamCounters[channel] = anamUIDBase // Start at 4000 for new channels
	}
	uid := channelAnamCounters[channel]
	channelAnamCounters[channel]++
	return uid
}

// getNextTransUID returns the next available Translation UID for a channel
func getNextTransUID(channel string) uint32 {
	if _, exists := channelTransCounters[channel]; !exists {
		channelTransCounters[channel] = transUIDBase // Start at 3000 for new channels
	}
	uid := channelTransCounters[channel]
	channelTransCounters[channel]++
	return uid
}

// PalabraStart handles starting a translation task
func (s *ServiceRouter) PalabraStart(w http.ResponseWriter, r *http.Request) {
	s.Logger.Info().Msg("Palabra start translation request received")

	// Parse request
	var req PalabraStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to parse request body")
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Log incoming request
	s.Logger.Info().
		Str("channel", req.Channel).
		Str("sourceUid", req.SourceUID).
		Str("sourceLanguage", req.SourceLanguage).
		Strs("targetLanguages", req.TargetLanguages).
		Str("mode", req.Mode).
		Msg("[PALABRA-START] Received translation request")

	// Validate required fields
	if req.Channel == "" || req.SourceUID == "" || req.SourceLanguage == "" || len(req.TargetLanguages) == 0 {
		s.Logger.Error().Msg("Missing required fields")
		respondWithError(w, http.StatusBadRequest, "Missing required fields: channel, sourceUid, sourceLanguage, targetLanguages")
		return
	}

	// OPTIMIZATION: Check if task already exists for this (channel, sourceUid, targetLanguage)
	// Prevent duplicate Palabra tasks for the same translation
	for _, targetLang := range req.TargetLanguages {
		taskKey := fmt.Sprintf("%s:%s:%s", req.Channel, req.SourceUID, targetLang)
		if existingTask, exists := activeTasksByKey[taskKey]; exists {
			s.Logger.Info().
				Str("taskKey", taskKey).
				Str("existingTaskID", existingTask.TaskID).
				Msg("[PALABRA-START] Task already exists, returning existing streams")

			// Return existing task info
			respondWithJSON(w, http.StatusOK, map[string]interface{}{
				"ok": true,
				"data": map[string]interface{}{
					"taskId":  existingTask.TaskID,
					"streams": existingTask.Streams,
				},
			})
			return
		}
	}

	// Get credentials
	appID := viper.GetString("APP_ID")
	appCertificate := viper.GetString("APP_CERTIFICATE")
	palabraClientID := viper.GetString("PALABRA_CLIENT_ID")
	palabraClientSecret := viper.GetString("PALABRA_CLIENT_SECRET")

	if appID == "" || appCertificate == "" {
		s.Logger.Error().Msg("Missing Agora credentials")
		respondWithError(w, http.StatusInternalServerError, "Server configuration error: missing Agora credentials")
		return
	}

	if palabraClientID == "" || palabraClientSecret == "" {
		s.Logger.Error().Msg("Missing Palabra credentials")
		respondWithError(w, http.StatusInternalServerError, "Server configuration error: missing Palabra credentials")
		return
	}

	// Generate tokens
	expireTime := uint32(time.Now().Unix()) + 3600*24 // 24 hours

	// Task token (UID 200)
	taskToken, err := rtctoken.BuildTokenWithUID(
		appID,
		appCertificate,
		req.Channel,
		taskUIDBase,
		rtctoken.RolePublisher,
		expireTime,
	)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to generate task token")
		respondWithError(w, http.StatusInternalServerError, "Failed to generate task token")
		return
	}

	// Translation tokens (UIDs 3000, 3001, ...)
	translations := make([]PalabraTranslation, len(req.TargetLanguages))
	streams := make([]PalabraStreamInfo, len(req.TargetLanguages))

	for i, lang := range req.TargetLanguages {
		uid := getNextTransUID(req.Channel) // Get unique UID per channel to avoid collisions
		token, err := rtctoken.BuildTokenWithUID(
			appID,
			appCertificate,
			req.Channel,
			uid,
			rtctoken.RolePublisher,
			expireTime,
		)
		if err != nil {
			s.Logger.Error().Err(err).Msgf("Failed to generate translation token for UID %d", uid)
			respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to generate translation token for UID %d", uid))
			return
		}

		translations[i] = PalabraTranslation{
			LocalUID:       fmt.Sprintf("%d", uid),
			Token:          token,
			TargetLanguage: lang,
			Options: map[string]interface{}{
				"speech_generation": map[string]interface{}{
					"voice_cloning": true,
					"voice_timbre_detection": map[string]interface{}{
						"enabled":            true,
						"high_timbre_voices": []string{"default_high"},
						"low_timbre_voices":  []string{"default_low"},
					},
				},
			},
		}

		streams[i] = PalabraStreamInfo{
			UID:      fmt.Sprintf("%d", uid),
			Language: lang,
		}
	}

	// Build Palabra API request
	palabraReq := PalabraAPIRequest{
		AgoraAppID: appID,
		Channel:    req.Channel,
		RemoteUID:  req.SourceUID,
		LocalUID:   fmt.Sprintf("%d", taskUIDBase),
		Token:      taskToken,
		SpeechRecognition: map[string]interface{}{
			"source_language": req.SourceLanguage,
			"options":         make(map[string]interface{}),
		},
		Translations: translations,
	}

	// Call Palabra API
	jsonData, err := json.Marshal(palabraReq)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to marshal Palabra request")
		respondWithError(w, http.StatusInternalServerError, "Failed to create API request")
		return
	}

	s.Logger.Info().Str("channel", req.Channel).Str("sourceUid", req.SourceUID).Msg("Calling Palabra API")

	httpReq, err := http.NewRequest("POST", palabraAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to create HTTP request")
		respondWithError(w, http.StatusInternalServerError, "Failed to create API request")
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("ClientID", palabraClientID)
	httpReq.Header.Set("ClientSecret", palabraClientSecret)

	// Create HTTP client with TLS config (skip verification for development)
	// TODO: For production, install proper CA certificates in container
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to call Palabra API")
		respondWithError(w, http.StatusInternalServerError, "Failed to call Palabra API")
		return
	}
	defer resp.Body.Close()

	// Read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to read Palabra API response")
		respondWithError(w, http.StatusInternalServerError, "Failed to read API response")
		return
	}

	s.Logger.Info().Int("status", resp.StatusCode).Str("body", string(body)).Msg("Palabra API response")

	// Check if successful
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.Logger.Error().Int("status", resp.StatusCode).Str("body", string(body)).Msg("Palabra API returned error")
		respondWithJSON(w, http.StatusOK, PalabraStartResponse{
			Success: false,
			Error:   fmt.Sprintf("Palabra API error: %s", string(body)),
		})
		return
	}

	// Parse Palabra response
	var palabraResp PalabraAPIResponse
	if err := json.Unmarshal(body, &palabraResp); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to parse Palabra API response")
		respondWithError(w, http.StatusInternalServerError, "Failed to parse API response")
		return
	}

	// Check if Palabra API call was successful
	if !palabraResp.OK {
		errorMsg := palabraResp.Data.Error
		if errorMsg == "" {
			errorMsg = "Unknown error"
		}
		s.Logger.Error().Str("error", errorMsg).Msg("Palabra API returned error")
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Palabra API error: %s", errorMsg))
		return
	}

	// Get task ID from response
	taskID := palabraResp.Data.TaskID

	s.Logger.Info().Str("taskId", taskID).Msg("Translation task started successfully")

	// Determine effective mode (backward compatible — empty = old behavior)
	mode := req.Mode
	if mode == "" {
		if viper.GetBool("ENABLE_ANAM") {
			mode = "translate_video"
		} else {
			mode = "translate_audio"
		}
	}

	s.Logger.Info().
		Str("requestedMode", req.Mode).
		Str("effectiveMode", mode).
		Msg("[PALABRA-START] Resolved translation mode")

	wantAvatar := (mode == "avatar" || mode == "translate_video")

	if wantAvatar {
		if !viper.GetBool("ENABLE_ANAM") {
			respondWithError(w, http.StatusBadRequest, "Avatar mode requires ENABLE_ANAM=true")
			return
		}

		s.Logger.Info().Str("mode", mode).Msg("Avatar mode requested, checking for existing avatar")

		// Check if a persistent avatar already exists for this source user
		existingAvatar := GetAvatarSession(req.Channel, req.SourceUID)

		if existingAvatar != nil {
			// PERSISTENT AVATAR MODE: Avatar already running, switch audio source to Palabra
			s.Logger.Info().
				Str("channel", req.Channel).
				Str("sourceUid", req.SourceUID).
				Str("sessionID", existingAvatar.SessionID).
				Uint32("anamUID", existingAvatar.AnamUID).
				Msg("[PERSISTENT-AVATAR] Avatar exists, switching audio to Palabra stream")

			// Get Palabra UID for this translation
			var palabraUIDNum uint32
			if len(streams) > 0 {
				fmt.Sscanf(streams[0].UID, "%d", &palabraUIDNum)
			} else {
				palabraUIDNum = transUIDBase // Default to 3000
			}

			// Switch audio source from original user to Palabra
			botManager := GetBotProcessManager()
			err := botManager.SwitchAudioSource(existingAvatar.SessionID, palabraUIDNum, true)
			if err != nil {
				s.Logger.Error().Err(err).Msg("[PERSISTENT-AVATAR] Failed to switch audio source")
				// Continue anyway - translation is started, avatar might catch up
			} else {
				s.Logger.Info().
					Uint32("palabraUID", palabraUIDNum).
					Msg("[PERSISTENT-AVATAR] Audio source switched to Palabra stream")
			}

			// Update avatar session to track translation state
			UpdateAvatarTranslation(req.Channel, req.SourceUID, true, taskID, palabraUIDNum)

			// Update streams to use existing Anam UID (client already subscribed)
			for i := range streams {
				streams[i].UID = fmt.Sprintf("%d", existingAvatar.AnamUID)
			}
		} else {
			// NO EXISTING AVATAR: Start new avatar + bot
			s.Logger.Info().Msg("No existing avatar, starting new avatar bot for translation")

			// Get Anam configuration
			avatarID := viper.GetString("ANAM_AVATAR_ID")

			if avatarID == "" {
				s.Logger.Warn().Msg("ANAM_AVATAR_ID not configured, skipping Anam")
			} else {
				// Create Agora bot for each translation stream
				for i, stream := range streams {
					// Save original Palabra UID
					palabraUID := stream.UID

					// Generate Anam UID (for avatar video/audio published by Anam)
					// Uses per-channel counter so each channel starts at 4000
					anamUIDNum := getNextAnamUID(req.Channel)
					anamUID := fmt.Sprintf("%d", anamUIDNum)

					// Generate Bot UID (for our audio forwarder - should NOT be visible to users)
					// Bot UID = 4500+ (within 3000-4999 range so frontend filters it out)
					botUIDNum := uint32(4500 + i)
					botUID := fmt.Sprintf("%d", botUIDNum)

					s.Logger.Info().
						Str("channel", req.Channel).
						Str("palabraUID", palabraUID).
						Str("anamUID", anamUID).
						Str("botUID", botUID).
						Msg("UID assignment for Anam avatar")

					// Update stream UID immediately - client should subscribe to Anam UID, not Palabra
					streams[i].UID = anamUID

					// Generate token for Anam UID (Anam joins as this UID via init message)
					anamToken, err := rtctoken.BuildTokenWithUID(
						appID,
						appCertificate,
						req.Channel,
						anamUIDNum,
						rtctoken.RolePublisher,
						expireTime,
					)
					if err != nil {
						s.Logger.Error().Err(err).Str("anamUID", anamUID).Msg("Failed to generate Anam token")
						continue
					}

					// Generate token for Bot UID (our audio forwarder bot)
					botToken, err := rtctoken.BuildTokenWithUID(
						appID,
						appCertificate,
						req.Channel,
						botUIDNum,
						rtctoken.RoleSubscriber, // Bot only subscribes, doesn't publish to channel
						expireTime,
					)
					if err != nil {
						s.Logger.Error().Err(err).Str("botUID", botUID).Msg("Failed to generate bot token")
						continue
					}

					// Use BotProcessManager to spawn isolated child process
					// This prevents Agora SDK crashes from bringing down the HTTP server
					botManager := GetBotProcessManager()

					// Get Anam configuration
					anamAPIKey := viper.GetString("ANAM_API_KEY")
					anamBaseURL := viper.GetString("ANAM_BASE_URL")
					if anamBaseURL == "" {
						anamBaseURL = "https://api.anam.ai"
					}

					// Parse UIDs to uint32
					var palabraUIDNum uint32
					fmt.Sscanf(palabraUID, "%d", &palabraUIDNum)

					config := StartSessionConfig{
						TaskID:         fmt.Sprintf("%s-%d", taskID, i),
						AppID:          appID,
						Channel:        req.Channel,
						BotUID:         botUIDNum,
						BotToken:       botToken,
						PalabraUID:     palabraUIDNum,
						AnamAPIKey:     anamAPIKey,
						AnamBaseURL:    anamBaseURL,
						AnamAvatarID:   avatarID,
						AnamUID:        anamUIDNum,
						AnamToken:      anamToken,
						TargetLanguage: stream.Language,
					}

					s.Logger.Info().
						Str("palabraUID", palabraUID).
						Str("anamUID", anamUID).
						Str("botUID", botUID).
						Msg("Starting bot process for Anam avatar")

					proc, err := botManager.StartSession(config)
					if err != nil {
						s.Logger.Error().Err(err).Str("anamUID", anamUID).Msg("Failed to start bot process")
						continue
					}

					s.Logger.Info().
						Str("palabraUID", palabraUID).
						Str("anamUID", anamUID).
						Str("botUID", botUID).
						Int("pid", proc.cmd.Process.Pid).
						Msg("Bot process started - isolated process handles Agora bot and Anam client")
				}
			}
		}
	}
	// For translate_audio / translate_audio_with_original: skip avatar block entirely

	// Store task info for deduplication
	for _, targetLang := range req.TargetLanguages {
		taskKey := fmt.Sprintf("%s:%s:%s", req.Channel, req.SourceUID, targetLang)
		activeTasksByKey[taskKey] = &TaskInfo{
			TaskID:    taskID,
			Streams:   streams,
			SourceUID: req.SourceUID,
			Channel:   req.Channel,
			Language:  targetLang,
			Mode:      mode,
		}
		s.Logger.Info().
			Str("taskKey", taskKey).
			Str("taskID", taskID).
			Msg("[PALABRA-START] Stored task for deduplication")
	}

	// Send success response
	respondWithJSON(w, http.StatusOK, PalabraStartResponse{
		Success: true,
		TaskID:  taskID,
		Streams: streams,
		Mode:    mode,
	})
}

// PalabraStop handles stopping a translation task
func (s *ServiceRouter) PalabraStop(w http.ResponseWriter, r *http.Request) {
	s.Logger.Info().Msg("Palabra stop translation request received")

	// Parse request
	var req PalabraStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.Logger.Error().Err(err).Msg("Failed to parse request body")
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if req.TaskID == "" {
		s.Logger.Error().Msg("Missing taskId")
		respondWithError(w, http.StatusBadRequest, "Missing required field: taskId")
		return
	}

	// Get Palabra credentials
	palabraClientID := viper.GetString("PALABRA_CLIENT_ID")
	palabraClientSecret := viper.GetString("PALABRA_CLIENT_SECRET")

	if palabraClientID == "" || palabraClientSecret == "" {
		s.Logger.Error().Msg("Missing Palabra credentials")
		respondWithError(w, http.StatusInternalServerError, "Server configuration error: missing Palabra credentials")
		return
	}

	// Call Palabra API to stop
	url := fmt.Sprintf("%s/%s", palabraAPIURL, req.TaskID)
	s.Logger.Info().Str("taskId", req.TaskID).Str("url", url).Msg("Calling Palabra API to stop translation")

	httpReq, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to create HTTP request")
		respondWithError(w, http.StatusInternalServerError, "Failed to create API request")
		return
	}

	httpReq.Header.Set("ClientID", palabraClientID)
	httpReq.Header.Set("ClientSecret", palabraClientSecret)

	// Create HTTP client with TLS config (skip verification for development)
	// TODO: For production, install proper CA certificates in container
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to call Palabra API")
		respondWithError(w, http.StatusInternalServerError, "Failed to call Palabra API")
		return
	}
	defer resp.Body.Close()

	// Read response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.Logger.Error().Err(err).Msg("Failed to read Palabra API response")
		respondWithError(w, http.StatusInternalServerError, "Failed to read API response")
		return
	}

	s.Logger.Info().Int("status", resp.StatusCode).Str("body", string(body)).Msg("Palabra API stop response")

	// Check if successful (200 or 204 are both success)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		s.Logger.Error().Int("status", resp.StatusCode).Str("body", string(body)).Msg("Palabra API returned error")
		respondWithJSON(w, http.StatusOK, PalabraStopResponse{
			Success: false,
			Error:   fmt.Sprintf("Palabra API error: %s", string(body)),
		})
		return
	}

	s.Logger.Info().Str("taskId", req.TaskID).Msg("Translation task stopped successfully")

	// Clean up bot processes if Anam is enabled
	enableAnam := viper.GetBool("ENABLE_ANAM")
	if enableAnam {
		botManager := GetBotProcessManager()

		// Check if any avatar session was using this translation (persistent avatar mode)
		// If so, switch audio back to original instead of stopping the bot
		avatarPreserved := false
		for avatarKey, session := range avatarSessions {
			if session.HasTranslation && session.PalabraTaskID == req.TaskID {
				s.Logger.Info().
					Str("avatarKey", avatarKey).
					Str("sessionID", session.SessionID).
					Msg("[PERSISTENT-AVATAR] Translation stopping on persistent avatar, switching back to original audio")

				// Parse source UID
				var sourceUIDNum uint32
				fmt.Sscanf(session.SourceUID, "%d", &sourceUIDNum)

				// Switch audio source back from Palabra to original user
				err := botManager.SwitchAudioSource(session.SessionID, sourceUIDNum, false)
				if err != nil {
					s.Logger.Error().Err(err).Msg("[PERSISTENT-AVATAR] Failed to switch audio back to original")
				} else {
					s.Logger.Info().
						Uint32("sourceUID", sourceUIDNum).
						Msg("[PERSISTENT-AVATAR] Audio source switched back to original user")
				}

				// Update avatar session to clear translation state
				session.HasTranslation = false
				session.PalabraTaskID = ""
				session.PalabraUID = 0

				avatarPreserved = true
				// Note: Don't break - there could be multiple avatar sessions affected
			}
		}

		// Only stop bot processes if no avatar was preserved
		// (i.e., translation was started without pre-existing avatar)
		if !avatarPreserved {
			// Stop all sessions associated with this task ID
			// Sessions are keyed as "taskID-index"
			sessions := botManager.GetAllSessions()
			for sessionID := range sessions {
				if len(sessionID) >= len(req.TaskID) && sessionID[:len(req.TaskID)] == req.TaskID {
					s.Logger.Info().Str("taskId", req.TaskID).Str("sessionId", sessionID).Msg("Stopping bot process")

					err := botManager.StopSession(sessionID)
					if err != nil {
						s.Logger.Error().Err(err).Str("sessionId", sessionID).Msg("Failed to stop bot process")
					}
				}
			}
		} else {
			s.Logger.Info().Str("taskId", req.TaskID).Msg("[PERSISTENT-AVATAR] Avatar preserved, bot process continues with original audio")
		}
	}

	// Clean up task deduplication map
	for taskKey, taskInfo := range activeTasksByKey {
		if taskInfo.TaskID == req.TaskID {
			delete(activeTasksByKey, taskKey)
			s.Logger.Info().
				Str("taskKey", taskKey).
				Str("taskID", req.TaskID).
				Msg("[PALABRA-STOP] Removed task from deduplication map")
		}
	}

	// Send success response
	respondWithJSON(w, http.StatusOK, PalabraStopResponse{
		Success: true,
	})
}

// Helper functions
func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

// PalabraTasks returns a list of active translation tasks
func (s *ServiceRouter) PalabraTasks(w http.ResponseWriter, r *http.Request) {
	tasks := make([]TaskInfo, 0)
	for _, task := range activeTasksByKey {
		tasks = append(tasks, *task)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"tasks":   tasks,
	})
}

// AvatarStart handles starting a standalone avatar (persistent avatar mode)
// The avatar subscribes to the source user's ORIGINAL audio (no translation yet)
func (s *ServiceRouter) AvatarStart(w http.ResponseWriter, r *http.Request) {
	s.Logger.Info().Msg("[AVATAR] Start avatar request received")

	// Parse request
	var req AvatarStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.Logger.Error().Err(err).Msg("[AVATAR] Failed to parse request body")
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if req.Channel == "" || req.SourceUID == "" {
		s.Logger.Error().Msg("[AVATAR] Missing required fields")
		respondWithError(w, http.StatusBadRequest, "Missing required fields: channel, sourceUid")
		return
	}

	s.Logger.Info().
		Str("channel", req.Channel).
		Str("sourceUid", req.SourceUID).
		Msg("[AVATAR] Starting avatar for source user")

	// Check if avatar already exists for this source
	avatarKey := fmt.Sprintf("%s:%s", req.Channel, req.SourceUID)
	if existing, exists := avatarSessions[avatarKey]; exists {
		s.Logger.Info().
			Str("avatarKey", avatarKey).
			Uint32("anamUID", existing.AnamUID).
			Msg("[AVATAR] Avatar already exists, returning existing session")

		respondWithJSON(w, http.StatusOK, AvatarStartResponse{
			Success:   true,
			SessionID: existing.SessionID,
			AnamUID:   existing.AnamUID,
		})
		return
	}

	// Check if Anam is enabled
	enableAnam := viper.GetBool("ENABLE_ANAM")
	if !enableAnam {
		s.Logger.Error().Msg("[AVATAR] Anam is not enabled")
		respondWithError(w, http.StatusBadRequest, "Avatar mode is not enabled (ENABLE_ANAM=false)")
		return
	}

	// Get credentials
	appID := viper.GetString("APP_ID")
	appCertificate := viper.GetString("APP_CERTIFICATE")
	avatarID := viper.GetString("ANAM_AVATAR_ID")
	anamAPIKey := viper.GetString("ANAM_API_KEY")
	anamBaseURL := viper.GetString("ANAM_BASE_URL")

	if appID == "" || appCertificate == "" {
		s.Logger.Error().Msg("[AVATAR] Missing Agora credentials")
		respondWithError(w, http.StatusInternalServerError, "Server configuration error: missing Agora credentials")
		return
	}

	if avatarID == "" || anamAPIKey == "" {
		s.Logger.Error().Msg("[AVATAR] Missing Anam credentials")
		respondWithError(w, http.StatusInternalServerError, "Server configuration error: missing Anam credentials")
		return
	}

	if anamBaseURL == "" {
		anamBaseURL = "https://api.anam.ai"
	}

	// Generate UIDs
	expireTime := uint32(time.Now().Unix()) + 3600*24 // 24 hours
	anamUIDNum := getNextAnamUID(req.Channel)
	botUIDNum := uint32(4500) // Bot UID for audio forwarding

	// Parse source UID
	var sourceUIDNum uint32
	fmt.Sscanf(req.SourceUID, "%d", &sourceUIDNum)

	// Generate token for Anam UID
	anamToken, err := rtctoken.BuildTokenWithUID(
		appID,
		appCertificate,
		req.Channel,
		anamUIDNum,
		rtctoken.RolePublisher,
		expireTime,
	)
	if err != nil {
		s.Logger.Error().Err(err).Msg("[AVATAR] Failed to generate Anam token")
		respondWithError(w, http.StatusInternalServerError, "Failed to generate Anam token")
		return
	}

	// Generate token for Bot UID
	botToken, err := rtctoken.BuildTokenWithUID(
		appID,
		appCertificate,
		req.Channel,
		botUIDNum,
		rtctoken.RoleSubscriber,
		expireTime,
	)
	if err != nil {
		s.Logger.Error().Err(err).Msg("[AVATAR] Failed to generate bot token")
		respondWithError(w, http.StatusInternalServerError, "Failed to generate bot token")
		return
	}

	// Generate unique session ID
	sessionID := fmt.Sprintf("avatar-%s-%s-%d", req.Channel, req.SourceUID, time.Now().UnixNano())

	// Start bot process
	// Key difference from translation: bot subscribes to sourceUIDNum (original user), not Palabra UID
	botManager := GetBotProcessManager()

	config := StartSessionConfig{
		TaskID:         sessionID,
		AppID:          appID,
		Channel:        req.Channel,
		BotUID:         botUIDNum,
		BotToken:       botToken,
		PalabraUID:     sourceUIDNum, // Subscribe to source user's audio, not Palabra
		AnamAPIKey:     anamAPIKey,
		AnamBaseURL:    anamBaseURL,
		AnamAvatarID:   avatarID,
		AnamUID:        anamUIDNum,
		AnamToken:      anamToken,
		TargetLanguage: "", // No target language for avatar-only mode
	}

	s.Logger.Info().
		Str("sessionID", sessionID).
		Uint32("sourceUID", sourceUIDNum).
		Uint32("anamUID", anamUIDNum).
		Uint32("botUID", botUIDNum).
		Msg("[AVATAR] Starting bot process for persistent avatar")

	proc, err := botManager.StartSession(config)
	if err != nil {
		s.Logger.Error().Err(err).Msg("[AVATAR] Failed to start bot process")
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to start avatar: %v", err))
		return
	}

	// Store avatar session
	avatarSessions[avatarKey] = &AvatarSession{
		SessionID:      sessionID,
		Channel:        req.Channel,
		SourceUID:      req.SourceUID,
		AnamUID:        anamUIDNum,
		BotUID:         botUIDNum,
		BotProcessID:   sessionID,
		HasTranslation: false,
		PalabraTaskID:  "",
		PalabraUID:     0,
	}

	s.Logger.Info().
		Str("sessionID", sessionID).
		Uint32("anamUID", anamUIDNum).
		Int("pid", proc.cmd.Process.Pid).
		Msg("[AVATAR] Avatar started successfully - bot subscribes to source user's original audio")

	respondWithJSON(w, http.StatusOK, AvatarStartResponse{
		Success:   true,
		SessionID: sessionID,
		AnamUID:   anamUIDNum,
	})
}

// AvatarStop handles stopping a standalone avatar
func (s *ServiceRouter) AvatarStop(w http.ResponseWriter, r *http.Request) {
	s.Logger.Info().Msg("[AVATAR] Stop avatar request received")

	// Parse request
	var req AvatarStopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.Logger.Error().Err(err).Msg("[AVATAR] Failed to parse request body")
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if req.Channel == "" || req.SourceUID == "" {
		s.Logger.Error().Msg("[AVATAR] Missing required fields")
		respondWithError(w, http.StatusBadRequest, "Missing required fields: channel, sourceUid")
		return
	}

	avatarKey := fmt.Sprintf("%s:%s", req.Channel, req.SourceUID)
	s.Logger.Info().
		Str("channel", req.Channel).
		Str("sourceUid", req.SourceUID).
		Str("avatarKey", avatarKey).
		Msg("[AVATAR] Stopping avatar")

	// Check if avatar exists
	session, exists := avatarSessions[avatarKey]
	if !exists {
		s.Logger.Warn().Str("avatarKey", avatarKey).Msg("[AVATAR] Avatar not found")
		respondWithJSON(w, http.StatusOK, AvatarStopResponse{
			Success: true, // Idempotent - already stopped
		})
		return
	}

	// If translation is active on this avatar, stop it first
	if session.HasTranslation && session.PalabraTaskID != "" {
		s.Logger.Info().
			Str("taskID", session.PalabraTaskID).
			Msg("[AVATAR] Avatar has active translation, stopping Palabra first")

		// Stop Palabra via API call (reuse existing logic)
		// For now, just clean up the task from our tracking
		for taskKey, taskInfo := range activeTasksByKey {
			if taskInfo.TaskID == session.PalabraTaskID {
				delete(activeTasksByKey, taskKey)
				s.Logger.Info().Str("taskKey", taskKey).Msg("[AVATAR] Cleaned up Palabra task")
			}
		}
	}

	// Stop bot process
	botManager := GetBotProcessManager()
	err := botManager.StopSession(session.SessionID)
	if err != nil {
		s.Logger.Error().Err(err).Str("sessionID", session.SessionID).Msg("[AVATAR] Failed to stop bot process")
		// Continue anyway - might already be stopped
	}

	// Remove from avatar sessions
	delete(avatarSessions, avatarKey)

	s.Logger.Info().
		Str("avatarKey", avatarKey).
		Str("sessionID", session.SessionID).
		Msg("[AVATAR] Avatar stopped successfully")

	respondWithJSON(w, http.StatusOK, AvatarStopResponse{
		Success: true,
	})
}

// GetAvatarSession returns the avatar session for a given channel and source UID
func GetAvatarSession(channel, sourceUID string) *AvatarSession {
	avatarKey := fmt.Sprintf("%s:%s", channel, sourceUID)
	return avatarSessions[avatarKey]
}

// UpdateAvatarTranslation updates the avatar session when translation starts/stops
func UpdateAvatarTranslation(channel, sourceUID string, hasTranslation bool, taskID string, palabraUID uint32) {
	avatarKey := fmt.Sprintf("%s:%s", channel, sourceUID)
	if session, exists := avatarSessions[avatarKey]; exists {
		session.HasTranslation = hasTranslation
		session.PalabraTaskID = taskID
		session.PalabraUID = palabraUID
	}
}
