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
}

var (
	// Per-channel counters for Anam UIDs (channel -> next available UID)
	channelAnamCounters = make(map[string]uint32)
	// Task deduplication: map key is "channel:sourceUid:targetLanguage"
	activeTasksByKey = make(map[string]*TaskInfo)
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
		uid := transUIDBase + uint32(i)
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

	// NEW: Check if Anam is enabled
	enableAnam := viper.GetBool("ENABLE_ANAM")

	if enableAnam {
		s.Logger.Info().Msg("Anam is enabled, starting avatar bot")

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

	// Store task info for deduplication
	for _, targetLang := range req.TargetLanguages {
		taskKey := fmt.Sprintf("%s:%s:%s", req.Channel, req.SourceUID, targetLang)
		activeTasksByKey[taskKey] = &TaskInfo{
			TaskID:    taskID,
			Streams:   streams,
			SourceUID: req.SourceUID,
			Channel:   req.Channel,
			Language:  targetLang,
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
