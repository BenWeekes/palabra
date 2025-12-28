# Agora App Builder - Project Notes

## Palabra Real-Time Translation Integration

### Overview
This project integrates Palabra's real-time speech translation API with Agora App Builder to provide live translation of audio streams during video conferences.

### Recent Updates (December 22, 2024)

#### Bug Fixes & Debugging Improvements

**1. Fixed Infinite Render Loop** (VideoCall.tsx:177)
- **Problem**: Creating `DefaultWrapper` component inside `useCustomization` callback caused infinite re-renders
- **Solution**: Used `React.useMemo` to maintain stable component reference
```typescript
const DefaultWrapper = React.useMemo(() => {
  const Component: React.FC<any> = ({children}) => <>{children}</>;
  return Component;
}, []);
```

**2. Disabled "Reload site?" Prompt** (RTMConfigure.tsx:118-143)
- **Problem**: Annoying `beforeunload` browser prompt during development
- **Solution**: Commented out `handBrowserClose` event listener
- **Note**: Re-enable for production if needed

**3. Cleaned Up Verbose Logging** (22 console.log statements removed)
- `customization/palabra/TranslationProvider.tsx` - 13 logs removed
- `customization/palabra/register-translation-menu.tsx` - 4 logs removed
- `src/subComponents/caption/usePalabraAPI.tsx` - 2 logs removed
- `src/subComponents/caption/useSTTAPI.tsx` - 3 logs removed
- **Kept**: Only error logs and translation failure alerts

**4. Enhanced Error Logging**

*RTM Login Errors* (RTMConfigure.tsx:164-178):
```typescript
console.error('[RTM] Login failed with error:', {
  errorCode: error?.code,
  errorMessage: error?.message,
  uid: localUid.toString(),
  hasToken: !!rtcProps.rtm,
  appId: $config.APP_ID,
  backendEndpoint: $config.BACKEND_ENDPOINT,
});

if (error?.code === 5) {
  console.error('[RTM] ERROR CODE 5: Signature verification failed. This usually means:');
  console.error('  1. Backend is not running at', $config.BACKEND_ENDPOINT);
  console.error('  2. APP_ID mismatch between frontend and backend');
  console.error('  3. Invalid or expired RTM token');
}
```

*RTC Join Errors* (Join.tsx:126-132):
```typescript
// Debug: Log APP_ID to help diagnose token issues
if (!$config.APP_ID || $config.APP_ID === '') {
  console.error('[JOIN] ERROR: APP_ID is not set in config.json');
  alert('Configuration Error: APP_ID is missing. Check config.json');
  return;
}
console.log('[JOIN] Using APP_ID:', $config.APP_ID);
```

**5. Fixed Backend Endpoint Configuration**
- **Issue**: `usePalabraAPI.tsx` was using `BACKEND_ENDPOINT` instead of `PALABRA_BACKEND_ENDPOINT`
- **Risk**: Could trigger authentication loop (see "CRITICAL" note in config section)
- **Fix**: Now uses `PALABRA_BACKEND_ENDPOINT` with fallback
```typescript
const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.PALABRA_BACKEND_ENDPOINT;
```

**6. Fixed Backend APP_ID Mismatch** (.env)
- **Problem**: Backend .env had wrong APP_ID (`a569f8fb0309417780b793786b534a86`)
- **Solution**: Updated to match frontend (`59367b7c63ca4472a529b3e96e0bafdd`)
- **Result**: RTC join now works, Error Code 5 resolved

**7. Fixed Dual Audio Subscription Bug** (TranslationProvider.tsx:160)
- **Problem**: Users heard both original English and translated audio simultaneously
- **Cause**: `unsubscribeFromUser()` only called `audioTrack.stop()` but didn't actually unsubscribe from RTC stream
- **Solution**: Added `await rtcClient.unsubscribe(user, 'audio')` to properly unsubscribe
- **Result**: Only translated audio plays when translation is active

### Architecture

#### Components
1. **Frontend (React/TypeScript)**
   - Location: `/app-builder-core/template/`
   - Translation UI: `customization/palabra/TranslationProvider.tsx`
   - Menu Integration: `customization/palabra/TranslationMenuItem.tsx`
   - API Hook: `src/subComponents/caption/usePalabraAPI.tsx`

