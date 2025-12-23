package services

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/samyak-jain/agora_backend/utils/rtctoken"
	"github.com/spf13/viper"
)

// PalabraStartRequest represents the request to start translation
type PalabraStartRequest struct {
	Channel          string   `json:"channel"`
	SourceUID        string   `json:"sourceUid"`
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
)

// ActiveTask represents a translation task in the registry
type ActiveTask struct {
	TaskID         string    `json:"taskId"`
	Channel        string    `json:"channel"`
	SourceUID      string    `json:"sourceUid"`
	SourceLanguage string    `json:"sourceLanguage"`
	TargetLanguage string    `json:"targetLanguage"`
	TranslationUID string    `json:"translationUid"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Global registry for active translation tasks
// Key format: "channel:sourceUid:targetLang"
var activeTasks sync.Map

// Global UID counter for translation streams (atomic operations)
var uidCounter uint32 = transUIDBase

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

	// Validate required fields
	if req.Channel == "" || req.SourceUID == "" || req.SourceLanguage == "" || len(req.TargetLanguages) == 0 {
		s.Logger.Error().Msg("Missing required fields")
		respondWithError(w, http.StatusBadRequest, "Missing required fields: channel, sourceUid, sourceLanguage, targetLanguages")
		return
	}

	// Check if task already exists in registry
	targetLang := req.TargetLanguages[0] // We only support single target language per request
	registryKey := fmt.Sprintf("%s:%s:%s", req.Channel, req.SourceUID, targetLang)

	if existing, ok := activeTasks.Load(registryKey); ok {
		task := existing.(ActiveTask)
		s.Logger.Info().
			Str("registryKey", registryKey).
			Str("taskId", task.TaskID).
			Str("translationUid", task.TranslationUID).
			Msg("Reusing existing translation task")

		// Return existing task (no Palabra API call)
		respondWithJSON(w, http.StatusOK, PalabraStartResponse{
			Success: true,
			TaskID:  task.TaskID,
			Streams: []PalabraStreamInfo{
				{
					UID:      task.TranslationUID,
					Language: task.TargetLanguage,
				},
			},
		})
		return
	}

	s.Logger.Info().Str("registryKey", registryKey).Msg("No existing task found, creating new translation task")

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
			Options:        make(map[string]interface{}),
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

	// Store in registry
	activeTask := ActiveTask{
		TaskID:         taskID,
		Channel:        req.Channel,
		SourceUID:      req.SourceUID,
		SourceLanguage: req.SourceLanguage,
		TargetLanguage: targetLang,
		TranslationUID: streams[0].UID,
		CreatedAt:      time.Now(),
	}
	activeTasks.Store(registryKey, activeTask)

	s.Logger.Info().
		Str("registryKey", registryKey).
		Str("taskId", taskID).
		Str("translationUid", streams[0].UID).
		Msg("Stored translation task in registry")

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

	// Remove task from registry
	var removedKey string
	activeTasks.Range(func(key, value interface{}) bool {
		task := value.(ActiveTask)
		if task.TaskID == req.TaskID {
			removedKey = key.(string)
			return false // Stop iteration
		}
		return true // Continue iteration
	})

	if removedKey != "" {
		activeTasks.Delete(removedKey)
		s.Logger.Info().
			Str("registryKey", removedKey).
			Str("taskId", req.TaskID).
			Msg("Removed translation task from registry")
	} else {
		s.Logger.Warn().Str("taskId", req.TaskID).Msg("Task not found in registry (may have been already removed)")
	}

	// Send success response
	respondWithJSON(w, http.StatusOK, PalabraStopResponse{
		Success: true,
	})
}

// PalabraTasks handles retrieving active translation tasks for a channel
func (s *ServiceRouter) PalabraTasks(w http.ResponseWriter, r *http.Request) {
	// Get channel from query parameter
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		s.Logger.Error().Msg("Missing channel parameter")
		respondWithError(w, http.StatusBadRequest, "Missing required parameter: channel")
		return
	}

	s.Logger.Info().Str("channel", channel).Msg("Fetching active translation tasks")

	// Collect all tasks for this channel
	var tasks []ActiveTask

	activeTasks.Range(func(key, value interface{}) bool {
		task := value.(ActiveTask)
		if task.Channel == channel {
			tasks = append(tasks, task)
		}
		return true // Continue iteration
	})

	s.Logger.Info().Int("count", len(tasks)).Str("channel", channel).Msg("Found active translation tasks")

	// Return tasks
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
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
