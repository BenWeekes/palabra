# Agora App Builder - Development Guide

## Overview

Agora App Builder is a customizable video conferencing platform with real-time translation capabilities via Palabra and optional Anam avatar integration.

**Architecture**: React/TypeScript frontend + Go backend + Docker deployment

## Prerequisites

- Node.js 16+ and npm
- Go 1.21+
- Docker & Docker Compose
- Agora account with APP_ID and APP_CERTIFICATE

## Project Structure

```
app-builder-backend/          # Go backend
├── services/
│   ├── palabra.go           # Translation integration
│   ├── anam_client.go       # Anam WebSocket client (optional)
│   └── agora_bot.go         # Audio forwarding bot (optional)
├── .env                     # Docker environment (CRITICAL)
└── docker-compose.yml

app-builder-core/template/    # React frontend
├── customization/palabra/   # Translation UI
│   └── TranslationProvider.tsx
├── config.json              # Frontend config
└── package.json
```

## Build Instructions

### Backend

```bash
cd /Users/benweekes/work/palabra/app-builder-backend

# Option 1: Docker (recommended)
docker compose down && docker compose up -d --build

# Option 2: Local Go build
go mod download
go build -o bin/server ./cmd/video_conferencing
./bin/server
```

**Backend runs at**: `http://localhost:8081`

### Frontend

```bash
cd /Users/benweekes/work/palabra/app-builder-core/template

# Install dependencies
npm install

# Development server
npm run web

# Production build
npm run build
```

**Frontend runs at**: `http://localhost:9000`

## Configuration

### Backend (.env)

**Location**: `/app-builder-backend/.env` (used by Docker)

```bash
# Agora credentials
APP_ID=59367b7c63ca4472a529b3e96e0bafdd
APP_CERTIFICATE=1508f5aab7e14f5e91e26e1921084563

# Palabra credentials
PALABRA_CLIENT_ID=your_client_id
PALABRA_CLIENT_SECRET=your_client_secret

# Anam configuration (optional - see anam-integrate.md)
ENABLE_ANAM=false                                    # Set true for avatar mode
ANAM_API_KEY=your_anam_key
ANAM_AVATAR_ID=1bed9d5e-5e81-4d98-a04a-21e346bea528
ANAM_VIDEO_ENCODING=H264                             # Must be uppercase

# Server
PORT=8080
ALLOWED_ORIGIN=http://localhost:9000
```

**CRITICAL**: Docker uses `.env`, NOT `config.json`. APP_ID must match frontend.

### Frontend (config.json)

**Location**: `/app-builder-core/template/config.json`

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

**Note**: APP_ID must match backend `.env`. CUSTOMER_* fields exist but are unused (v007 token architecture).

## Deploy

### Local Development

```bash
# Terminal 1: Backend
cd app-builder-backend
docker compose up --build

# Terminal 2: Frontend
cd app-builder-core/template
npm run web
```

Open `http://localhost:9000`, create meeting, test translation.

### Production Checklist

- [ ] Set `ALLOWED_ORIGIN` to production frontend URL
- [ ] Enable HTTPS with valid SSL certificates
- [ ] Replace stub auth endpoints in `services/auth.go`
- [ ] Configure production Palabra/Anam credentials
- [ ] Set up monitoring and error alerting
- [ ] Test with multiple concurrent sessions
- [ ] Verify rate limits and quotas

## Debug

### Backend Logs

```bash
# All logs
docker logs server -f

# Palabra activity
docker logs server -f | grep -i palabra

# Anam activity (if ENABLE_ANAM=true)
docker logs server -f | grep Anam

# Audio forwarding (if ENABLE_ANAM=true)
docker logs server -f | grep "Forwarding audio"

# UID assignments
docker logs server 2>&1 | grep -E "(palabraUID|anamUID|botUID)"
```

### Frontend Logs

**Browser Console** (Chrome DevTools):

```javascript
// Expected logs when translation starts (audio-only mode):
[Palabra] ✓ Overridden client.subscribe() to filter translation UIDs
[Palabra] 🚫 Blocking auto-subscribe for translation UID 3000 audio
[Palabra] 📡 Translation UID published: 3000 Type: audio
[Palabra] ✓ Playing Palabra translation audio from UID 3000

// Expected logs (avatar mode, ENABLE_ANAM=true):
[Palabra] ✓ Playing Anam avatar audio from UID 4000
[Palabra] ✓ Anam avatar video now playing in tile for UID 81433
```

### Common Issues

#### RTC Error Code 5 / Token Invalid

**Symptom**: `AgoraRTCError CAN_NOT_GET_GATEWAY_SERVER: invalid token`

**Cause**: APP_ID mismatch between frontend `config.json` and backend `.env`

**Fix**: Ensure both use same APP_ID, restart both services

#### CORS Errors

**Symptom**: `Access to fetch at 'http://localhost:8081/...' blocked by CORS policy`

**Fix**:
1. Verify `ALLOWED_ORIGIN=http://localhost:9000` in backend `.env`
2. Restart backend: `docker compose down && docker compose up -d --build`
3. Check CORS middleware order in `server.go`:
```go
router.Use(c.Handler)  // MUST be before routes
router.HandleFunc("/v1/palabra/start", ...)
```

#### Translation Not Working

**Debug steps**:
1. Check backend logs: `docker logs server -f | grep Palabra`
2. Verify Palabra API call succeeded (200 response)
3. Check if translation UID (3000+ or 4000+) appears in browser console
4. Verify frontend subscribed to translation stream

