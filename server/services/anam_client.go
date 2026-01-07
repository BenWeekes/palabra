package services

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
)

// AnamClient handles communication with Anam API
type AnamClient struct {
	conn          *websocket.Conn
	avatarID      string
	appID         string
	channel       string
	anamUID       string
	token         string
	sessionToken  string
	sessionID     string
	wsAddress     string
	mu            sync.Mutex
	isConnected   bool
	stopChan      chan struct{}
}

// AnamSessionTokenRequest represents the session token request
// Per anam_api_flow.md: HTTP request includes ALL config (camelCase)
type AnamSessionTokenRequest struct {
	PersonaConfig struct {
		AvatarID string `json:"avatarId"`
	} `json:"personaConfig"`
	Environment struct {
		AgoraSettings struct {
			AppID              string `json:"appId"`
			Token              string `json:"token"`
			Channel            string `json:"channel"`
			UID                string `json:"uid"`
			Quality            string `json:"quality"`
			VideoEncoding      string `json:"videoEncoding"`
			EnableStringUIDs   bool   `json:"enableStringUids"`
			ActivityIdleTimeout int    `json:"activityIdleTimeout"`
		} `json:"agoraSettings"`
	} `json:"environment"`
}

// AnamSessionTokenResponse represents the session token response
type AnamSessionTokenResponse struct {
	SessionToken string `json:"sessionToken"`
}

// AnamSessionResponse represents the engine session response
type AnamSessionResponse struct {
	SessionID         string `json:"sessionId"`
	WebsocketAddress  string `json:"websocketAddress"`
	WebsocketURL      string `json:"websocketUrl"`
	WebSocketAddress  string `json:"webSocketAddress"`
	WebSocketURL      string `json:"webSocketUrl"`
}

// NewAnamClient creates a new Anam client
func NewAnamClient(avatarID, appID, channel, anamUID, token string) *AnamClient {
	return &AnamClient{
		avatarID:    avatarID,
		appID:       appID,
		channel:     channel,
		anamUID:     anamUID,
		token:       token,
		isConnected: false,
		stopChan:    make(chan struct{}),
	}
}

// Connect creates an Anam session (calls auth/session-token then engine/session)
func (c *AnamClient) Connect() error {
	// This will be called in StartSession with Agora config
	return nil
}

