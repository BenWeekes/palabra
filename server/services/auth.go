// ********************************************
// Copyright © 2021 Agora Lab, Inc., all rights reserved.
// AppBuilder and all associated components, source code, APIs, services, and documentation
// (the "Materials") are owned by Agora Lab, Inc. and its licensors.  The Materials may not be
// accessed, used, modified, or distributed for any purpose without a license from Agora Lab, Inc.
// Use without a license or in violation of any license terms and conditions (including use for
// any purpose competitive to Agora Lab, Inc.'s business) is strictly prohibited.  For more
// information visit https://appbuilder.agora.io.
// *********************************************

package services

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/samyak-jain/agora_backend/utils/rtctoken"
	"github.com/samyak-jain/agora_backend/utils/rtmtoken"
	"github.com/spf13/viper"
)

// UserDetails - Stub endpoint for local development
// Returns dummy user details to bypass authentication
func (req *ServiceRouter) UserDetails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"uid":      "local-dev-user",
			"name":     "Local Dev User",
			"email":    "dev@localhost",
			"verified": true,
		},
	}

	json.NewEncoder(w).Encode(response)
}

// Login - Stub endpoint for local development
// Returns dummy auth token to bypass authentication
func (req *ServiceRouter) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"success": true,
		"token":   "local-dev-token",
		"uid":     "local-dev-user",
	}

	json.NewEncoder(w).Encode(response)
}

// CreateChannel - Stub endpoint for local development
// Returns dummy channel details to bypass backend channel creation
func (req *ServiceRouter) CreateChannel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Generate random passphrases
	rand.Seed(time.Now().UnixNano())
	hostPassphrase := generatePassphrase()
	viewerPassphrase := generatePassphrase()

	response := map[string]interface{}{
		"channel":            hostPassphrase,
		"host_pass_phrase":   hostPassphrase,
		"viewer_pass_phrase": viewerPassphrase,
	}

	json.NewEncoder(w).Encode(response)
}

// JoinChannel - Stub endpoint for local development
// Returns channel join details with real RTC tokens
func (req *ServiceRouter) JoinChannel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse request body to get the passphrase
	var requestBody struct {
		Passphrase string `json:"passphrase"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		req.Logger.Error().Err(err).Msg("Failed to decode request body")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if requestBody.Passphrase == "" {
		req.Logger.Error().Msg("Passphrase is empty")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Passphrase is required"})
		return
	}

	// Get Agora credentials
	appID := viper.GetString("APP_ID")
	appCertificate := viper.GetString("APP_CERTIFICATE")

	// Use the passphrase from request as channel name
	channelName := requestBody.Passphrase

	// Generate UID, ensuring it doesn't fall in reserved Palabra range (3000-3099)
	const palabraUIDMin = 3000
	const palabraUIDMax = 3099
	var uid uint32
	for {
		uid = uint32(rand.Intn(90000) + 10000) // UID between 10000-99999
		// Verify not in reserved range (safety check, shouldn't happen with current range)
		if uid < palabraUIDMin || uid > palabraUIDMax {
			break
		}
	}
	screenShareUid := uid + 1

	// Token expiration (24 hours)
	expireTime := uint32(time.Now().Unix()) + 3600*24

	// Generate RTC token for main user
	rtcToken, err := rtctoken.BuildTokenWithUID(
		appID,
		appCertificate,
		channelName,
		uid,
		rtctoken.RolePublisher,
		expireTime,
	)
	if err != nil {
		req.Logger.Error().Err(err).Msg("Failed to generate RTC token for main user")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate token"})
		return
	}

	// Generate RTM token for main user (RTM uses string UID)
	// NOTE: RTM uses the same APP_ID and APP_CERTIFICATE as RTC
	// The v007 token builder supports both services
	rtmToken, err := rtmtoken.BuildToken(
		appID,
		appCertificate,
		fmt.Sprintf("%d", uid),
		rtmtoken.RoleRtmUser,
		expireTime,
	)
	if err != nil {
		req.Logger.Error().Err(err).Msg("Failed to generate RTM token")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate RTM token"})
		return
	}

	// Generate RTC token for screen share user
	screenShareToken, err := rtctoken.BuildTokenWithUID(
		appID,
		appCertificate,
		channelName,
		screenShareUid,
		rtctoken.RolePublisher,
		expireTime,
	)
	if err != nil {
		req.Logger.Error().Err(err).Msg("Failed to generate screenshare token")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate screenshare token"})
		return
	}

	response := map[string]interface{}{
		"channel_name": channelName,
		"main_user": map[string]interface{}{
			"uid": uid,
			"rtc": rtcToken,
			"rtm": rtmToken,
		},
		"screen_share_user": map[string]interface{}{
			"uid": screenShareUid,
			"rtc": screenShareToken,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ShareChannel - Stub endpoint for local development
// Returns meeting details from a passphrase
func (req *ServiceRouter) ShareChannel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var requestBody struct {
		Passphrase string `json:"passphrase"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	// For local dev, just return the passphrase as both host and attendee
	response := map[string]interface{}{
		"passphrases": map[string]string{
			"host":     requestBody.Passphrase,
			"attendee": requestBody.Passphrase,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func generatePassphrase() string {
	// Generate UUID-like passphrase to match original behavior
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		rand.Uint32(),
		rand.Uint32()&0xffff,
		rand.Uint32()&0xffff,
		rand.Uint32()&0xffff,
		rand.Uint64()&0xffffffffffff,
	)
}
