# Palabra Real-Time Translation Integration

## Overview

Palabra provides real-time speech translation for Agora video conferences. Two operating modes:

1. **Audio-Only Mode** (`ENABLE_ANAM=false`) - Palabra translates directly, client receives audio stream
2. **Avatar Mode** (`ENABLE_ANAM=true`) - Palabra + Anam bot creates lip-synced avatar video

**Client auto-detects mode from UID range** (no client config needed):
- UID 3000-3999: Audio-only (Palabra direct)
- UID 4000-4999: Avatar (Anam video+audio)

## Architecture

### Data Flow

```
User A speaks (English, UID 100)
  ↓
User B clicks "Translate Audio" → French
  ↓
Frontend → POST /v1/palabra/start
  {sourceUid: "100", targetLanguage: "fr"}
  ↓
Backend (palabra.go):
  - Creates Palabra translation task
  - If ENABLE_ANAM=false: Returns UID 3000 (audio-only)
  - If ENABLE_ANAM=true: Creates bot+Anam, returns UID 4000 (avatar)
  ↓
Frontend (TranslationProvider.tsx):
  - Stores translation: activeTranslations.set("100", {translationUid: "3000" or "4000"})
  - When UID publishes, checks activeTranslations Map
  - If found, subscribes to audio (and video if 4000+)
  ↓
User B hears/sees translated stream
```

### UID Ranges

| Range | Purpose | Auto-Subscribe |
|-------|---------|----------------|
| 1-2999 | Normal users | ✅ Yes (App Builder default) |
| 3000-3999 | Palabra audio-only | ❌ No (explicit only) |
| 4000-4999 | Anam avatar video+audio | ❌ No (explicit only) |
| 5000+ | Backend bot (audio forwarder) | ❌ No (invisible) |

## Build & Deploy

### Backend

```bash
cd /Users/benweekes/work/palabra/app-builder-backend

# Build with Docker
docker compose down && docker compose up -d --build

# Runs at http://localhost:8081
```

### Frontend

```bash
cd /Users/benweekes/work/palabra/app-builder-core/template

npm install
npm run web

# Runs at http://localhost:9000
```

### Configuration

**Backend (.env)** - Controls which mode:

```bash
# Palabra credentials (required)
PALABRA_CLIENT_ID=your_client_id
PALABRA_CLIENT_SECRET=your_client_secret

# Mode selection
ENABLE_ANAM=false    # Audio-only mode (set true for avatar)

# Anam credentials (only needed if ENABLE_ANAM=true)
ANAM_API_KEY=your_key
ANAM_AVATAR_ID=1bed9d5e-5e81-4d98-a04a-21e346bea528

# Agora credentials
APP_ID=59367b7c63ca4472a529b3e96e0bafdd
APP_CERTIFICATE=1508f5aab7e14f5e91e26e1921084563
```

**Frontend (config.json)**:

```json
{
  "PALABRA_BACKEND_ENDPOINT": "http://localhost:8081"
}
```

**To switch modes**: Change `ENABLE_ANAM` in `.env` and restart backend.

## Backend Implementation

### API Endpoints

#### POST `/v1/palabra/start`

**Request**:
```json
{
  "channel": "meeting-channel",
  "sourceUid": "100",
  "sourceName": "John Doe",
  "sourceLanguage": "en-US",
  "targetLanguages": ["fr"]
}
```

**Response** (ENABLE_ANAM=false):
```json
{
  "ok": true,
  "data": {
    "taskId": "uuid",
    "streams": [
      {
        "uid": "3000",
        "language": "fr",
        "token": "agora_rtc_token"
      }
    ]
  }
}
```

**Response** (ENABLE_ANAM=true):
```json
{
  "ok": true,
  "data": {
    "taskId": "uuid",
    "streams": [
      {
        "uid": "4000",      // Changed from 3000
        "language": "fr",
        "token": "agora_rtc_token"
      }
    ]
  }
}
```

**Key Point**: Backend returns DIFFERENT UID based on `ENABLE_ANAM`. Client doesn't need mode awareness.

#### POST `/v1/palabra/stop`

**Request**:
```json
{
  "taskId": "uuid"
}
```

**Response**:
```json
{
  "ok": true
}
```

### Backend Code (services/palabra.go)

**Lines 300-405 - Mode selection**:

