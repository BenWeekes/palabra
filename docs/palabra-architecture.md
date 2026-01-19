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
│  - POST /v1/avatar/start   - Start persistent avatar            │
│  - POST /v1/avatar/stop    - Stop persistent avatar             │
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
- `SWITCH_AUDIO_SOURCE` - Switch bot's audio subscription to a different UID (for persistent avatar mode)

**Child → Parent:**
- `STATUS_UPDATE` - Session state changes (CONNECTING, STREAMING, etc.)
- `LOG_MESSAGE` - Log output from child process
- `ERROR_RESPONSE` - Error occurred (fatal or non-fatal)

### SWITCH_AUDIO_SOURCE (Persistent Avatar Mode)

When translation starts/stops with an active avatar, the parent sends `SWITCH_AUDIO_SOURCE` to change which UID the bot subscribes to:

```
Parent → Child: SWITCH_AUDIO_SOURCE {
  task_id: "avatar-channel-uid-xxx",
  new_uid: 3000,        // Palabra UID for translation, or source UID for original
  is_translation: true  // true = Palabra audio, false = original user audio
}
```

The child process then:
1. Unsubscribes from current audio source
2. Subscribes to the new UID
3. Continues forwarding audio to Anam

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

## Persistent Avatar Mode

Persistent Avatar Mode allows the avatar to be started independently from translation. The avatar lip-syncs to the speaker's original audio, and when translation is enabled, seamlessly switches to the translated audio.

### User Flow

1. **Start Avatar** → Avatar appears, lip-syncing to original audio
2. **Start Translation** → Avatar switches to translated audio (no visual change)
3. **Stop Translation** → Avatar continues with original audio
4. **Stop Avatar** → Avatar removed, original video restored

### Session Management

Avatar sessions are tracked separately from translation sessions:

```go
// Avatar session (in palabra.go)
type AvatarSession struct {
    SessionID    string    // Bot process task ID
    Channel      string
    SourceUID    string    // Original user being avatarized
    AnamUID      uint32    // Anam avatar UID (4000+)
    BotUID       uint32    // Bot process UID (4500+)
    IsTranslating bool     // Currently translating?
}

var avatarSessions = make(map[string]*AvatarSession) // key: "channel:sourceUid"
```

### Audio Source Switching

When translation starts/stops with an active avatar:

1. **Translation Starts**: Backend detects existing avatar, sends `SWITCH_AUDIO_SOURCE` with Palabra UID (3000)
2. **Translation Stops**: Backend sends `SWITCH_AUDIO_SOURCE` with source user UID

The bot process handles the switch:
```go
// In bot_worker/main.go
case ipc.MessageTypeSWITCH_AUDIO_SOURCE:
    newUID := payload.NewUid()
    isTranslation := payload.IsTranslation()

    // Unsubscribe from current source
    bot.Unsubscribe(currentUID)

    // Subscribe to new source
    bot.Subscribe(newUID)
```

### Frontend Integration

The frontend tracks avatar state separately:

```typescript
// TranslationProvider.tsx
const [activeAvatars, setActiveAvatars] = useState<Map<string, AvatarSession>>(new Map());

// Start avatar (calls /v1/avatar/start)
const startAvatar = async (sourceUid: string) => {
    const response = await fetch('/v1/avatar/start', {...});
    // Store avatar session, subscribe to Anam UID 4000
};

// Start translation (detects existing avatar)
const startTranslation = async (sourceUid: string, targetLanguage: string) => {
    // If avatar active, backend switches audio source
    // Frontend keeps same subscription (UID 4000)
};
```