// StartSession creates an Anam streaming session and connects WebSocket
func (c *AnamClient) StartSession() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isConnected {
		return fmt.Errorf("already connected")
	}

	baseURL := viper.GetString("ANAM_BASE_URL")
	apiKey := viper.GetString("ANAM_API_KEY")

	if baseURL == "" || apiKey == "" {
		return fmt.Errorf("ANAM_BASE_URL or ANAM_API_KEY not configured")
	}

	// Step 1: Get session token
	// Per anam_api_flow.md: HTTP request includes ALL config
	tokenURL := fmt.Sprintf("%s/auth/session-token", baseURL)

	// Get quality and video encoding from config
	quality := viper.GetString("ANAM_QUALITY")
	if quality == "" {
		quality = "high"
	}
	videoEncoding := viper.GetString("ANAM_VIDEO_ENCODING")
	if videoEncoding == "" {
		videoEncoding = "H264"
	}

	tokenReq := AnamSessionTokenRequest{}
	tokenReq.PersonaConfig.AvatarID = c.avatarID
	tokenReq.Environment.AgoraSettings.AppID = c.appID
	tokenReq.Environment.AgoraSettings.Token = c.token
	tokenReq.Environment.AgoraSettings.Channel = c.channel
	tokenReq.Environment.AgoraSettings.UID = c.anamUID
	tokenReq.Environment.AgoraSettings.Quality = quality
	tokenReq.Environment.AgoraSettings.VideoEncoding = videoEncoding
	tokenReq.Environment.AgoraSettings.EnableStringUIDs = false
	tokenReq.Environment.AgoraSettings.ActivityIdleTimeout = 120

	jsonData, err := json.Marshal(tokenReq)
	if err != nil {
		return fmt.Errorf("failed to marshal token request: %w", err)
	}

	fmt.Printf("[Anam] Getting session token at %s\n", tokenURL)
	fmt.Printf("[Anam] Token request body: %s\n", string(jsonData))

	httpReq, err := http.NewRequest("POST", tokenURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to get session token: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		fmt.Printf("[Anam] Token request failed: %d %s - %s\n", resp.StatusCode, resp.Status, string(body))
		return fmt.Errorf("token request failed: %d %s", resp.StatusCode, resp.Status)
	}

	var tokenResp AnamSessionTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	c.sessionToken = tokenResp.SessionToken
	fmt.Printf("[Anam] Got session token\n")

	// Step 2: Create engine session
	sessionURL := fmt.Sprintf("%s/engine/session", baseURL)

	sessionReq := map[string]interface{}{}
	sessionData, err := json.Marshal(sessionReq)
	if err != nil {
		return fmt.Errorf("failed to marshal session request: %w", err)
	}

	fmt.Printf("[Anam] Creating engine session at %s\n", sessionURL)
	fmt.Printf("[Anam] Engine session request body: %s\n", string(sessionData))

	httpReq2, err := http.NewRequest("POST", sessionURL, bytes.NewBuffer(sessionData))
	if err != nil {
		return fmt.Errorf("failed to create session request: %w", err)
	}

	httpReq2.Header.Set("Content-Type", "application/json")
	httpReq2.Header.Set("Accept", "application/json")
	httpReq2.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.sessionToken))

	resp2, err := httpClient.Do(httpReq2)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer resp2.Body.Close()

	body2, err := ioutil.ReadAll(resp2.Body)
	if err != nil {
		return fmt.Errorf("failed to read session response: %w", err)
	}

	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusCreated {
		fmt.Printf("[Anam] Session creation failed: %d %s - %s\n", resp2.StatusCode, resp2.Status, string(body2))
		return fmt.Errorf("session creation failed: %d %s", resp2.StatusCode, resp2.Status)
	}

	fmt.Printf("[Anam] Engine session response body: %s\n", string(body2))

	var sessionResp AnamSessionResponse
	if err := json.Unmarshal(body2, &sessionResp); err != nil {
		return fmt.Errorf("failed to parse session response: %w", err)
	}

	c.sessionID = sessionResp.SessionID

	// Try different field names for WebSocket URL (Anam API inconsistency)
	if sessionResp.WebsocketAddress != "" {
		c.wsAddress = sessionResp.WebsocketAddress
	} else if sessionResp.WebsocketURL != "" {
		c.wsAddress = sessionResp.WebsocketURL
	} else if sessionResp.WebSocketAddress != "" {
		c.wsAddress = sessionResp.WebSocketAddress
	} else if sessionResp.WebSocketURL != "" {
		c.wsAddress = sessionResp.WebSocketURL
	}

	// Per TEN framework: use WebSocket URL as-is, no cleanup needed
	fmt.Printf("[Anam] Session created: %s, WebSocket: %s\n", c.sessionID, c.wsAddress)

	// Step 3: Connect to WebSocket
	if c.wsAddress != "" {
		// gorilla/websocket doesn't follow redirects, but Python websockets does
		// We need to handle 301 manually by following Location header
		dialer := &websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}

		headers := http.Header{}
		headers.Set("User-Agent", "Go-http-client/1.1")

		fmt.Printf("[Anam] Connecting to WebSocket: %s\n", c.wsAddress)
		conn, resp, err := dialer.Dial(c.wsAddress, headers)

		// If we get a redirect, follow it
		if err != nil && resp != nil && (resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 307 || resp.StatusCode == 308) {
			location := resp.Header.Get("Location")
			if location != "" {
				// If location is relative, make it absolute
				if location[0] == '/' {
					// Extract host from original URL: wss://connect-eu.anam.ai/...
					// Split on "//" then get the part before the next "/"
					parts := strings.Split(c.wsAddress, "//")
					if len(parts) >= 2 {
						hostParts := strings.SplitN(parts[1], "/", 2)
						location = "wss://" + hostParts[0] + location
					}
				}
				fmt.Printf("[Anam] Following redirect to: %s\n", location)
				conn, resp, err = dialer.Dial(location, headers)
			}
		}

		if err != nil {
			if resp != nil {
				fmt.Printf("[Anam] WebSocket handshake failed: %d %s\n", resp.StatusCode, resp.Status)
				if resp.Body != nil {
					bodyBytes, _ := ioutil.ReadAll(resp.Body)
					fmt.Printf("[Anam] Response body: %s\n", string(bodyBytes))
				}
			}
			return fmt.Errorf("failed to connect to WebSocket: %w", err)
		}

		c.conn = conn
		c.isConnected = true

		fmt.Printf("[Anam] Connected to Anam WebSocket\n")

		// Step 4: Send "init" command with full configuration (per anam_api_flow.md)
		// WebSocket uses snake_case
		initMsg := map[string]interface{}{
			"command":               "init",
			"event_id":              uuid.Must(uuid.NewV4()).String(), // REQUIRED per working version
			"session_id":            c.sessionID,
			"avatar_id":             c.avatarID,
			"quality":               quality,
			"version":               "1.0",
			"video_encoding":        videoEncoding,
			"activity_idle_timeout": 120,
			"agora_settings": map[string]interface{}{
				"app_id":            c.appID,
				"token":             c.token,
				"channel":           c.channel,
				"uid":               c.anamUID,
				"enable_string_uid": false,
			},
		}

		initMsgJSON, _ := json.Marshal(initMsg)
		fmt.Printf("[Anam] 📤 Sending init - Avatar will join as UID %s in channel %s\n", c.anamUID, c.channel)
	fmt.Printf("[Anam] Init command: %s\n", string(initMsgJSON))

		if err := conn.WriteJSON(initMsg); err != nil {
			return fmt.Errorf("failed to send init command: %w", err)
		}

		fmt.Printf("[Anam] Init command sent successfully\n")

		// CRITICAL: Wait 500ms after init before starting heartbeat/audio
		// Per anam_ws_flow.md: "give Anam time to set up before sending audio"
		fmt.Printf("[Anam] Waiting 500ms for Anam to initialize...\n")
		time.Sleep(500 * time.Millisecond)
		fmt.Printf("[Anam] Init delay complete, starting heartbeat\n")

		// Start heartbeat to keep connection alive (required by Anam)
		go c.sendHeartbeat()

		// Start listening for messages from Anam
		go c.receiveLoop()
	} else {
		return fmt.Errorf("no WebSocket address provided by Anam")
	}

	return nil
}

