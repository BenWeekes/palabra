package services

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/samyak-jain/agora_backend/services/ipc/botipc"
)

// Default idle timeout in seconds (stop session if no audio for this long)
const DefaultIdleTimeoutSeconds = 60

// StatusCallback is called when session status changes
type StatusCallback func(taskID string, status botipc.SessionStatus, message string, anamUID uint32)

// LogCallback is called to send log messages to parent
type LogCallback func(taskID string, level botipc.LogLevel, message string)

// ErrorCallback is called when an error occurs
type ErrorCallback func(taskID, errorCode, message string, fatal bool)

// BotWorkerConfig contains all configuration needed to start a bot session
type BotWorkerConfig struct {
	TaskID         string
	AppID          string
	Channel        string
	BotUID         uint32
	BotToken       string
	PalabraUID     uint32
	AnamAPIKey     string
	AnamBaseURL    string
	AnamAvatarID   string
	AnamUID        uint32
	AnamToken      string
	TargetLanguage string

	// Callbacks for IPC
	StatusCallback StatusCallback
	LogCallback    LogCallback
	ErrorCallback  ErrorCallback
}

// BotWorker orchestrates AgoraBot and AnamClient in the child process
type BotWorker struct {
	config     BotWorkerConfig
	agoraBot   *AgoraBot
	anamClient *AnamClient
	stopChan   chan struct{}
	mu         sync.Mutex
	isRunning  bool
}

// NewBotWorker creates a new BotWorker instance
func NewBotWorker(config BotWorkerConfig) *BotWorker {
	return &BotWorker{
		config:   config,
		stopChan: make(chan struct{}),
	}
}

// Run starts the bot worker and blocks until stopped or error
func (w *BotWorker) Run() error {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return fmt.Errorf("worker already running")
	}
	w.isRunning = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.isRunning = false
		w.mu.Unlock()
	}()

	w.log(botipc.LogLevelINFO, "Starting bot worker for task %s", w.config.TaskID)

	// Step 1: Create and connect Anam client
	w.sendStatus(botipc.SessionStatusCONNECTING_ANAM, "Connecting to Anam API", 0)

	w.anamClient = NewAnamClient(
		w.config.AnamAvatarID,
		w.config.AppID,
		w.config.Channel,
		fmt.Sprintf("%d", w.config.AnamUID),
		w.config.AnamToken,
		w.config.AnamBaseURL,
		w.config.AnamAPIKey,
	)

	// Start Anam session (this connects to Anam API and WebSocket)
	if err := w.anamClient.StartSession(); err != nil {
		errMsg := fmt.Sprintf("Failed to start Anam session: %v", err)
		w.log(botipc.LogLevelERROR, errMsg)
		w.sendError("ANAM_CONNECT_FAILED", errMsg, true)
		return fmt.Errorf(errMsg)
	}

	w.log(botipc.LogLevelINFO, "Anam client connected")

	// Step 2: Create and start Agora bot
	w.sendStatus(botipc.SessionStatusCONNECTING_AGORA, "Connecting to Agora RTC", 0)

	w.agoraBot = NewAgoraBot(
		w.config.AppID,
		w.config.Channel,
		fmt.Sprintf("%d", w.config.BotUID),
		w.config.BotToken,
		fmt.Sprintf("%d", w.config.PalabraUID),
		w.anamClient, // Pass AnamClient reference
	)

	if err := w.agoraBot.Start(); err != nil {
		errMsg := fmt.Sprintf("Failed to start Agora bot: %v", err)
		w.log(botipc.LogLevelERROR, errMsg)
		w.sendError("AGORA_CONNECT_FAILED", errMsg, true)
		// Cleanup Anam
		w.anamClient.Close()
		return fmt.Errorf(errMsg)
	}

	w.log(botipc.LogLevelINFO, "Agora bot connected and subscribed to UID %d", w.config.PalabraUID)

	// Step 3: Send connected status with Anam UID
	w.sendStatus(botipc.SessionStatusCONNECTED, "Session connected", w.config.AnamUID)
	w.sendStatus(botipc.SessionStatusSTREAMING, "Audio streaming active", w.config.AnamUID)

	// Get idle timeout from environment (default 60 seconds)
	idleTimeoutSeconds := DefaultIdleTimeoutSeconds
	if envTimeout := os.Getenv("PALABRA_IDLE_TIMEOUT_SECONDS"); envTimeout != "" {
		if parsed, err := strconv.Atoi(envTimeout); err == nil && parsed > 0 {
			idleTimeoutSeconds = parsed
		}
	}
	idleTimeout := time.Duration(idleTimeoutSeconds) * time.Second
	w.log(botipc.LogLevelINFO, "Bot worker running, idle timeout: %v", idleTimeout)

	// Step 4: Wait for stop signal, target left, or idle timeout
	idleCheckTicker := time.NewTicker(10 * time.Second) // Check every 10 seconds
	defer idleCheckTicker.Stop()

	for {
		select {
		case <-w.stopChan:
			w.log(botipc.LogLevelINFO, "Received stop signal")
			goto cleanup
		case <-w.agoraBot.TargetLeftChan():
			// Palabra bot (target UID) left the channel - no point continuing
			w.log(botipc.LogLevelWARN, "Palabra bot (UID %d) left channel - auto-stopping", w.config.PalabraUID)
			w.sendError("TARGET_LEFT", fmt.Sprintf("Palabra bot UID %d left channel", w.config.PalabraUID), true)
			goto cleanup
		case <-idleCheckTicker.C:
			// Check if we've been idle too long
			if w.agoraBot != nil {
				idleDuration := w.agoraBot.GetIdleDuration()
				if idleDuration > idleTimeout {
					w.log(botipc.LogLevelWARN, "Session idle for %v (timeout: %v) - auto-stopping", idleDuration, idleTimeout)
					w.sendError("IDLE_TIMEOUT", fmt.Sprintf("No audio activity for %v", idleDuration), true)
					goto cleanup
				}
			}
		}
	}

cleanup:
	// Step 5: Cleanup
	w.log(botipc.LogLevelINFO, "Stopping bot worker")
	w.cleanup()

	return nil
}

// Stop signals the worker to stop
func (w *BotWorker) Stop() {
	w.mu.Lock()
	if !w.isRunning {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	close(w.stopChan)
}

// cleanup stops all components
func (w *BotWorker) cleanup() {
	if w.agoraBot != nil {
		w.log(botipc.LogLevelINFO, "Stopping Agora bot")
		w.agoraBot.Stop()
		w.agoraBot = nil
	}

	if w.anamClient != nil {
		w.log(botipc.LogLevelINFO, "Closing Anam client")
		w.anamClient.Close()
		w.anamClient = nil
	}
}

// sendStatus sends a status update via callback
func (w *BotWorker) sendStatus(status botipc.SessionStatus, message string, anamUID uint32) {
	if w.config.StatusCallback != nil {
		w.config.StatusCallback(w.config.TaskID, status, message, anamUID)
	}
}

// sendError sends an error via callback
func (w *BotWorker) sendError(errorCode, message string, fatal bool) {
	if w.config.ErrorCallback != nil {
		w.config.ErrorCallback(w.config.TaskID, errorCode, message, fatal)
	}
}

// log sends a log message via callback
func (w *BotWorker) log(level botipc.LogLevel, format string, args ...interface{}) {
	if w.config.LogCallback != nil {
		message := fmt.Sprintf(format, args...)
		w.config.LogCallback(w.config.TaskID, level, message)
	}
}