```go
enableAnam := viper.GetBool("ENABLE_ANAM")

if enableAnam {
  // Avatar mode: Create bot + Anam session
  for i, stream := range streams {
    palabraUID := stream.UID  // Original 3000

    // Generate Anam UID (4000+) for avatar
    anamUIDNum := getNextAnamUID(req.Channel)
    anamUID := fmt.Sprintf("%d", anamUIDNum)

    // Generate Bot UID (5000+) for audio forwarder
    botUIDNum := uint32(5000 + i)
    botUID := fmt.Sprintf("%d", botUIDNum)

    // CRITICAL: Change stream UID - client subscribes to Anam, not Palabra
    streams[i].UID = anamUID  // 3000 → 4000

    // Create Anam WebSocket session + bot
    anamClient := NewAnamClient(avatarID, appID, req.Channel, anamUID, anamToken)
    anamClient.StartSession()

    bot := NewAgoraBot(appID, req.Channel, botUID, botToken, palabraUID, anamClient)
    bot.Start()
  }
}
// If ENABLE_ANAM=false, streams[i].UID stays 3000 (Palabra audio-only)

return streams  // Contains either UID 3000 or 4000
```

## Frontend Implementation

### Monkey-Patch Subscription (TranslationProvider.tsx)

**Problem**: App Builder auto-subscribes to ALL users. This causes:
- Privacy issue: Users hear translations they didn't request
- Race conditions: UID publishes before backend response

**Solution**: Monkey-patch `client.subscribe()` to block UIDs 3000-4999.

**Lines 109-148**:

```typescript
useEffect(() => {
  const client = (rtcClient as any).client;  // Native Agora SDK
  const originalSubscribe = client.subscribe.bind(client);
  originalSubscribeRef.current = originalSubscribe;  // Store for manual use

  // Override subscribe to filter translation UIDs
  client.subscribe = async (user: any, mediaType: 'audio' | 'video') => {
    const uidNum = typeof user.uid === 'string' ? parseInt(user.uid, 10) : user.uid;
    const isTranslationUID = uidNum >= 3000 && uidNum < 5000;

    if (isTranslationUID) {
      console.log('[Palabra] 🚫 Blocking auto-subscribe for UID', user.uid, mediaType);
      return;  // Don't auto-subscribe
    }

    return originalSubscribe(user, mediaType);  // Normal UIDs proceed
  };

  console.log('[Palabra] ✓ Overridden client.subscribe() to filter translation UIDs');
}, [rtcClient]);
```

### Explicit Subscription

**Lines 612-662 - user-published event**:

```typescript
client.on('user-published', async (user, mediaType) => {
  const uidString = user.uid.toString();
  const uid = parseInt(uidString, 10);

  // Check if this UID is in activeTranslations (requested by THIS user)
  const translation = Array.from(activeTranslationsRef.current.values()).find(
    t => t.translationUid === uidString
  );

  if (!translation) {
    console.log('[Palabra] UID', uidString, 'not requested by this user (ignoring)');
    return;
  }

  const originalSubscribe = originalSubscribeRef.current;  // Bypass monkey-patch

  // Subscribe to audio (Anam 4000+ OR Palabra 3000+)
  if (mediaType === 'audio' && (isAnamUid(uid) || isPalabraUid(uid))) {
    await originalSubscribe(user, 'audio');
    user.audioTrack.play();
    console.log('[Palabra] ✓ Playing translation audio from UID', uidString);
  }

  // Subscribe to video (ONLY Anam 4000+)
  if (mediaType === 'video' && isAnamUid(uid)) {
    await originalSubscribe(user, 'video');

    // Play avatar video in source user's tile
    const sourceUid = translation.sourceUid;
    const sourceUser = client.remoteUsers.find(u => u.uid.toString() === sourceUid);
    if (sourceUser?.videoTrack) {
      sourceUser.videoTrack.stop();  // Stop original video
    }
    user.videoTrack.play(sourceUid);  // Play avatar in same tile

    console.log('[Palabra] ✓ Anam avatar video now playing in tile for UID', sourceUid);
  }
});
```

### Late-Arrival Handling

**Lines 455-502 - Check if UID already published**:

```typescript
// After backend returns UID, check if it already published (race condition)
const client = (rtcClient as any).client;
const remoteUsers = client.remoteUsers || [];  // Native SDK (not wrapper)
const existingUser = remoteUsers.find(u => u.uid.toString() === translationUid);

if (existingUser) {
  console.log('[Palabra] ⚡ Translation UID already published (late arrival)');

  const originalSubscribe = originalSubscribeRef.current;

  // Subscribe to audio
  if (existingUser.hasAudio) {
    await originalSubscribe(existingUser, 'audio');
    existingUser.audioTrack.play();
  }

  // Subscribe to video (Anam only)
  if (existingUser.hasVideo && isAnamUid(parseInt(translationUid, 10))) {
    await originalSubscribe(existingUser, 'video');
    // ... video replacement logic ...
  }
}
```

### State Management

