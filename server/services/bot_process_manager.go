package services

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/samyak-jain/agora_backend/services/ipc"
	"github.com/samyak-jain/agora_backend/services/ipc/botipc"
	"github.com/spf13/viper"
)

// Default session timeout in minutes
const DefaultSessionTimeoutMinutes = 10

// BotProcess represents a running child process
type BotProcess struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	stdinWriter  *ipc.MessageWriter
	TaskID       string
	Status       botipc.SessionStatus
	AnamUID      uint32
	StartTime    time.Time
	mu           sync.RWMutex
	shutdownChan chan struct{}
	timeoutTimer *time.Timer
}

// BotProcessManager manages child bot processes
type BotProcessManager struct {
	processes      map[string]*BotProcess // taskID -> process
	mu             sync.RWMutex
	logger         *log.Logger
	workerPath     string        // Path to bot_worker binary
	sessionTimeout time.Duration // Max session duration
	shutdownChan   chan struct{}
}

// StartSessionConfig contains configuration for starting a bot session
type StartSessionConfig struct {
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
}

// Global instance (initialized once)
var (
	globalBotManager     *BotProcessManager
	globalBotManagerOnce sync.Once
)

// GetBotProcessManager returns the global BotProcessManager instance
func GetBotProcessManager() *BotProcessManager {
	globalBotManagerOnce.Do(func() {
		globalBotManager = NewBotProcessManager()
	})
	return globalBotManager
}

// NewBotProcessManager creates a new BotProcessManager
func NewBotProcessManager() *BotProcessManager {
	// Look for bot_worker in same directory as server, or in PATH
	workerPath := "./bot_worker"
	if _, err := os.Stat(workerPath); os.IsNotExist(err) {
		workerPath = "/go/bin/bot_worker"
	}

	// Read session timeout from config (default 10 minutes)
	timeoutMinutes := viper.GetInt("PALABRA_SESSION_TIMEOUT_MINUTES")
	if timeoutMinutes <= 0 {
		timeoutMinutes = DefaultSessionTimeoutMinutes
	}
	sessionTimeout := time.Duration(timeoutMinutes) * time.Minute

	logger := log.New(os.Stderr, "[BotProcessManager] ", log.LstdFlags|log.Lshortfile)
	logger.Printf("Session timeout configured: %v", sessionTimeout)

	return &BotProcessManager{
		processes:      make(map[string]*BotProcess),
		logger:         logger,
		workerPath:     workerPath,
		sessionTimeout: sessionTimeout,
		shutdownChan:   make(chan struct{}),
	}
}

// StartSession spawns a new child process for a translation session
func (m *BotProcessManager) StartSession(config StartSessionConfig) (*BotProcess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if session already exists
	if existing, ok := m.processes[config.TaskID]; ok {
		return existing, fmt.Errorf("session already exists for task %s", config.TaskID)
	}

	m.logger.Printf("Starting session for task %s", config.TaskID)

	// Create child process command
	cmd := exec.Command(m.workerPath)

	// Setup pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Inherit environment variables (for Agora SDK libs)
	cmd.Env = append(os.Environ(),
		"LD_LIBRARY_PATH=/usr/local/lib:/go/agora_sdk",
	)

	// Start the child process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("failed to start child process: %w", err)
	}

	m.logger.Printf("Child process started with PID %d for task %s", cmd.Process.Pid, config.TaskID)

	// Create process record
	proc := &BotProcess{
		cmd:          cmd,
		stdin:        stdin,
		stdout:       stdout,
		stderr:       stderr,
		stdinWriter:  ipc.NewMessageWriter(stdin),
		TaskID:       config.TaskID,
		Status:       botipc.SessionStatusINITIALIZING,
		StartTime:    time.Now(),
		shutdownChan: make(chan struct{}),
	}

	m.processes[config.TaskID] = proc

	// Start goroutines to handle child output
	go m.handleChildStderr(proc)
	go m.handleChildMessages(proc)
	go m.monitorChildProcess(proc)

	// Start session timeout timer
	proc.timeoutTimer = time.AfterFunc(m.sessionTimeout, func() {
		m.logger.Printf("Session %s timed out after %v - auto-stopping", config.TaskID, m.sessionTimeout)
		m.StopSession(config.TaskID)
	})
	m.logger.Printf("Session timeout timer started: %v", m.sessionTimeout)

	// Send START_SESSION command to child
	startMsg := ipc.BuildStartSessionMessage(
		config.TaskID,
		config.AppID,
		config.Channel,
		config.BotUID,
		config.BotToken,
		config.PalabraUID,
		config.AnamAPIKey,
		config.AnamBaseURL,
		config.AnamAvatarID,
		config.AnamUID,
		config.AnamToken,
		config.TargetLanguage,
	)

	if err := proc.stdinWriter.WriteMessage(startMsg); err != nil {
		m.logger.Printf("Failed to send START_SESSION: %v", err)
		proc.cmd.Process.Kill()
		delete(m.processes, config.TaskID)
		return nil, fmt.Errorf("failed to send start command: %w", err)
	}

	// Wait for connection (with timeout)
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			m.logger.Printf("Timeout waiting for session %s to connect", config.TaskID)
			m.StopSession(config.TaskID)
			return nil, fmt.Errorf("timeout waiting for session to connect")
		case <-ticker.C:
			proc.mu.RLock()
			status := proc.Status
			proc.mu.RUnlock()

			if status == botipc.SessionStatusCONNECTED || status == botipc.SessionStatusSTREAMING {
				m.logger.Printf("Session %s connected successfully", config.TaskID)
				return proc, nil
			}
			if status == botipc.SessionStatusFAILED {
				m.logger.Printf("Session %s failed to connect", config.TaskID)
				m.StopSession(config.TaskID)
				return nil, fmt.Errorf("session failed to connect")
			}
		}
	}
}

