# Palabra Real-Time Translation Integration Reference

## Task Deduplication & UID Management (December 23, 2024)

### Problem Statement
**Current Issues**:
- Multiple users requesting same translation (e.g., "User B → French") creates duplicate Palabra tasks
- Late-joining users cannot discover existing translation streams
- No mapping between Palabra UIDs (3000+) and source users/languages
- Users hear both original and translated audio (subscription bug)

### Solution: Backend-Managed Translation Registry

Backend maintains in-memory registry of active translation tasks as single source of truth.

### UID Range Management

| Range | Purpose | Assignment |
|-------|---------|------------|
| **3000-3099** | Palabra translation streams | Backend (palabra.go) |
| **10000-99999** | Regular conference participants | Backend (auth.go:109) |
| **200** | Palabra task bot | Backend (palabra.go) |

**Collision Prevention**: Backend validates user UIDs to prevent collision with reserved Palabra range (3000-3099).

### Task Deduplication Strategy

**Registry Key**: `channel:sourceUid:targetLang`

**Source Language Handling**:
- First requester sets source language
- Subsequent requesters inherit the initial source language setting
- Returns existing task if `(channel, sourceUid, targetLang)` already exists

**Example Flows**:

```
Request 1: User A translates User B (en-US) → French
  → Backend: No existing task found
  → Create Palabra task, assign UID 3000
  → Store: "channel:B:fr" → {taskId, sourceLanguage: "en-US", translationUid: "3000"}
  → Return: UID 3000

Request 2: User C translates User B (de-DE) → French
  → Backend: Found existing task "channel:B:fr"
  → Ignore de-DE source language (inherit en-US from registry)
  → Return: UID 3000 (NO Palabra API call)

Request 3: User D translates User B (en-US) → Spanish
  → Backend: No task found for "channel:B:es"
  → Create NEW Palabra task, assign UID 3001
  → Store: "channel:B:es" → {taskId, sourceLanguage: "en-US", translationUid: "3001"}
  → Return: UID 3001
```

**Benefits**:
- ✅ Eliminates duplicate Palabra tasks (saves API quota & bandwidth)
- ✅ Late joiners discover translations via `/tasks` endpoint
- ✅ Backend as single source of truth (vs ephemeral RTM state)
- ✅ Efficient: Users only subscribe to requested translations
- ✅ UID collision prevention

### Backend State Management

**Data Structure** (`services/palabra.go`):
```go
type ActiveTask struct {
    TaskID         string    // Palabra task UUID
    Channel        string    // Agora channel name
    SourceUID      string    // User being translated
    SourceLanguage string    // Set by first requester
    TargetLanguage string    // Translation target language
    TranslationUID string    // Palabra stream UID (e.g., "3000")
    CreatedAt      time.Time // Task creation timestamp
}

// Thread-safe in-memory registry
var activeTasks = sync.Map{}  // Key: "channel:sourceUid:targetLang"
var uidCounter uint32 = 3000  // Global UID counter for translation streams
```

### New Backend Endpoints

#### GET `/v1/palabra/tasks?channel=<name>`

**Purpose**: Discover active translation tasks in a channel

**Called By**: Frontend on channel join

**Response**:
```json
{
  "tasks": [
    {
      "sourceUid": "38324",
      "sourceLanguage": "en-US",
      "targetLanguage": "fr",
      "translationUid": "3000",
      "taskId": "4dadc853-8b50-4f44-8436-5e6bacc6494f"
    },
    {
      "sourceUid": "38324",
      "sourceLanguage": "en-US",
      "targetLanguage": "es",
      "translationUid": "3001",
      "taskId": "7f3e9a21-4c5b-4e8a-9d2f-8b7c6a5d4e3f"
    }
  ]
}
```

#### POST `/v1/palabra/start` (Modified)

**Changes**:
- Check registry before calling Palabra API
- If task exists: Return existing UID (no Palabra API call)
- If new: Create Palabra task, store in registry, return new UID

