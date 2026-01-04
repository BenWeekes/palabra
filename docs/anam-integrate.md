# Anam Avatar Integration

## Overview

Anam provides lip-synced avatar video for Palabra translations. Enabled via `ENABLE_ANAM=true` in backend `.env`.

**Architecture**: Backend bot subscribes to Palabra audio (UID 3000), forwards to Anam WebSocket, Anam publishes avatar as UID 4000.

**Three-UID System**:
- **UID 3000**: Palabra translation (audio-only, bot subscribes)
- **UID 4000**: Anam avatar (video+audio, client subscribes)
- **UID 5000**: Backend bot (audio forwarder, invisible)

## Build & Deploy

### Prerequisites

- Anam API key (base64 encoded)
- Anam Avatar ID
- Docker with CGO support (Agora SDK requires native libraries)

### Configuration (.env)

```bash
# Enable avatar mode
ENABLE_ANAM=true

# Anam credentials
ANAM_API_KEY=base64_encoded_key
ANAM_BASE_URL=https://api.anam.ai/v1
ANAM_AVATAR_ID=1bed9d5e-5e81-4d98-a04a-21e346bea528
ANAM_VIDEO_ENCODING=H264        # Must be uppercase
ANAM_QUALITY=high

# Agora credentials (required)
APP_ID=59367b7c63ca4472a529b3e96e0bafdd
APP_CERTIFICATE=1508f5aab7e14f5e91e26e1921084563
```

### Build & Run

```bash
cd /Users/benweekes/work/palabra/app-builder-backend

# Rebuild with CGO enabled
docker compose down && docker compose up -d --build
```

**Build notes**:
- `Dockerfile` uses `CGO_ENABLED=1` (required for Agora SDK)
- Platform: `linux/amd64` (Agora SDK is x86-64 only)
- Agora SDK `.so` files copied to `/usr/local/lib`

## Architecture

### Data Flow

```
User A speaks (English, UID 100)
  ↓
Palabra API translates → UID 3000 (audio-only)
  ↓
Backend Bot (UID 5000):
  - Joins Agora channel as subscriber
  - Subscribes to UID 3000 audio
  - Receives PCM frames in callback
  - Upsamples 16kHz → 24kHz
  - Forwards to Anam WebSocket
  ↓
Anam WebSocket:
  - Receives "voice" commands with base64 PCM
  - Generates lip-synced avatar video
  - Publishes to Agora as UID 4000
  ↓
User B subscribes to UID 4000:
  - Sees French-speaking avatar video
  - Hears translated audio
  - Avatar video replaces original user's tile
```

### UID Separation (Critical)

**Why separate UIDs?**