2. **Backend (Go)**
   - Location: `/app-builder-backend/`
   - Palabra Handler: `services/palabra.go`
   - Auth Stubs: `services/auth.go`
   - Server: `cmd/video_conferencing/server.go`

#### Data Flow
```
User clicks "Translate Audio"
→ TranslationProvider.startTranslation()
→ POST http://localhost:8081/v1/palabra/start
→ Backend generates unique RTC tokens per UID
→ Palabra API creates translation task
→ Translation stream (UID 3000+) publishes to channel
→ Frontend subscribes to translation stream
→ Translated audio plays to user
```

### Configuration

#### Backend (`/app-builder-backend/.env`)
**IMPORTANT**: Docker uses `.env` file, not `config.json`
```bash
# Agora Credentials (v007 token - used for BOTH RTC and RTM)
APP_ID=59367b7c63ca4472a529b3e96e0bafdd
APP_CERTIFICATE=1508f5aab7e14f5e91e26e1921084563

# Note: CUSTOMER_ID/CUSTOMER_CERTIFICATE not used in v007 architecture
# Both RTC and RTM use APP_ID + APP_CERTIFICATE

# Palabra Credentials
PALABRA_CLIENT_ID=211ebf3e524bc67baf007a5fe33d6828
PALABRA_CLIENT_SECRET=86d0cd8ea40e8f4d74908ca0d0c33d14704cbe69e86e2391bcd9a39cca1985c9

# Server
PORT=8080
ALLOWED_ORIGIN=http://localhost:9000
```

#### Frontend (`/app-builder-core/template/config.json`)
```json
{
  "APP_ID": "59367b7c63ca4472a529b3e96e0bafdd",
  "APP_CERTIFICATE": "1508f5aab7e14f5e91e26e1921084563",
  "CUSTOMER_ID": "40b25d211955491580720cb54099c3c4",
  "CUSTOMER_CERTIFICATE": "555d0c42035c450a9b562ec20773d6b4",
  "PALABRA_BACKEND_ENDPOINT": "http://localhost:8081",
  "ENABLE_STT": true,
  "ENABLE_CAPTION": true
}
```

**Note**: `CUSTOMER_ID` and `CUSTOMER_CERTIFICATE` exist in config but are **not used**. The v007 token system uses `APP_ID` + `APP_CERTIFICATE` for both RTC and RTM.

**CRITICAL**: Use `PALABRA_BACKEND_ENDPOINT` instead of `BACKEND_ENDPOINT` to avoid triggering authentication flow.

### Running the Application

#### Backend
```bash
cd /Users/benweekes/work/keys/appb/app-builder-backend
docker compose up --build
```

Server runs at: `http://localhost:8081`

#### Frontend
```bash
cd /Users/benweekes/work/keys/appb/app-builder-core/template
npm run web
```

App runs at: `http://localhost:9000`

### API Endpoints

#### Palabra Translation
- **Start Translation**: `POST /v1/palabra/start`
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
      "streams": [
        {
          "uid": "3000",
          "language": "es",
          "token": "unique-rtc-token"
        }
      ]
    }
  }
  ```

- **Stop Translation**: `POST /v1/palabra/stop`
  ```json
  {
    "taskId": "4dadc853-8b50-4f44-8436-5e6bacc6494f"
  }
  ```

#### Stub Auth (Local Dev Only)
- `GET /v1/user/details` - Returns dummy user data
- `POST /v1/login` - Returns dummy auth token

### Troubleshooting

#### Token/Authentication Errors

**RTC Error Code 5 / CAN_NOT_GET_GATEWAY_SERVER**
- **Symptom**: `AgoraRTCError CAN_NOT_GET_GATEWAY_SERVER: invalid token`
- **Cause**: APP_ID mismatch between frontend config.json and backend .env
- **Solution**: Ensure both use same APP_ID (`59367b7c63ca4472a529b3e96e0bafdd`)

**RTM/RTC Token Errors**
- **Root Cause**: APP_ID mismatch between frontend `config.json` and backend `.env`
- **Solution**: Ensure both use same APP_ID (`59367b7c63ca4472a529b3e96e0bafdd`)
- **Note**: v007 token architecture - RTC and RTM use the same APP_ID + APP_CERTIFICATE
- Restart both services after .env changes: `docker compose down && docker compose up -d --build`

#### CORS Errors
**Symptom**: `Access to fetch at 'http://localhost:8081/...' blocked by CORS policy`

**Solution**:
1. Verify CORS middleware is applied BEFORE routes in `server.go`:
   ```go
   router.Use(c.Handler)  // CORS middleware first
   router.HandleFunc("/v1/palabra/start", ...)  // Routes after
   ```

2. Check `ALLOWED_ORIGIN` in backend config.json matches frontend URL

3. Clear webpack cache if changes don't take effect:
   ```bash
   cd /Users/benweekes/work/keys/appb/app-builder-core/template
   rm -rf .cache dist node_modules/.cache
   npm run web
   ```

#### $config Not Defined Error
**Symptom**: `ReferenceError: $config is not defined`

**Cause**: Accessing $config at module load time instead of runtime

**Solution**: Move config access inside component functions:
```typescript
// WRONG:
const BACKEND_URL = $config.PALABRA_BACKEND_ENDPOINT;  // Module-level