**Logic Flow**:
```go
func PalabraStart(req PalabraStartRequest) PalabraStartResponse {
    // Build registry key
    key := fmt.Sprintf("%s:%s:%s", req.Channel, req.SourceUID, req.TargetLanguages[0])

    // Check if task already exists
    if existing, ok := activeTasks.Load(key); ok {
        task := existing.(ActiveTask)
        return PalabraStartResponse{
            Success: true,
            TaskID:  task.TaskID,
            Streams: []PalabraStreamInfo{
                {UID: task.TranslationUID, Language: task.TargetLanguage},
            },
            Reused: true,  // Indicates task was reused
        }
    }

    // Create new Palabra task
    uidCounter++
    translationUid := fmt.Sprintf("%d", uidCounter)

    task := callPalabraAPI(req)

    // Store in registry
    activeTask := ActiveTask{
        TaskID:         task.TaskID,
        Channel:        req.Channel,
        SourceUID:      req.SourceUID,
        SourceLanguage: req.SourceLanguage,
        TargetLanguage: req.TargetLanguages[0],
        TranslationUID: translationUid,
        CreatedAt:      time.Now(),
    }
    activeTasks.Store(key, activeTask)

    return PalabraStartResponse{
        Success: true,
        TaskID:  task.TaskID,
        Streams: []PalabraStreamInfo{
            {UID: translationUid, Language: req.TargetLanguages[0]},
        },
        Reused: false,
    }
}
```

#### POST `/v1/palabra/stop` (Modified)

**Changes**:
- Remove task from registry after stopping
- Optionally: Free UID for reuse (requires UID recycling logic)

**Logic**:
```go
func PalabraStop(taskID string) error {
    // Find and remove task from registry
    var taskKey string
    activeTasks.Range(func(key, value interface{}) bool {
        task := value.(ActiveTask)
        if task.TaskID == taskID {
            taskKey = key.(string)
            return false  // Stop iteration
        }
        return true
    })

    if taskKey != "" {
        activeTasks.Delete(taskKey)
    }

    // Call Palabra API to stop task
    return callPalabraStopAPI(taskID)
}
```

### Updated Data Flow

#### User Joins Channel
```
Frontend:
  → GET /v1/palabra/tasks?channel=foo

Backend:
  → Query activeTasks registry
  → Return list of active tasks

Frontend:
  → Store in local availableTranslations registry
  → DO NOT auto-subscribe to Palabra UIDs
```

#### User Requests Translation
```
Frontend:
  → User clicks "Translate User B → French"
  → POST /v1/palabra/start {sourceUid: "B", targetLang: "fr"}

Backend:
  → Check registry for key "channel:B:fr"
  → IF EXISTS:
      → Return existing UID 3000 (no Palabra API call)
  → IF NEW:
      → Call Palabra API
      → Get taskId from response
      → Assign UID 3000
      → Store in registry: "channel:B:fr" → task
      → Return UID 3000

Frontend:
  → Unsubscribe from User B's original audio (UID 38324)
  → Wait for Palabra stream to publish (UID 3000)
  → Subscribe to UID 3000
  → User hears French translation
```

#### Palabra Stream Publishes
```
Frontend rtc-user-published event (UID 3000):
  → isPalabraUid(3000)? YES
  → Check if user requested translation for this UID
  → IF YES: Subscribe and play
  → IF NO: Ignore (do NOT auto-subscribe)
```

### Frontend Changes

#### TranslationProvider.tsx Updates

**1. Fetch Tasks on Channel Join**:
```typescript
useEffect(() => {
  if (!channel) return;

  const fetchTasks = async () => {
    try {
      const backendUrl = $config.PALABRA_BACKEND_ENDPOINT;
      const response = await fetch(`${backendUrl}/v1/palabra/tasks?channel=${channel}`);
      const data = await response.json();

      // Store available translations (for discovery, not auto-subscribe)
      data.tasks?.forEach(task => {
        availableTranslations.set(task.translationUid, {
          sourceUid: task.sourceUid,
          targetLanguage: task.targetLanguage,
          taskId: task.taskId,
        });
      });
    } catch (error) {
      console.error('[Palabra] Failed to fetch tasks:', error);
    }
  };

  fetchTasks();
}, [channel]);
```