- Bot (5000) cannot use Anam's UID (4000) → would cause collision
- Bot is subscriber-only (doesn't publish)
- Anam joins as publisher via WebSocket init command
- Client receives UID 4000 in API response, subscribes to it

**Diagram**:
```
┌─────────────────────────────────────────────────┐
│ Agora RTC Channel "meeting-123"                 │
│                                                  │
│  UID 100  (User A - speaks English)             │
│    ↓                                             │
│  UID 3000 (Palabra - publishes French audio)    │
│    ↓                                             │
│  UID 5000 (Our Bot - subscribes to 3000)        │
│    ↓                                             │
│  UID 4000 (Anam Avatar - publishes video+audio) │
└─────────────────────────────────────────────────┘
             ↑
             │
      Anam WebSocket
      (bot forwards PCM)
```

## Backend Implementation

### Files

**services/anam_client.go** (NEW):
- WebSocket client for Anam API
- Two-step auth: session-token → engine/session
- `SendAudio(base64_pcm)` - sends audio chunks
- `SendVoiceEnd()` - signals end of utterance
- Heartbeat every 5 seconds

**services/agora_bot.go** (NEW):
- Agora SDK wrapper (Go CGO bindings)
- Joins channel as UID 5000 (subscriber-only)
- Subscribes to Palabra UID 3000
- Audio callback forwards PCM to Anam
- Silence detection (500ms threshold) → voice_end

**services/palabra.go** (MODIFIED):
- Lines 300-405: `ENABLE_ANAM` flag logic
- When enabled:
  - Creates Anam WebSocket session
  - Spawns bot with separate UID 5000
  - Changes response UID from 3000 → 4000
- When disabled: Returns UID 3000 as before

### Key Code

**palabra.go - Lines 300-405**:

```go
enableAnam := viper.GetBool("ENABLE_ANAM")

if enableAnam {
  avatarID := viper.GetString("ANAM_AVATAR_ID")

  for i, stream := range streams {
    palabraUID := stream.UID  // Original 3000

    // Generate Anam UID (4000+)
    anamUIDNum := getNextAnamUID(req.Channel)
    anamUID := fmt.Sprintf("%d", anamUIDNum)

    // Generate Bot UID (5000+) - SEPARATE from Anam
    botUIDNum := uint32(5000 + i)
    botUID := fmt.Sprintf("%d", botUIDNum)

    // Generate tokens
    anamToken, _ := BuildTokenWithUID(..., anamUIDNum, RolePublisher, ...)
    botToken, _ := BuildTokenWithUID(..., botUIDNum, RoleSubscriber, ...)

    // Create Anam WebSocket session
    anamClient := NewAnamClient(avatarID, appID, req.Channel, anamUID, anamToken)
    err := anamClient.StartSession()
    if err != nil {
      return nil, fmt.Errorf("Anam session failed: %w", err)
    }

    // Create bot with separate UID
    bot := NewAgoraBot(appID, req.Channel, botUID, botToken, palabraUID, anamClient)
    err = bot.Start()
    if err != nil {
      return nil, fmt.Errorf("Bot start failed: %w", err)
    }

    // CRITICAL: Client subscribes to Anam UID, not Palabra
    streams[i].UID = anamUID  // 3000 → 4000
  }
}

return streams
```

**agora_bot.go - Audio forwarding**:

```go
type AgoraBot struct {
  appID       string
  channel     string
  botUID      string      // "5000" (our bot)
  botToken    string
  targetUID   string      // "3000" (Palabra translation)
  anamClient  *AnamClient
  rtcService  unsafe.Pointer
  connection  unsafe.Pointer
}

func (b *AgoraBot) Start() error {
  // Initialize Agora SDK
  b.rtcService = C.agora_service_initialize(...)
  b.connection = C.agora_rtc_conn_create(...)

  // Register audio callback
  C.agora_rtc_conn_register_observer(..., OnPlaybackAudioFrameBeforeMixing)

  // Join channel as subscriber
  C.agora_rtc_conn_connect(..., b.channel, b.botUID, b.botToken)

  return nil
}

// Audio callback (called by Agora SDK for each frame)
func OnPlaybackAudioFrameBeforeMixing(userId string, frame *AudioFrame, ...) {
  if userId == b.targetUID {  // "3000"
    // Upsample 16kHz → 24kHz (Anam requirement)
    upsampled := upsample(frame.Buffer, 16000, 24000)

    // Detect silence using RMS energy
    if isSilence(upsampled) {
      silenceDuration += 10ms
      if silenceDuration >= 500ms {
        b.anamClient.SendVoiceEnd()
        silenceDuration = 0
      }
    } else {
      // Forward to Anam
      audioB64 := base64.StdEncoding.EncodeToString(upsampled)
      b.anamClient.SendAudioWithSampleRate(audioB64, 24000)
    }
  }
}
```

**anam_client.go - WebSocket protocol**:

```go
func (c *AnamClient) StartSession() error {
  // Step 1: POST /auth/session-token
  tokenReq := AnamSessionTokenRequest{
    PersonaConfig: struct {
      AvatarID string `json:"avatarId"`
    }{AvatarID: c.avatarID},
    Environment: struct {
      AgoraSettings struct {
        AppID            string `json:"appId"`
        Token            string `json:"token"`
        Channel          string `json:"channel"`
        UID              string `json:"uid"`       // "4000"
        Quality          string `json:"quality"`
        VideoEncoding    string `json:"videoEncoding"`  // "H264"
        EnableStringUIDs bool   `json:"enableStringUids"`
      } `json:"agoraSettings"`
    }{...},
  }

  resp, _ := http.Post(baseURL + "/auth/session-token", tokenReq)
  c.sessionToken = resp.SessionToken

  // Step 2: POST /engine/session
  resp2, _ := http.Post(baseURL + "/engine/session", map[string]interface{}{})
  c.sessionID = resp2.SessionID
  c.wsAddress = resp2.WebSocketAddress

  // Step 3: Connect WebSocket
  conn, _ := websocket.Dial(c.wsAddress)
  c.conn = conn

  // Step 4: Send init command
  initMsg := map[string]interface{}{
    "command":               "init",
    "event_id":              uuid.NewString(),
    "session_id":            c.sessionID,
    "avatar_id":             c.avatarID,
    "quality":               "high",
    "version":               "1.0",
    "video_encoding":        "H264",  // Must be uppercase
    "activity_idle_timeout": 120,
    "agora_settings": map[string]interface{}{
      "app_id":            c.appID,
      "token":             c.token,
      "channel":           c.channel,
      "uid":               c.anamUID,  // "4000"
      "enable_string_uid": false,
    },
  }
  conn.WriteJSON(initMsg)

  // CRITICAL: Wait 500ms before sending audio
  time.Sleep(500 * time.Millisecond)

  // Step 5: Start heartbeat (every 5s)
  go c.sendHeartbeat()

  return nil
}

func (c *AnamClient) SendAudioWithSampleRate(audioB64 string, sampleRate int) error {
  msg := map[string]interface{}{
    "command":     "voice",
    "audio":       audioB64,
    "sample_rate": sampleRate,  // 24000
    "encoding":    "PCM16",
    "event_id":    uuid.NewString(),
  }
  return c.conn.WriteJSON(msg)
}

func (c *AnamClient) SendVoiceEnd() error {
  msg := map[string]interface{}{
    "command":  "voice_end",
    "event_id": uuid.NewString(),
  }
  return c.conn.WriteJSON(msg)
}

func (c *AnamClient) sendHeartbeat() {
  ticker := time.NewTicker(5 * time.Second)
  for {
    <-ticker.C
    heartbeat := map[string]interface{}{
      "command":   "heartbeat",
      "event_id":  uuid.NewString(),
      "timestamp": time.Now().UnixMilli(),
    }
    c.conn.WriteJSON(heartbeat)
  }
}
```

## Anam WebSocket Protocol

### Requirements (Critical)

- ✅ All messages use `snake_case` (not camelCase)
- ✅ All messages include `event_id` (UUID v4)
- ✅ Use `"command": "voice"` (NOT `"kind": "audio"`)
- ✅ Video encoding must be uppercase `"H264"`
- ✅ Wait 500ms after init before sending audio
- ✅ Heartbeat every 5 seconds (with timestamp)
- ✅ Handle WebSocket 301 redirects

### Message Types

**init** (sent once):
```json
{
  "command": "init",
  "event_id": "uuid-v4",
  "session_id": "session-id-from-engine-api",
  "avatar_id": "1bed9d5e-5e81-4d98-a04a-21e346bea528",
  "quality": "high",
  "version": "1.0",
  "video_encoding": "H264",
  "activity_idle_timeout": 120,
  "agora_settings": {
    "app_id": "your_app_id",
    "token": "agora_rtc_token",
    "channel": "meeting-channel",
    "uid": "4000",
    "enable_string_uid": false
  }
}
```

**voice** (sent continuously):
```json
{
  "command": "voice",
  "event_id": "uuid-v4",
  "audio": "base64_encoded_pcm",
  "sample_rate": 24000,
  "encoding": "PCM16"
}
```

**voice_end** (sent after silence):
```json
{
  "command": "voice_end",
  "event_id": "uuid-v4"
}
```

**heartbeat** (sent every 5s):
```json
{
  "command": "heartbeat",
  "event_id": "uuid-v4",
  "timestamp": 1704235123456
}
```

## Frontend Integration

### Video Display

**Goal**: Anam avatar replaces original user's video in their tile.

**Implementation** (`TranslationProvider.tsx:636-662`):

```typescript
// When Anam UID 4000 publishes video
if (mediaType === 'video' && isAnamUid(uid)) {
  await originalSubscribe(user, 'video');

  if (user.videoTrack) {
    const sourceUid = translation.sourceUid;  // Original user "100"

    // Stop original video
    const sourceUser = client.remoteUsers.find(u => u.uid.toString() === sourceUid);
    if (sourceUser?.videoTrack) {
      sourceUser.videoTrack.stop();
    }

    // Play avatar video in source user's tile
    user.videoTrack.play(sourceUid);  // Agora finds div by UID

    console.log('[Palabra] ✓ Anam avatar video now playing in tile for UID', sourceUid);
  }
}
```

**User Experience**:
1. User clicks "Translate Audio" → "French"
2. Original English video stops
3. French-speaking Anam avatar appears in same tile
4. Lips sync with French audio

## Debug

### Backend Logs

```bash
# Expected logs when Anam enabled
docker logs server -f | grep -i anam

[Anam] Getting session token at https://api.anam.ai/v1/auth/session-token
[Anam] Got session token
[Anam] Creating engine session
[Anam] Session created: xxx, WebSocket: wss://connect-us.anam.ai/...
[Anam] Connected to Anam WebSocket
[Anam] 📤 Sending init - Avatar will join as UID 4000 in channel meeting-123
[Anam] Init command sent successfully
[Anam] Waiting 500ms for Anam to initialize...
[Anam] Init delay complete, starting heartbeat
[Anam] Sent heartbeat

# Bot logs
docker logs server -f | grep AgoraBot

[AgoraBot] Agora service initialized
[AgoraBot] RTC connection created
[AgoraBot] ✅ Bot (UID 5000) connected to channel: meeting-123
[AgoraBot] Audio callback - UID: 3000, Bytes: 3840
[AgoraBot] Forwarding audio to Anam (24kHz, 480 bytes)
```

### Frontend Logs

```javascript
// Browser console
[Palabra] 🚫 Blocking auto-subscribe for translation UID 4000 audio
[Palabra] 📡 Translation UID published: 4000 Type: audio
[Palabra] ✓ Playing Anam avatar audio from UID 4000

[Palabra] 🚫 Blocking auto-subscribe for translation UID 4000 video
[Palabra] 📡 Translation UID published: 4000 Type: video
[Palabra] ✓ Anam avatar video now playing in tile for UID 100
```

### Common Issues

#### Avatar Disappears After 5 Seconds

**Symptom**: Avatar joins, then leaves immediately

**Cause**: UID collision - bot joining as 4000, kicking out Anam

**Fix**: ✅ Bot now uses UID 5000 (separate from Anam's 4000)

**Verify**:
```bash
docker logs server 2>&1 | grep -E "(botUID|anamUID)"
# Should see: botUID=5000, anamUID=4000
```

#### No Audio Forwarded to Anam

**Debug steps**:
1. Check bot joined channel:
   ```bash
   docker logs server | grep "Bot.*connected"
   # Expected: [AgoraBot] ✅ Bot (UID 5000) connected
   ```

2. Check audio callback fires:
   ```bash
   docker logs server | grep "Audio callback"
   # Should appear when user speaks
   ```

3. Check UID match:
   ```bash
   docker logs server | grep "target UID"
   # Expected: targetUID=3000
   ```

4. Check Anam send success:
   ```bash
   docker logs server | grep "Forwarding audio"
   # Should appear for each audio frame
   ```

#### WebSocket Connection Fails

**Symptoms**:
- `failed to connect to WebSocket: dial tcp: i/o timeout`
- `WebSocket handshake failed: 401 Unauthorized`

**Debug**:
1. Check Anam API key valid:
   ```bash
   echo $ANAM_API_KEY | base64 -d
   ```

2. Check session token received:
   ```bash
   docker logs server | grep "Got session token"
   ```

3. Check WebSocket URL format:
   ```bash
   docker logs server | grep "WebSocket:"
   # Expected: wss://connect-us.anam.ai/...
   ```

4. Test with curl:
   ```bash
   curl -X POST https://api.anam.ai/v1/auth/session-token \
     -H "Authorization: Bearer $ANAM_API_KEY" \
     -H "Content-Type: application/json" \
     -d '{"personaConfig": {"avatarId": "..."}, ...}'
   ```

#### Bot Doesn't Join Channel

**Symptoms**:
- No "Bot connected" log
- Agora errors in backend

**Debug**:
1. Check CGO libraries loaded:
   ```bash
   docker logs server | grep "Agora service"
   # Expected: [AgoraBot] Agora service initialized
   ```

2. Check platform:
   ```bash
   docker inspect server | grep Platform
   # Expected: linux/amd64 (Agora SDK is x86-64 only)
   ```

3. Check shared libraries:
   ```bash
   docker exec server ls /usr/local/lib/*.so
   # Should list Agora .so files
   ```

## Testing Checklist

### Basic Flow

- [ ] Backend starts with ENABLE_ANAM=true
- [ ] User clicks "Translate Audio" → French
- [ ] Backend creates Anam session (check logs)
- [ ] Bot joins channel as UID 5000
- [ ] Anam avatar joins as UID 4000
- [ ] Client subscribes to UID 4000 audio+video
- [ ] Avatar video replaces original user's tile
- [ ] Audio is French translation with lip sync

### Edge Cases

- [ ] Multiple users translated simultaneously
- [ ] User stops translation → original video restored
- [ ] Late joiner doesn't see/hear avatar (unless they request)
- [ ] Network interruption → WebSocket reconnects
- [ ] Anam API rate limit → graceful error

### Cleanup

- [ ] Stop translation → bot disconnects
- [ ] Stop translation → Anam WebSocket closes
- [ ] Stop translation → UID 4000 leaves channel
- [ ] No memory leaks (check with valgrind if needed)

## Production Deployment

### Docker Build

**Dockerfile** already configured:
- `FROM --platform=linux/amd64 golang:1.21` (build stage)
- `CGO_ENABLED=1` for Agora SDK
- Copies Agora SDK headers (`agora_sdk/`)
- Copies `.so` files to `/usr/local/lib/`
- Sets `LD_LIBRARY_PATH=/usr/local/lib`

### Environment Variables

```bash
# Required for Anam
ENABLE_ANAM=true
ANAM_API_KEY=base64_key
ANAM_AVATAR_ID=uuid
ANAM_VIDEO_ENCODING=H264
ANAM_QUALITY=high

# Optional (defaults shown)
ANAM_BASE_URL=https://api.anam.ai/v1
```

### Monitoring

**Key metrics**:
- Anam session creation success rate
- WebSocket connection uptime
- Audio forwarding latency
- Bot join success rate
- Avatar UID 4000 publish rate

**Alerts**:
- Anam session creation failures
- WebSocket disconnects (>3 per hour)
- Bot join failures (>1% of attempts)
- Audio callback not firing (when translation active)

## References

- **Palabra Integration**: See `palabra-integrate.md`
- **General Dev**: See `app-builder-dev.md`
- **Anam API**: Contact Anam team for documentation
- **Agora SDK**: https://docs.agora.io/en/sdks (Golang SDK)