// SendAudio sends base64-encoded PCM audio to Anam with default 16kHz sample rate
func (c *AnamClient) SendAudio(audioB64 string) error {
	return c.SendAudioWithSampleRate(audioB64, 16000)
}

// SendAudioWithSampleRate sends base64-encoded PCM audio to Anam with specified sample rate
func (c *AnamClient) SendAudioWithSampleRate(audioB64 string, sampleRate int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected || c.conn == nil {
		return fmt.Errorf("not connected to Anam")
	}

	// Per anam_api_flow.md: WebSocket uses snake_case, "command" not "kind", "audio" not "stream"
	msg := map[string]interface{}{
		"command":     "voice",
		"audio":       audioB64,
		"sample_rate": sampleRate,
		"encoding":    "PCM16",
		"event_id":    uuid.Must(uuid.NewV4()).String(),
	}

	return c.conn.WriteJSON(msg)
}

// SendVoiceEnd sends voice_end signal to Anam (called after silence detected)
func (c *AnamClient) SendVoiceEnd() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected || c.conn == nil {
		return fmt.Errorf("not connected to Anam")
	}

	msg := map[string]interface{}{
		"command":  "voice_end",
		"event_id": uuid.Must(uuid.NewV4()).String(),
	}

	fmt.Printf("[Anam] Sending voice_end signal\n")
	return c.conn.WriteJSON(msg)
}

// receiveLoop continuously receives messages from Anam
func (c *AnamClient) receiveLoop() {
	fmt.Printf("[Anam] Starting receive loop\n")

	for {
		select {
		case <-c.stopChan:
			fmt.Printf("[Anam] Stopping receive loop\n")
			return
		default:
			if c.conn == nil {
				return
			}

			var msg map[string]interface{}
			err := c.conn.ReadJSON(&msg)
			if err != nil {
				fmt.Printf("[Anam] Error reading message: %v\n", err)
				return
			}

			// Log ALL messages from Anam for debugging
			msgType, ok := msg["type"].(string)
			if ok {
				fmt.Printf("[Anam] Received message type: %s, full: %+v\n", msgType, msg)
			} else {
				fmt.Printf("[Anam] Received message (no type field): %+v\n", msg)
			}

			// Check for error messages
			if errMsg, ok := msg["error"].(string); ok && errMsg != "" {
				fmt.Printf("[Anam] ERROR from server: %s\n", errMsg)
			}
		}
	}
}

// sendHeartbeat sends periodic heartbeat to keep WebSocket connection alive
func (c *AnamClient) sendHeartbeat() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	fmt.Printf("[Anam] Starting heartbeat sender (every 5 seconds)\n")

	for {
		select {
		case <-c.stopChan:
			fmt.Printf("[Anam] Stopping heartbeat sender\n")
			return
		case <-ticker.C:
			c.mu.Lock()
			if !c.isConnected || c.conn == nil {
				c.mu.Unlock()
				return
			}

			heartbeat := map[string]interface{}{
				"command":   "heartbeat",
				"event_id":  uuid.Must(uuid.NewV4()).String(),
				"timestamp": time.Now().UnixMilli(),
			}

			err := c.conn.WriteJSON(heartbeat)
			c.mu.Unlock()

			if err != nil {
				fmt.Printf("[Anam] Error sending heartbeat: %v\n", err)
			} else {
				fmt.Printf("[Anam] Sent heartbeat\n")
			}
		}
	}
}

// Close gracefully closes the Anam connection
func (c *AnamClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected {
		return nil
	}

	if c.conn != nil {
		close(c.stopChan)

		// Send close message
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		c.conn.WriteMessage(websocket.CloseMessage, closeMsg)

		// Close connection
		c.conn.Close()
	}

	c.isConnected = false

	fmt.Printf("[Anam] Connection closed\n")

	return nil
}

// IsConnected returns whether the client is connected
func (c *AnamClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isConnected
}