**2. Remove Auto-Subscribe for Palabra UIDs**:
```typescript
// BEFORE: Auto-subscribed to all Palabra UIDs
const handleUserPublished = async (uid, trackType) => {
  if (isPalabraUid(uid)) {
    await rtcClient.subscribe(user, 'audio');  // ❌ Auto-subscribe
  }
};

// AFTER: Only subscribe to explicitly requested translations
const handleUserPublished = async (uid, trackType) => {
  if (isPalabraUid(uid)) {
    const translation = activeTranslations.find(t => t.translationUid === uid);
    if (translation) {
      await rtcClient.subscribe(user, 'audio');  // ✅ Only if requested
    }
    // Else: Ignore (another user is listening to this translation)
  }
};
```

**3. Fix Unsubscribe Bug**:
```typescript
const unsubscribeFromUser = async (uid) => {
  const user = remoteUsers.find(u => u.uid.toString() === uid);
  if (user && user.audioTrack) {
    user.audioTrack.stop();                    // Stop playback
    await rtcClient.unsubscribe(user, 'audio'); // ✅ Actually unsubscribe
  }
};
```

### Implementation Checklist

**Backend** (`services/palabra.go`):
- [ ] Add `ActiveTask` struct
- [ ] Add `activeTasks sync.Map` registry
- [ ] Add `uidCounter` global variable
- [ ] Implement `GET /v1/palabra/tasks` handler
- [ ] Modify `POST /v1/palabra/start` to check registry
- [ ] Modify `POST /v1/palabra/stop` to remove from registry
- [ ] Add UID validation in `auth.go` (block 3000-3099 range)

**Frontend** (`TranslationProvider.tsx`):
- [ ] Add `availableTranslations` state (Map)
- [ ] Add `useEffect` to fetch tasks on channel join
- [ ] Modify `handleUserPublished` to not auto-subscribe to Palabra UIDs
- [ ] Fix `unsubscribeFromUser` to call `rtcClient.unsubscribe()`
- [ ] Update `startTranslation` to handle reused tasks

**Testing**:
- [ ] Test duplicate task prevention (2 users request same translation)
- [ ] Test late joiner discovery (join channel with existing translations)
- [ ] Test UID collision prevention (user join should not assign 3000+)
- [ ] Test audio subscription fix (no dual audio)
- [ ] Test multi-language scenario (User B → French + Spanish)

## Files Changed/Added

### Backend (`/app-builder-backend/`)

#### New Files
- **`services/palabra.go`** - Palabra integration handler
  - Handles translation task lifecycle (start/stop/list)
  - In-memory task registry with deduplication (`activeTasks sync.Map`)
  - Generates unique RTC tokens per UID
  - Calls Palabra API with proper authentication
  - Task registry prevents duplicate translations (saves API quota)
  - Routes: `POST /v1/palabra/start`, `POST /v1/palabra/stop`, `GET /v1/palabra/tasks`

- **`services/auth.go`** - Stub authentication endpoints (local dev only)
  - `POST /v1/login` - Returns dummy auth token
  - `GET /v1/user/details` - Returns dummy user data
  - `POST /v1/channel` - Creates meeting channel, generates tokens
  - `POST /v1/channel/join` - Join meeting, generates tokens
  - `POST /v1/channel/share` - Share meeting link
  - Generates UIDs in range 10000-99999 (avoids Palabra range 3000-3099)
  - Generates RTC and RTM tokens using v007 architecture

- **`.env`** - Environment configuration (Docker)
  - Agora credentials (APP_ID, APP_CERTIFICATE)
  - Palabra credentials (PALABRA_CLIENT_ID, PALABRA_CLIENT_SECRET)
  - Server config (PORT, ALLOWED_ORIGIN)
  - **NOTE**: Docker uses `.env`, not `config.json`