// CORRECT:
const startTranslation = async () => {
  const backendUrl = $config.PALABRA_BACKEND_ENDPOINT;  // Inside function
};
```

#### Translation Not Working
**Symptom**: "Translate Audio" button doesn't produce translated audio

**Debugging Steps**:
1. Check browser console for errors
2. Check backend logs: `docker compose logs -f server | grep -i palabra`
3. Verify Palabra API was called successfully
4. Check if translation stream UID (3000+) appears in remote users
5. Verify subscription to translation stream succeeded

**Common Issues**:
- Wrong backend URL (check `PALABRA_BACKEND_ENDPOINT`)
- Response format mismatch (frontend expects `data.taskId`, not `data.translation_task.task_id`)
- Token generation failures
- Palabra API rate limits or quota exceeded

#### Cannot Read Properties of Undefined
**Symptom**: `TypeError: Cannot read properties of undefined (reading 'task_id')`

**Cause**: Backend response format doesn't match frontend expectations

**Solution**: Backend returns `data.taskId` directly. Ensure TranslationProvider.tsx uses:
```typescript
const translation: ActiveTranslation = {
  sourceUid,
  taskId: data.taskId,  // NOT data.translation_task.task_id
  targetLanguage,
  translationUid: translationStream.uid,
};
```

### Known Issues

#### Palabra API Limitations

1. **Duplicate Tokens Rejected** (Unfixed as of December 2024)
   - Palabra API rejects same RTC token for multiple UIDs
   - Prevents using Agora testing mode (token: "007...006" for all UIDs)
   - **Workaround**: Backend generates unique token per UID using APP_CERTIFICATE
   - **Test Scripts**: See `test-palabra-without-tokens.sh` for reproduction

2. **Task Bot UID Required**
   - Palabra requires task bot UID (200) even though it never publishes streams
   - Translation streams use separate UIDs (3000+)

3. **No Update Endpoint**
   - To change target language, must stop and restart translation task
   - Frontend implements this in `useSTTAPI.tsx`:
     ```typescript
     const update = async (botUid, translationConfig) => {
       await palabraAPI.stop(botUid);
       return await palabraAPI.start(botUid, translationConfig);
     };
     ```

### Testing

#### Manual Testing
1. Start backend and frontend servers
2. Open `http://localhost:9000` in two browser tabs
3. Create a meeting room, copy join link to second tab
4. In tab 2, click remote user's "..." menu → "Translate Audio"
5. Select target language (e.g., French)
6. Speak in tab 1, verify translated audio plays in tab 2

#### API Testing
```bash
# Test with unique tokens (production mode) - PASSES
./test-palabra-with-tokens.sh

# Test with duplicate tokens (Agora testing mode) - FAILS
./test-palabra-without-tokens.sh

# Test UID format variations - MIXED RESULTS
./test-palabra-uid-formats.sh
```

### Implementation Timeline

#### December 2024 - Local Integration
1. ✅ Configured Agora credentials in backend/frontend
2. ✅ Fixed CORS middleware ordering
3. ✅ Created stub auth endpoints for local development
4. ✅ Fixed `$config` access timing issue
5. ✅ Added `PALABRA_BACKEND_ENDPOINT` to avoid auth loop
6. ✅ Fixed response format mismatch (`data.taskId` vs `data.translation_task.task_id`)
7. ✅ Integrated Palabra API with TranslationProvider