// StopSession stops a running session
func (m *BotProcessManager) StopSession(taskID string) error {
	m.mu.Lock()
	proc, ok := m.processes[taskID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("no session found for task %s", taskID)
	}
	delete(m.processes, taskID)
	m.mu.Unlock()

	// Cancel timeout timer if running
	if proc.timeoutTimer != nil {
		proc.timeoutTimer.Stop()
	}

	m.logger.Printf("Stopping session for task %s", taskID)

	// Send STOP_SESSION command
	stopMsg := ipc.BuildStopSessionMessage(taskID, "Requested by parent")
	if err := proc.stdinWriter.WriteMessage(stopMsg); err != nil {
		m.logger.Printf("Failed to send STOP_SESSION (will force kill): %v", err)
	}

	// Close shutdown channel to signal handlers
	close(proc.shutdownChan)

	// Give child time to cleanup gracefully
	done := make(chan struct{})
	go func() {
		proc.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Printf("Child process for task %s exited gracefully", taskID)
	case <-time.After(5 * time.Second):
		m.logger.Printf("Child process for task %s did not exit, killing", taskID)
		proc.cmd.Process.Kill()
	}

	// Close pipes
	proc.stdin.Close()
	proc.stdout.Close()
	proc.stderr.Close()

	return nil
}

// GetSession returns a session by task ID
func (m *BotProcessManager) GetSession(taskID string) (*BotProcess, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	proc, ok := m.processes[taskID]
	return proc, ok
}

// GetAllSessions returns all active sessions
func (m *BotProcessManager) GetAllSessions() map[string]*BotProcess {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*BotProcess)
	for k, v := range m.processes {
		result[k] = v
	}
	return result
}

// handleChildStderr reads and logs child stderr
func (m *BotProcessManager) handleChildStderr(proc *BotProcess) {
	scanner := bufio.NewScanner(proc.stderr)
	for scanner.Scan() {
		select {
		case <-proc.shutdownChan:
			return
		default:
			m.logger.Printf("[child:%s] %s", proc.TaskID, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		m.logger.Printf("Error reading child stderr for task %s: %v", proc.TaskID, err)
	}
}

// handleChildMessages reads IPC messages from child stdout
func (m *BotProcessManager) handleChildMessages(proc *BotProcess) {
	reader := ipc.NewMessageReader(proc.stdout)

	for {
		select {
		case <-proc.shutdownChan:
			return
		default:
		}

		msgBytes, err := reader.ReadMessage()
		if err != nil {
			if err == io.EOF {
				m.logger.Printf("Child stdout closed for task %s", proc.TaskID)
			} else {
				m.logger.Printf("Error reading from child for task %s: %v", proc.TaskID, err)
			}
			return
		}

		msgType, payloadBytes, err := ipc.ParseIPCMessage(msgBytes)
		if err != nil {
			m.logger.Printf("Error parsing IPC message for task %s: %v", proc.TaskID, err)
			continue
		}

		switch msgType {
		case botipc.MessageTypeSTATUS_UPDATE:
			payload := ipc.ParseStatusPayload(payloadBytes)
			proc.mu.Lock()
			proc.Status = payload.Status()
			proc.AnamUID = payload.AnamUid()
			proc.mu.Unlock()
			m.logger.Printf("Task %s status: %s - %s (AnamUID: %d)",
				proc.TaskID,
				botipc.EnumNamesSessionStatus[payload.Status()],
				string(payload.Message()),
				payload.AnamUid())

		case botipc.MessageTypeLOG_MESSAGE:
			payload := ipc.ParseLogPayload(payloadBytes)
			levelName := botipc.EnumNamesLogLevel[payload.Level()]
			m.logger.Printf("[child:%s][%s] %s", proc.TaskID, levelName, string(payload.Message()))

		case botipc.MessageTypeERROR_RESPONSE:
			payload := ipc.ParseErrorPayload(payloadBytes)
			m.logger.Printf("Task %s error [%s]: %s (fatal: %v)",
				proc.TaskID,
				string(payload.ErrorCode()),
				string(payload.Message()),
				payload.Fatal())

			if payload.Fatal() {
				proc.mu.Lock()
				proc.Status = botipc.SessionStatusFAILED
				proc.mu.Unlock()
			}

		default:
			m.logger.Printf("Unknown message type from child for task %s: %d", proc.TaskID, msgType)
		}
	}
}

// monitorChildProcess watches for child process exit
func (m *BotProcessManager) monitorChildProcess(proc *BotProcess) {
	// Wait for process to exit
	err := proc.cmd.Wait()

	select {
	case <-proc.shutdownChan:
		// Normal shutdown, ignore
		return
	default:
	}

	// Unexpected exit (crash)
	m.logger.Printf("Child process for task %s exited unexpectedly: %v", proc.TaskID, err)

	// Update status
	proc.mu.Lock()
	proc.Status = botipc.SessionStatusFAILED
	proc.mu.Unlock()

	// Remove from active processes
	m.mu.Lock()
	delete(m.processes, proc.TaskID)
	m.mu.Unlock()

	// Close pipes
	proc.stdin.Close()
	proc.stdout.Close()
	proc.stderr.Close()
}

// Shutdown stops all sessions and cleans up
func (m *BotProcessManager) Shutdown() {
	m.logger.Println("Shutting down all bot processes")

	m.mu.Lock()
	taskIDs := make([]string, 0, len(m.processes))
	for taskID := range m.processes {
		taskIDs = append(taskIDs, taskID)
	}
	m.mu.Unlock()

	for _, taskID := range taskIDs {
		m.StopSession(taskID)
	}

	close(m.shutdownChan)
}
