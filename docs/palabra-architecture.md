# Palabra/Anam Integration Architecture

This document describes the architecture for integrating Palabra real-time translation with Anam AI avatar rendering.

## Overview

The integration enables real-time speech translation with AI avatar video output:

1. **Palabra** transcribes and translates speech in real-time
2. **Anam** renders an AI avatar that speaks the translated text
3. **Agora SDK** handles audio/video streaming between all participants

## Problem: SDK Crash Isolation

The Agora Go SDK wraps native C code that can crash with segfaults. Go's `recover()` cannot catch these native crashes, which would bring down the entire HTTP server causing 502 errors for all users.

**Solution:** Run the Agora SDK in an isolated child process. If it crashes, only that translation session ends - the HTTP server stays up.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      PARENT PROCESS                              │
│                    (HTTP Server - port 7080)                     │
│                                                                  │
│  Endpoints:                                                      │
│  - POST /v1/palabra/start  - Start translation session          │
│  - POST /v1/palabra/stop   - Stop translation session           │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                  BotProcessManager                          │ │
│  │                                                              │ │
│  │  - Spawns bot_worker child processes                        │ │
│  │  - One child per translation session                        │ │
│  │  - Communicates via stdin/stdout pipes                      │ │
│  │  - Uses FlatBuffers for efficient binary IPC                │ │
│  │  - Monitors child health, handles crashes gracefully        │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
              │                              ▲
              │ stdin                        │ stdout
              │ (START_SESSION,              │ (STATUS_UPDATE,
              │  STOP_SESSION)               │  LOG_MESSAGE,
              ▼                              │  ERROR_RESPONSE)
┌─────────────────────────────────────────────────────────────────┐
│                      CHILD PROCESS                               │
│                      (bot_worker binary)                         │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                     BotWorker                               │ │
│  │                                                              │ │
│  │  ┌─────────────────┐      ┌─────────────────┐              │ │
│  │  │    AgoraBot     │      │   AnamClient    │              │ │
│  │  │                 │      │                 │              │ │
│  │  │ - Joins channel │      │ - Connects to   │              │ │
│  │  │ - Subscribes to │─────▶│   Anam API      │              │ │
│  │  │   Palabra UID   │audio │ - Sends audio   │              │ │
│  │  │ - Forwards PCM  │      │ - Avatar joins  │              │ │
│  │  │   audio frames  │      │   Agora channel │              │ │
│  │  └─────────────────┘      └─────────────────┘              │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## IPC Protocol

Communication between parent and child uses FlatBuffers for efficient binary serialization.

### Message Types

**Parent → Child:**
- `START_SESSION` - Start a new translation session with config
- `STOP_SESSION` - Gracefully stop the session

**Child → Parent:**
- `STATUS_UPDATE` - Session state changes (CONNECTING, STREAMING, etc.)
- `LOG_MESSAGE` - Log output from child process
- `ERROR_RESPONSE` - Error occurred (fatal or non-fatal)

### Message Framing

Messages are length-prefixed:
```
[4 bytes: big-endian length][N bytes: FlatBuffer payload]
```

### Session States

```
INITIALIZING → CONNECTING_ANAM → CONNECTING_AGORA → CONNECTED → STREAMING
                                                                    │
                                                                    ▼
                                                              DISCONNECTING
                                                                    │
                                                                    ▼
                                                              DISCONNECTED
```

On error: Any state → `FAILED`

## UID Assignment

Each participant in the Agora channel has a unique UID:

| UID Range | Purpose |
|-----------|---------|
| 1-999 | Reserved |
| 1000-2999 | Real users |
| 3000-3999 | Palabra translation bots (one per language) |
| 4000-4999 | Anam avatar UIDs (renders translated speech) |
| 4500+ | Audio forwarder bots (subscribes to Palabra, forwards to Anam) |

## File Structure

```
services/
├── palabra.go              # HTTP handlers, orchestration
├── bot_process_manager.go  # Parent-side process management
├── bot_worker.go           # Child-side orchestrator
├── agora_bot.go            # Agora SDK wrapper
├── anam_client.go          # Anam API/WebSocket client
└── ipc/
    ├── bot_ipc.fbs         # FlatBuffers schema
    ├── botipc/             # Generated Go code
    └── ipc.go              # IPC utilities

cmd/
├── video_conferencing/     # Main HTTP server
│   └── server.go
└── bot_worker/             # Child process entry point
    └── main.go
```

## Building

The Dockerfile builds both binaries:

```dockerfile
# Build main server
RUN go build -o /go/bin/server ./cmd/video_conferencing

# Build child process
RUN go build -o /go/bin/bot_worker ./cmd/bot_worker
```

## Session Protection

Sessions are protected by multiple safeguards to prevent runaway resource usage:

### 1. Session Timeout (Parent Process)

Maximum session duration enforced by `BotProcessManager`:

```go
// Default: 10 minutes, configurable via PALABRA_SESSION_TIMEOUT_MINUTES
proc.timeoutTimer = time.AfterFunc(sessionTimeout, func() {
    m.StopSession(taskID)
})
```

When timeout fires:
- Sends `STOP_SESSION` to child
- Cleans up resources
- Closes Anam connection

### 2. Idle Detection (Child Process)

Auto-stops session if no audio activity:

```go
// Default: 60 seconds, configurable via PALABRA_IDLE_TIMEOUT_SECONDS
if idleDuration > idleTimeout {
    sendError("IDLE_TIMEOUT", ...)
    // Cleanup and exit
}
```

- Checks every 10 seconds
- Tracks `lastAudioTime` in AgoraBot
- Prevents credit burn during silence

### 3. Target-Left Detection (Child Process)

Auto-stops if Palabra bot (source UID) leaves the channel:

```go
// In Agora OnUserLeft callback
if uid == targetUID {
    close(targetLeftChan)  // Signal BotWorker to stop
}
```

- Immediate detection via Agora callback
- No waiting for timeout
- Prevents orphaned sessions

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PALABRA_SESSION_TIMEOUT_MINUTES` | 10 | Max session duration |
| `PALABRA_IDLE_TIMEOUT_SECONDS` | 60 | Stop after this long with no audio |

## Crash Recovery

When a child process crashes:

1. `BotProcessManager.monitorChildProcess()` detects the exit
2. Session status is set to `FAILED`
3. Process is removed from active sessions map
4. Pipes are closed
5. HTTP server continues running normally
6. User can retry starting a new session

## Debugging

Child process logs are captured and prefixed:
```
[BotProcessManager] ... [child:task-id] [bot_worker] ...
```

Session lifecycle is logged:
```
[BotProcessManager] Task xxx status: CONNECTING_ANAM - Connecting to Anam API
[BotProcessManager] Task xxx status: STREAMING - Audio streaming active
```