#### November 2024 - Initial Testing
- Discovered duplicate tokens issue
- Created comprehensive test suite
- Reported findings to Palabra team

### Development Notes

#### Code Patterns

**Backend Token Generation**:
```go
// Generate unique token per UID
func generateToken(channel string, uid uint32) string {
    appID := viper.GetString("APP_ID")
    appCertificate := viper.GetString("APP_CERTIFICATE")
    token, _ := rtctokenbuilder.BuildTokenWithUid(
        appID,
        appCertificate,
        channel,
        uid,
        rtctokenbuilder.RolePublisher,
        uint32(time.Now().Unix())+3600,
    )
    return token
}
```

**Frontend Translation Management**:
```typescript
// Subscribe/unsubscribe pattern
const startTranslation = async (sourceUid, sourceLanguage, targetLanguage) => {
  // 1. Unsubscribe from original audio
  await unsubscribeFromUser(sourceUid);

  // 2. Call backend to start Palabra task
  const response = await fetch(`${backendUrl}/v1/palabra/start`, {...});

  // 3. Store translation info
  setActiveTranslations(prev => {
    const newMap = new Map(prev);
    newMap.set(sourceUid, translation);
    return newMap;
  });

  // 4. Wait for translation stream to publish
  // 5. Subscribe to translation stream (handled by rtc-user-published listener)
};
```

#### Key Files Modified

**Backend**:
- `services/palabra.go` - NEW FILE - Palabra integration with task registry and deduplication
- `services/auth.go` - NEW FILE - Stub auth endpoints for local development
- `.env` - NEW FILE - Docker environment configuration (fixed APP_ID mismatch)
- `cmd/video_conferencing/server.go` - Fixed CORS middleware order, registered Palabra routes

**Frontend (Original Integration)**:
- `customization/palabra/TranslationProvider.tsx` - Translation UI and logic
- `customization/palabra/TranslationMenuItem.tsx` - Menu integration
- `customization/palabra/register-translation-menu.tsx` - Registration helper
- `config.json` - Added `PALABRA_BACKEND_ENDPOINT`
- `src/subComponents/caption/usePalabraAPI.tsx` - NEW FILE - Palabra API hook
- `src/subComponents/caption/useSTTAPI.tsx` - Modified to use Palabra instead of Agora STT

**Frontend (Dec 22-23, 2024 - Bug Fixes)**:
- `src/pages/VideoCall.tsx` - Fixed infinite render loop with React.useMemo
- `customization/palabra/TranslationProvider.tsx` - Fixed dual audio bug (proper unsubscribe), cleaned up logging
- `customization/index.tsx` - TranslationProvider integration via customization API
- `src/subComponents/caption/useSTTAPI.tsx` - Modified to use Palabra API
- `src/subComponents/caption/usePalabraAPI.tsx` - NEW FILE - Palabra API hook

### Production Deployment Checklist

- [ ] Replace stub auth endpoints with real authentication
- [ ] Update `ALLOWED_ORIGIN` to production frontend URL
- [ ] Set up HTTPS with valid SSL certificates
- [ ] Configure production Palabra API credentials
- [ ] Re-enable "Reload site?" prompt in `RTMConfigure.tsx` (currently disabled for dev)
- [ ] Review and adjust error logging levels (currently verbose for debugging)
- [ ] Set up monitoring for translation task failures
- [ ] Add rate limiting for Palabra API calls
- [ ] Implement proper error handling and user notifications
- [ ] Test with multiple concurrent translation sessions
- [ ] Verify billing/quota limits with Palabra
- [ ] Document operational runbooks for common issues

### References

- **Palabra API Documentation**: Contact Palabra team for latest docs
- **Agora RTC Token Builder**: https://docs.agora.io/en/video-calling/develop/authentication-workflow
- **App Builder Docs**: https://appbuilder-docs.agora.io/
- **Test Reports**:
  - `PALABRA-API-IMPROVEMENT-REPORT.md`
  - `PALABRA-IMPROVEMENTS-SUMMARY.md`
oo