**Common causes**:
- Wrong `PALABRA_BACKEND_ENDPOINT` in `config.json`
- Palabra API credentials invalid
- Response format mismatch (check backend returns `data.taskId`)

#### Subscribe is Not a Function

**Symptom**: `rtcClient.subscribe is not a function`

**Cause**: Using App Builder wrapper instead of native Agora SDK client

**Fix**: Already fixed in `TranslationProvider.tsx:109` - uses native client

#### Audio/Video Not Playing

**Check**:
- Browser console for errors
- User granted microphone/camera permissions
- Audio output device is working
- For avatar mode: ENABLE_ANAM=true in backend `.env`

### Clear Cache

If changes not taking effect:

```bash
cd app-builder-core/template
rm -rf .cache dist node_modules/.cache
npm run web
```

## Architecture Notes

### UID Ranges

- **1-2999**: Normal users (auto-subscribed by App Builder)
- **3000-3999**: Palabra translation audio-only (NOT auto-subscribed)
- **4000-4999**: Anam avatar video+audio (NOT auto-subscribed)
- **5000+**: Backend bot (subscribes to Palabra, forwards to Anam)

### Token Architecture (v007)

Both RTC and RTM use `APP_ID + APP_CERTIFICATE`. CUSTOMER_* fields are legacy/unused.

### Monkey-Patch Pattern

`TranslationProvider.tsx:109-148` overrides `client.subscribe()` to block auto-subscription for UIDs 3000-4999. Only explicitly requested translations are subscribed.

### Mode Detection

Client auto-detects mode from UID range:
- UID 3000-3999 → Audio-only (Palabra direct)
- UID 4000-4999 → Avatar (Anam + video)

No client-side config needed to switch modes - controlled by backend `ENABLE_ANAM` flag.

## Known Issues

### Palabra API Limitations

1. **Duplicate Tokens Rejected**
   - Palabra API rejects same RTC token for multiple UIDs
   - Prevents using Agora testing mode (token: "007...006" for all UIDs)
   - **Workaround**: Backend generates unique token per UID using APP_CERTIFICATE

2. **Task Bot UID Required**
   - Palabra requires task bot UID (200) even though it never publishes streams
   - Translation streams use separate UIDs (3000+)

3. **No Update Endpoint**
   - To change target language, must stop and restart translation task
   - Frontend implements this in `useSTTAPI.tsx`

### Development-Only Configuration

**RTMConfigure.tsx** - "Reload site?" prompt disabled for development:
- Lines 118-143: `beforeunload` event listener commented out
- **Production**: Re-enable this prompt before deploying

**VideoCall.tsx** - Infinite render loop fix:
- Line 177: Use `React.useMemo` for `DefaultWrapper` component
- Prevents re-creating component on every render

## Production Checklist

**Security & Auth**:
- [ ] Replace stub auth endpoints (`services/auth.go`) with real authentication
- [ ] Validate and sanitize all user inputs
- [ ] Implement rate limiting for API endpoints
- [ ] Set up proper CORS for production domain

**Configuration**:
- [ ] Update `ALLOWED_ORIGIN` to production frontend URL
- [ ] Set up HTTPS with valid SSL certificates
- [ ] Configure production Palabra API credentials
- [ ] Configure production Anam credentials (if using avatar mode)
- [ ] Remove or secure debug logging

**Frontend**:
- [ ] Re-enable "Reload site?" prompt in `RTMConfigure.tsx`
- [ ] Build production bundle: `npm run build`
- [ ] Test production build locally before deployment

**Monitoring & Operations**:
- [ ] Set up monitoring for translation task failures
- [ ] Configure error alerting (Sentry, CloudWatch, etc.)
- [ ] Set up uptime monitoring for backend
- [ ] Test with multiple concurrent translation sessions
- [ ] Verify Palabra/Anam billing limits and quotas
- [ ] Document operational runbooks for common issues

**Testing**:
- [ ] End-to-end testing with real users
- [ ] Load testing (multiple concurrent translations)
- [ ] Network interruption testing
- [ ] Browser compatibility testing

## Bug Fixes History

**December 2024**:
- Fixed dual audio bug (hearing original + translation simultaneously)
- Fixed APP_ID mismatch between frontend/backend
- Fixed `$config not defined` error (module vs runtime access)
- Fixed response format mismatch (`data.taskId`)
- Fixed CORS middleware ordering in `server.go`
- Fixed infinite render loop in `VideoCall.tsx`
- Cleaned up 22 verbose console.log statements

**January 2025 (Early)**:
- Fixed `rtcClient.subscribe is not a function` (use native SDK client)
- Fixed race condition (UID publishes before API response)
- Fixed audio play error (synchronous, not Promise)
- Fixed video element ID (use UID directly, not `video-{uid}`)
- Fixed UID collision (bot UID 5000, Anam UID 4000)

**January 2025 (Latest)**:
- **CRITICAL**: Fixed dual audio bug (sourceUid re-publish auto-subscribes)
- **CRITICAL**: Fixed video not restoring when translation stopped
- **OPTIMIZATION**: Added backend task deduplication (shared translations)

## References

- **Palabra Docs**: See `palabra-integrate.md`
- **Anam Docs**: See `anam-integrate.md`
- **Agora SDK**: https://docs.agora.io/en/video-calling/
- **App Builder**: https://appbuilder-docs.agora.io/
- **Test Reports**: `PALABRA-API-IMPROVEMENT-REPORT.md`, `PALABRA-IMPROVEMENTS-SUMMARY.md`