#### Modified Files
- **`cmd/video_conferencing/server.go`**
  - Fixed CORS middleware ordering (must be before routes)
  - Registered Palabra routes: `/v1/palabra/start`, `/v1/palabra/stop`, `/v1/palabra/tasks`
  - Registered stub auth routes for local development

- **`config.json`**
  - Updated APP_ID to match frontend
  - **NOTE**: Not used by Docker (uses `.env` instead)

- **`docker-compose.yml`**
  - Configured to use `.env` file for environment variables

**Note**: RTM token generation in `utils/tokens.go` already uses APP_ID/APP_CERTIFICATE correctly (v007 architecture). No changes needed.

### Frontend (`/app-builder-core/template/`)

**Note**: The `customization/` folder is not tracked by git (it's for user customizations). Files created there are new additions to your local setup.

#### New Files (Customization Layer)
- **`customization/index.tsx`** - Main customization entry point
  - Registers `TranslationProvider` as VideoCall wrapper using customization API
  - Proper App Builder customization pattern (doesn't modify core code)

- **`customization/palabra/TranslationProvider.tsx`** - Main translation orchestration
  - Manages translation state (`activeTranslations`, `availableTranslations`)
  - Fetches existing tasks on channel join (`GET /v1/palabra/tasks`)
  - Handles audio subscription/unsubscription
  - Properly unsubscribes from original audio before subscribing to translation
  - Only subscribes to Palabra UIDs if explicitly requested (prevents auto-subscribe)
  - Context provider for translation functionality

- **`customization/palabra/TranslationMenuItem.tsx`** - UI component
  - Dropdown menu item in user action menu
  - Language selection interface
  - Shows available languages (Spanish, French, German, Japanese, Chinese, Portuguese, Italian, Korean)
  - Displays current translation status

- **`customization/palabra/register-translation-menu.tsx`** - Menu registration helper
  - Registers translation menu item in user action menu
  - Provides `useRegisterTranslationMenu()` hook
  - Handles menu visibility and positioning

- **`src/subComponents/caption/usePalabraAPI.tsx`** - Palabra API client hook
  - HTTP client for Palabra backend endpoints
  - `start()` - POST /v1/palabra/start
  - `stop()` - POST /v1/palabra/stop
  - `getTasks()` - GET /v1/palabra/tasks
  - Uses `PALABRA_BACKEND_ENDPOINT` from config.json

#### Modified Files
- **`src/pages/VideoCall.tsx`**
  - Fixed infinite render loop using `React.useMemo` for DefaultWrapper component
  - Fixes bug where React.Fragment doesn't accept props
  - **NOTE**: Translation integration is in `customization/index.tsx` (proper pattern)

- **`src/subComponents/caption/useSTTAPI.tsx`**
  - Modified to use Palabra API instead of Agora STT
  - Implements `start()`, `stop()`, and `update()` via `usePalabraAPI` hook
  - `update()` implemented by stopping and restarting task (Palabra has no update endpoint)
  - **⚠️ WARNING**: This modifies core App Builder code - consider if caption functionality should be entirely in customization layer

### Configuration Files

#### Frontend Config (`app-builder-core/template/config.json`)
```json
{
  "APP_ID": "59367b7c63ca4472a529b3e96e0bafdd",
  "APP_CERTIFICATE": "1508f5aab7e14f5e91e26e1921084563",
  "PALABRA_BACKEND_ENDPOINT": "http://localhost:8081",
  "ENABLE_STT": true,
  "ENABLE_CAPTION": true
}
```

#### Backend Config (`app-builder-backend/.env`)
```bash
APP_ID=59367b7c63ca4472a529b3e96e0bafdd
APP_CERTIFICATE=1508f5aab7e14f5e91e26e1921084563
PALABRA_CLIENT_ID=211ebf3e524bc67baf007a5fe33d6828
PALABRA_CLIENT_SECRET=86d0cd8ea40e8f4d74908ca0d0c33d14704cbe69e86e2391bcd9a39cca1985c9
PORT=8080
ALLOWED_ORIGIN=http://localhost:9000
```

## Architecture

### Components
**Frontend** (`/app-builder-core/template/`):
- `customization/palabra/TranslationProvider.tsx` - Translation UI
- `customization/palabra/TranslationMenuItem.tsx` - Menu integration
- `src/subComponents/caption/usePalabraAPI.tsx` - API hook

**Backend** (`/app-builder-backend/`):
- `services/palabra.go` - Palabra handler
- `services/auth.go` - Auth stubs
- `cmd/video_conferencing/server.go` - Server

### Data Flow
```
User clicks "Translate Audio"
→ TranslationProvider.startTranslation()
→ POST http://localhost:8081/v1/palabra/start
→ Backend generates unique RTC tokens per UID
→ Palabra API creates translation task
→ Translation stream (UID 3000+) publishes to channel
→ Frontend subscribes to translation stream
→ Translated audio plays
```

## Quick Start

### Backend Setup
```bash
cd /Users/benweekes/work/palabra/app-builder-backend

# Configure .env with your credentials
# APP_ID, APP_CERTIFICATE, PALABRA_CLIENT_ID, PALABRA_CLIENT_SECRET

# Start backend
docker compose up --build
```

Backend runs at: `http://localhost:8081`

### Frontend Setup
```bash
cd /Users/benweekes/work/palabra/app-builder-core/template

# Configure config.json
# Set PALABRA_BACKEND_ENDPOINT to http://localhost:8081

# Start frontend
npm run web
```

Frontend runs at: `http://localhost:9000`

### Test Translation
1. Open `http://localhost:9000` in two browser tabs
2. Create a meeting in tab 1, join in tab 2
3. In tab 1: Click "..." on remote user → "Translate Audio" → Select language
4. Speak in tab 2, verify translated audio plays in tab 1

## API Endpoints

### POST /v1/palabra/start
**Request**:
```json
{
  "channel": "meeting-channel-name",
  "sourceUid": "123",
  "sourceLanguage": "en-US",
  "targetLanguages": ["es", "fr"]
}
```

**Response**:
```json
{
  "ok": true,
  "data": {
    "taskId": "4dadc853-8b50-4f44-8436-5e6bacc6494f",
    "streams": [{"uid": "3000", "language": "es", "token": "unique-rtc-token"}]
  }
}
```

### POST /v1/palabra/stop
**Request**:
```json
{"taskId": "4dadc853-8b50-4f44-8436-5e6bacc6494f"}
```

### Stub Auth (Local Dev Only)
- `GET /v1/user/details` - Returns dummy user
- `POST /v1/login` - Returns dummy token

## Testing

### Test Scripts
**test-palabra-with-tokens.sh** - Production setup with unique tokens (PASSES)
**test-palabra-without-tokens.sh** - Duplicate tokens test (FAILS - Palabra issue)
**test-palabra-uid-formats.sh** - UID format variations

```bash
./run-all-palabra-tests.sh  # Run all tests
```

### Manual Test Flow
1. Start backend: `docker compose up --build`
2. Start frontend: `npm run web`
3. Open http://localhost:9000 in two tabs
4. Create meeting in tab 1, join in tab 2
5. Tab 1: Click "..." on remote user → "Translate Audio" → Select language
6. Verify translated audio plays in tab 1

## Known Issues

### 1. Duplicate Tokens Rejected (UNFIXED - Dec 2024)
**Issue**: Palabra rejects same RTC token for multiple UIDs
**Impact**: Cannot use Agora testing mode (token: APP_ID for all UIDs)
**Workaround**: Backend generates unique token per UID using APP_CERTIFICATE
**Test**: `test-palabra-without-tokens.sh` confirms still broken

**Error Response**:
```json
{
  "ok": false,
  "errors": [{
    "title": "Unprocessable Entity",
    "detail": "invalid task: task has duplicate tokens",
    "status": 422,
    "error_code": 200010
  }]
}
```

### 2. UID Format Requirements
**Working**: String UID `"101"`, String with zeros `"00101"`
**Failing**: Integer `101`, Hex `"0x65"`, Large numbers `4294967295`
**Conclusion**: UIDs must be decimal strings

### 3. Task Bot UID Required
Palabra requires task bot UID (200) even though it never publishes streams.

### 4. No Update Endpoint
To change target language, must stop and restart translation task:
```typescript
const update = async (botUid, translationConfig) => {
  await palabraAPI.stop(botUid);
  return await palabraAPI.start(botUid, translationConfig);
};
```

## Current Implementation Architecture

### Customization Files
`/template/customization/palabra/`:
- `TranslationProvider.tsx` - Main translation context provider
- `TranslationMenuItem.tsx` - Menu item UI component
- `register-translation-menu.tsx` - Menu registration helper

### Integration Pattern
The integration uses App Builder's Customization API (proper pattern):

```typescript
// customization/index.tsx
import {customize} from 'customization-api';
import {TranslationProvider} from './palabra/TranslationProvider';

const customization = customize({
  components: {
    videoCall: {
      wrapper: VideoCallWrapper,  // Wraps with TranslationProvider
    },
  },
});

export default customization;
```

This approach avoids modifying core App Builder code (unlike directly editing VideoCall.tsx).

## Implementation Timeline

| Phase | Hours | Dependencies |
|-------|-------|--------------|
| Lambda Setup | 2-4 | Palabra credentials |
| App Builder Review | 2-3 | None |
| UI Component Dev | 8-12 | Lambda working |
| Core Integration | 4-8 | UI ready |
| Stream Handling | 4-6 | Core done |
| Testing | 8-12 | All complete |
| Documentation | 4-6 | Testing done |
| Deployment | 2-4 | All complete |

Total: 40-67 hours (5-8 days, 1 developer)

## Palabra API Payload Structure

```json
{
  "agoraAppId": "your_app_id",
  "channel": "channel_name",
  "remote_uid": "101",
  "local_uid": "200",
  "token": "006def...",
  "speech_recognition": {
    "source_language": "en",
    "options": {}
  },
  "translations": [
    {
      "local_uid": "3000",
      "token": "006abc...",
      "target_language": "es",
      "audioOutput": {"enabled": true, "voiceCloning": false},
      "textOutput": {"enabled": false},
      "options": {}
    }
  ],
  "settings": {
    "enableTranscription": false,
    "enableVoiceCloning": false
  }
}
```

## Translation Modes

**audio** - User speaks → Palabra translates → Publishes translated audio stream
**text** - User speaks → Palabra translates → Sends text via WebSocket/RTM (subtitles)
**both** - User speaks → Palabra translates → Publishes audio + sends text

Query parameter: `?translation_mode=audio|text|both`

## Test Results (Dec 2024)

### Production Setup (Unique Tokens) - WORKING
```json
{
  "success": true,
  "taskId": "6602363d-b3c4-44a9-96c0-58cdc8de9fa3",
  "streams": [
    {"uid": "3000", "language": "es"},
    {"uid": "3001", "language": "fr"}
  ]
}
```

### Testing Mode (Duplicate Tokens) - BROKEN
```json
{
  "ok": false,
  "errors": [{
    "title": "Unprocessable Entity",
    "detail": "invalid task: task has duplicate tokens",
    "status": 422,
    "error_code": 200010
  }]
}
```

**Conclusion**: Duplicate tokens issue NOT fixed. Must use unique tokens per UID.

## References

- Palabra API: https://docs.palabra.ai/docs/partner-integrations/agora/
- Agora RTC Token: https://docs.agora.io/en/video-calling/develop/authentication-workflow
- App Builder: https://appbuilder-docs.agora.io/