**activeTranslations Map**:
- Key: `sourceUid` (user being translated, e.g., "100")
- Value: `{sourceUid, taskId, targetLanguage, translationUid}`

**Why useRef?** Event handlers capture state at registration time. Using ref ensures handler sees current Map:

```typescript
const activeTranslationsRef = useRef(activeTranslations);

useEffect(() => {
  activeTranslationsRef.current = activeTranslations;  // Sync on every change
}, [activeTranslations]);

// Event handler uses ref, not state
const translation = activeTranslationsRef.current.get(uid);
```

### Helper Functions

```typescript
const isPalabraUid = (uid: number) => uid >= 3000 && uid < 4000;
const isAnamUid = (uid: number) => uid >= 4000 && uid < 5000;
```

## Debug

### Expected Logs (Audio-Only Mode)

**Browser Console**:
```
[Palabra] ✓ Overridden client.subscribe() to filter translation UIDs
[Palabra] 🚫 Blocking auto-subscribe for translation UID 3000 audio
[Palabra] 📡 Translation UID published: 3000 Type: audio
[Palabra] ✓ Playing translation audio from UID 3000
```

### Expected Logs (Avatar Mode)

**Browser Console**:
```
[Palabra] ✓ Overridden client.subscribe() to filter translation UIDs
[Palabra] 🚫 Blocking auto-subscribe for translation UID 4000 audio
[Palabra] 📡 Translation UID published: 4000 Type: audio
[Palabra] ✓ Playing Anam avatar audio from UID 4000

[Palabra] 🚫 Blocking auto-subscribe for translation UID 4000 video
[Palabra] 📡 Translation UID published: 4000 Type: video
[Palabra] ✓ Anam avatar video now playing in tile for UID 100
```

**Backend Logs** (avatar mode only):
```bash
docker logs server -f | grep -i anam

[Anam] Got session token
[Anam] Connected to Anam WebSocket
[Anam] Init command sent successfully
[AgoraBot] Connected to channel as UID 5000
[AgoraBot] Audio callback - UID: 3000, Bytes: 3840
[Anam] Sent heartbeat
```

### Common Issues

#### Dual Audio (Hear Original + Translation)

**Cause**: Not unsubscribing from original user when translation starts

**Fix**: `TranslationProvider.tsx` already handles this - calls `unsubscribeFromUser(sourceUid)` before starting

#### Translation UID Not Subscribing

**Debug**:
1. Check backend response - contains UID 3000 or 4000?
2. Check `activeTranslations` Map - contains the UID?
3. Check `user-published` event - fires for the UID?
4. Check browser console - blocking or allowing?

**Common cause**: `activeTranslations` not updated before UID publishes (race condition). Fixed by updating `activeTranslationsRef.current` synchronously.

#### Wrong UID Range

**Symptom**: Backend returns UID 3000 when ENABLE_ANAM=true (or vice versa)

**Fix**: Check `palabra.go:334` - `streams[i].UID = anamUID` line should be inside `if enableAnam` block

## Testing Checklist

### Audio-Only Mode (ENABLE_ANAM=false)

- [ ] Backend returns UID 3000 in response
- [ ] User B hears translated audio from UID 3000
- [ ] User B still sees original video (not replaced)
- [ ] User C (late joiner) does NOT hear UID 3000 unless they request it
- [ ] Stopping translation re-subscribes to original audio

### Avatar Mode (ENABLE_ANAM=true)

- [ ] Backend returns UID 4000 in response
- [ ] User B hears translated audio from UID 4000
- [ ] User B sees Anam avatar video in source user's tile
- [ ] Original video stopped/hidden
- [ ] Avatar lips sync with French audio
- [ ] User C does NOT see/hear UID 4000 unless they request it

### Multi-User

- [ ] User B requests French translation of User A
- [ ] User C requests Spanish translation of User A
- [ ] Both hear their respective translations
- [ ] Translations don't interfere with each other

## Files Changed

**Backend**:
- `services/palabra.go` (lines 300-405) - Mode selection logic
- `services/anam_client.go` (NEW) - WebSocket client (avatar mode only)
- `services/agora_bot.go` (NEW) - Audio forwarder (avatar mode only)

**Frontend**:
- `customization/palabra/TranslationProvider.tsx`:
  - Lines 109-148: Monkey-patch subscribe
  - Lines 455-502: Late-arrival handling
  - Lines 612-662: user-published event handler
- `config.json` - Added `PALABRA_BACKEND_ENDPOINT`

## References

- **Anam Integration**: See `anam-integrate.md` for WebSocket protocol details
- **General Dev**: See `app-builder-dev.md` for build/deploy instructions
- **Palabra API**: Contact Palabra team for documentation
