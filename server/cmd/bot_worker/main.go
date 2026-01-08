// bot_worker is a child process that runs Agora SDK operations in isolation.
// If this process crashes (e.g., Agora SDK segfault), the parent HTTP server
// stays up and can handle the error gracefully.
package main

import (
	"io"
	"log"
	"os"
	"sync"

	"github.com/samyak-jain/agora_backend/services"
	"github.com/samyak-jain/agora_backend/services/ipc"
	"github.com/samyak-jain/agora_backend/services/ipc/botipc"
)

var (
	logger       *log.Logger
	stdoutWriter *ipc.MessageWriter
	stdoutLock   sync.Mutex

	// Original stdout for IPC (before redirect)
	originalStdout *os.File
)

func main() {
	// Save original stdout for IPC communication BEFORE redirecting
	originalStdout = os.Stdout

	// Redirect stdout to /dev/null to prevent Agora SDK from polluting IPC
	devNull, err := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		log.Fatalf("[bot_worker] Failed to open /dev/null: %v", err)
	}
	os.Stdout = devNull

	// Setup logging to stderr
	logger = log.New(os.Stderr, "[bot_worker] ", log.LstdFlags|log.Lshortfile)
	logger.Println("Bot worker process started")

	// Setup IPC writer using original stdout
	stdoutWriter = ipc.NewMessageWriter(originalStdout)

	// Setup IPC reader from stdin
	stdinReader := ipc.NewMessageReader(os.Stdin)

	// Main command loop
	runCommandLoop(stdinReader)

	logger.Println("Bot worker process exiting")
}

func runCommandLoop(reader *ipc.MessageReader) {
	var worker *services.BotWorker

	for {
		// Read next command from parent
		msgBytes, err := reader.ReadMessage()
		if err != nil {
			if err == io.EOF {
				logger.Println("Parent closed stdin, shutting down")
			} else {
				logger.Printf("Error reading from stdin: %v", err)
			}
			// Cleanup and exit
			if worker != nil {
				worker.Stop()
			}
			return
		}

		// Parse the IPC message
		msgType, payloadBytes, err := ipc.ParseIPCMessage(msgBytes)
		if err != nil {
			logger.Printf("Error parsing IPC message: %v", err)
			continue
		}

		switch msgType {
		case botipc.MessageTypeSTART_SESSION:
			if worker != nil {
				logger.Println("Session already running, ignoring START_SESSION")
				continue
			}

			payload := ipc.ParseStartSessionPayload(payloadBytes)
			taskID := string(payload.TaskId())

			logger.Printf("Received START_SESSION for task %s", taskID)

			// Send INITIALIZING status
			sendStatus(taskID, botipc.SessionStatusINITIALIZING, "Starting session", 0)

			// Create and start the worker
			config := services.BotWorkerConfig{
				TaskID:        taskID,
				AppID:         string(payload.AppId()),
				Channel:       string(payload.Channel()),
				BotUID:        payload.BotUid(),
				BotToken:      string(payload.BotToken()),
				PalabraUID:    payload.PalabraUid(),
				AnamAPIKey:    string(payload.AnamApiKey()),
				AnamBaseURL:   string(payload.AnamBaseUrl()),
				AnamAvatarID:  string(payload.AnamAvatarId()),
				AnamUID:       payload.AnamUid(),
				AnamToken:     string(payload.AnamToken()),
				TargetLanguage: string(payload.TargetLanguage()),
				StatusCallback: sendStatus,
				LogCallback:    sendLog,
				ErrorCallback:  sendError,
			}

			worker = services.NewBotWorker(config)

			// Start the worker in a goroutine
			go func() {
				err := worker.Run()
				if err != nil {
					logger.Printf("Worker failed: %v", err)
					sendError(taskID, "WORKER_FAILED", err.Error(), true)
				}
				// Worker finished, we should exit
				logger.Println("Worker finished, exiting")
				os.Exit(0)
			}()

		case botipc.MessageTypeSTOP_SESSION:
			payload := ipc.ParseStopSessionPayload(payloadBytes)
			taskID := string(payload.TaskId())
			reason := string(payload.Reason())

			logger.Printf("Received STOP_SESSION for task %s: %s", taskID, reason)

			if worker != nil {
				sendStatus(taskID, botipc.SessionStatusDISCONNECTING, "Stopping session", 0)
				worker.Stop()
				worker = nil
				sendStatus(taskID, botipc.SessionStatusDISCONNECTED, "Session stopped", 0)
			}

			// Exit after stop
			return

		default:
			logger.Printf("Unknown message type: %d", msgType)
		}
	}
}

// sendStatus sends a status update to the parent process
func sendStatus(taskID string, status botipc.SessionStatus, message string, anamUID uint32) {
	stdoutLock.Lock()
	defer stdoutLock.Unlock()

	msg := ipc.BuildStatusMessage(taskID, status, message, anamUID)
	if err := stdoutWriter.WriteMessage(msg); err != nil {
		logger.Printf("Failed to send status: %v", err)
	}
}

// sendLog sends a log message to the parent process
func sendLog(taskID string, level botipc.LogLevel, message string) {
	stdoutLock.Lock()
	defer stdoutLock.Unlock()

	msg := ipc.BuildLogMessage(taskID, level, message)
	if err := stdoutWriter.WriteMessage(msg); err != nil {
		logger.Printf("Failed to send log: %v", err)
	}
}

// sendError sends an error to the parent process
func sendError(taskID, errorCode, message string, fatal bool) {
	stdoutLock.Lock()
	defer stdoutLock.Unlock()

	msg := ipc.BuildErrorMessage(taskID, errorCode, message, fatal)
	if err := stdoutWriter.WriteMessage(msg); err != nil {
		logger.Printf("Failed to send error: %v", err)
	}
}
