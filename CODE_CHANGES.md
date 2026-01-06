# Code Changes Summary

## Overview

This document summarizes the code changes for Palabra + Anam integration in Agora App Builder. Use this as a reference for creating git commits or reviewing implementation details.

## Backend Changes

### New Files

**services/anam_client.go** (485 lines):
- Anam WebSocket client
- Two-step auth: `POST /auth/session-token` → `POST /engine/session`
- Commands: init, voice, voice_end, heartbeat
- 500ms delay after init (per protocol)
- Handles WebSocket 301 redirects
- All messages use snake_case with event_id

**services/agora_bot.go** (393 lines):
- Agora SDK wrapper (CGO bindings)
- Joins channel as subscriber (UID 5000)
- FIXED: Set `AutoSubscribeAudio: false` (line 68)
  - Prevents auto-subscribing to ALL users (19874, 3000, 4000)
  - Only subscribes to target UID 3000 explicitly (line 120)
  - Reduces audio processing overhead by 66%
- Voice Activity Detection (VAD):
  - Ring buffer (10 frames = 100ms pre-roll)
  - RMS threshold = 100 (int64 for type safety)
  - Sends pre-roll when voice detected
  - Continues for 500ms after silence
  - Sends voice_end after 500ms silence
- Audio callback forwards PCM to Anam
- Upsamples 16kHz → 24kHz
- Logging: `🎤 VOICE DETECTED`, `🔇 SILENCE`, `📊 stats`

### Modified Files

**services/palabra.go** (lines 300-405):
- Added `ENABLE_ANAM` flag logic
- When true:
  - Creates Anam WebSocket session
  - Spawns bot with UID 5000
  - Changes response UID: 3000 → 4000
- When false: Original behavior (UID 3000)

**Dockerfile**:
- Set `CGO_ENABLED=1` (Agora SDK requirement)
- Platform: `linux/amd64` (x86-64 only)
- Copy Agora SDK `.so` files to `/usr/local/lib`
- Set `LD_LIBRARY_PATH=/usr/local/lib`

**.env** (new environment variables):
```bash
ENABLE_ANAM=false
ANAM_API_KEY=base64_key
ANAM_BASE_URL=https://api.anam.ai/v1
ANAM_AVATAR_ID=uuid
ANAM_VIDEO_ENCODING=H264
ANAM_QUALITY=high
```

## Frontend Changes

### Modified Files

**customization/palabra/TranslationProvider.tsx**:

**Lines 109-160 - Monkey-patch subscription**:
- Extract native Agora SDK client: `(rtcClient as any).client`
- Override `client.subscribe()` to block UIDs 3000-4999
- Block sourceUid if it's being translated (prevents dual audio)
- Store original subscribe in ref for manual use
- Prevents auto-subscription to translation UIDs
- Added logging for debugging: `✅ Allowing` / `🚫 Blocking`

**Lines 277-324 - Unsubscribe from source user**:
- CRITICAL FIX: Use `client.remoteUsers` (native SDK), NOT `rtcClient.remoteUsers` (wrapper)
- Stop audio playback before unsubscribing
- Handle case where user has `hasAudio` but no `audioTrack`
- Detailed logging shows remoteUsers UIDs for debugging
- Called when translation starts (prevents dual audio)

**Lines 355-370 - Pre-block sourceUid in Map**:
- Store placeholder in `activeTranslations` BEFORE backend call
- Immediately blocks sourceUid from auto-resubscribe
- Prevents race condition where UID publishes before API response
- Logging: `🔒 Pre-blocked sourceUid in Map (size now: 1)`

**Lines 455-502 - Late-arrival handling**:
- After backend API returns, check if UID already published
- Use native `client.remoteUsers` (not wrapper)
- Subscribe immediately if UID found (race condition fix)
- Update `activeTranslationsRef.current` synchronously (before setState)

**Lines 645-760 - user-published event**:
- Listen directly to Agora SDK event
- Check if UID matches `translationUid` in `activeTranslations` Map
- FIXED: Wait for backend response (don't auto-accept first UID that publishes)
- If match found:
  - Subscribe to audio (both 3000+ and 4000+)
  - Subscribe to video (only 4000+)
  - Play avatar video in source user's tile
- If no match: Ignore (privacy protection)

**Helper functions**:
```typescript
const isPalabraUid = (uid: number) => uid >= 3000 && uid < 4000;
const isAnamUid = (uid: number) => uid >= 4000 && uid < 5000;
```

**Video replacement (line 496)**:
```typescript
// Play Anam avatar video in source user's tile
user.videoTrack.play(sourceUid);  // Just UID, not "video-{uid}"
```

**Audio play fix (lines 463, 622)**:
```typescript
// Changed from Promise-based to synchronous
try {
  audioTrack.play();  // Synchronous, not Promise
} catch (err) {
  console.error('[Palabra] Failed to play audio:', err);
}
```

**agora-rn-uikit/src/Reducer/UserJoined.ts**:

**Lines 16-27 - Tile filtering for translation UIDs**:
- Filter out UIDs 3000-5999 from `activeUids`
- Prevents creating UI tiles for:
  - Palabra (3000-3999): Audio-only streams
  - Anam (4000-4999): Video plays in existing tile
  - Bot (5000+): Backend-only, invisible
- Returns unchanged state (no tile created)
- Logging: `🚫 Skipping translation/bot UID X - not adding to activeUids`
- FIXED: Changed range from `< 5000` to `< 6000` to include bot UID 5000

**config.json**:
```json
{
  "PALABRA_BACKEND_ENDPOINT": "http://localhost:8081"
}
```

## Key Architectural Decisions

### UID Ranges

| Range | Purpose | Assignment | Auto-Subscribe |
|-------|---------|------------|----------------|
| 1-2999 | Normal users | Agora | Yes |
| 3000-3999 | Palabra audio-only | Backend | No |
| 4000-4999 | Anam avatar | Backend | No |
| 5000+ | Backend bot | Backend | No |

### Monkey-Patch vs Core Edits

**Chosen**: Monkey-patch in `TranslationProvider.tsx`

**Alternatives considered**:
- ❌ Edit core RTC Engine - requires re-applying after upgrades
- ❌ Unsubscribe after subscribe - race conditions, dangerous

**Benefits**:
- All logic in one file (customization layer)
- Survives App Builder upgrades
- Easy to enable/disable
- Uses activeTranslations Map as allow-list

### useRef Pattern for State

**Problem**: Event handlers capture state at registration time (stale closure)

**Solution**: Sync ref with state on every change
```typescript
const activeTranslationsRef = useRef(activeTranslations);

useEffect(() => {
  activeTranslationsRef.current = activeTranslations;
}, [activeTranslations]);

// Event handler uses ref (always current)
const translation = activeTranslationsRef.current.get(uid);
```

### Three-UID Architecture

**Why separate UIDs for bot and Anam?**

- Bot (5000) cannot use Anam's UID (4000) → collision
- Bot is subscriber-only (doesn't publish)
- Anam joins as publisher via WebSocket init
- Client only knows about UID 4000 (Anam)

## Git Commit Strategy

### Suggested Commits

**Commit 1: Backend - Add Anam WebSocket client**
```
feat(backend): add Anam WebSocket client for avatar integration

- Add services/anam_client.go with two-step auth flow
- Implement voice, voice_end, heartbeat commands
- Handle WebSocket redirects and 500ms init delay
- All messages use snake_case with UUID event_id
```

**Commit 2: Backend - Add Agora bot for audio forwarding**
```
feat(backend): add Agora bot to forward Palabra audio to Anam

- Add services/agora_bot.go with CGO bindings
- Bot joins as UID 5000 (subscriber-only)
- Subscribes to Palabra UID 3000, forwards to Anam WebSocket
- Upsamples 16kHz → 24kHz, detects silence for voice_end
```

**Commit 3: Backend - Add ENABLE_ANAM flag**
```
feat(backend): add ENABLE_ANAM flag for avatar mode

- Modify services/palabra.go lines 300-405
- When true: Creates bot + Anam session, returns UID 4000
- When false: Original behavior, returns UID 3000
- Update Dockerfile for CGO support (linux/amd64)
```

**Commit 4: Frontend - Monkey-patch subscription**
```
feat(frontend): monkey-patch client.subscribe to filter translation UIDs

- Override client.subscribe in TranslationProvider.tsx:109-148
- Block auto-subscription for UIDs 3000-4999
- Store original subscribe for manual use
- Prevents privacy leak (users hearing others' translations)
```

**Commit 5: Frontend - Add explicit subscription logic**
```
feat(frontend): add explicit subscription for translation UIDs

- Listen to user-published event (lines 612-662)
- Check activeTranslations Map for allow-list
- Subscribe to audio (3000+ and 4000+)
- Subscribe to video (4000+ only)
- Play avatar video in source user's tile
```

**Commit 6: Frontend - Fix race condition (late-arrival)**
```
fix(frontend): handle translation UID publishing before API response

- Add late-arrival check in TranslationProvider.tsx:455-502
- Use native client.remoteUsers (not wrapper)
- Update activeTranslationsRef.current synchronously
- Subscribe immediately if UID already published
```

**Commit 7: Frontend - Fix audio play not a Promise**
```
fix(frontend): change audioTrack.play() from Promise to synchronous

- Remove .then()/.catch() - play() is synchronous
- Use try/catch for error handling (lines 463, 622)
- Fixes "Cannot read properties of undefined (reading 'then')"
```

**Commit 8: Frontend - Fix video element ID**
```
fix(frontend): play avatar video using UID directly

- Change videoTrack.play(sourceUid) from play(`video-${sourceUid}`)
- Agora SDK finds container div by UID automatically
- Fixes "video element not found" error (line 496)
```

**Commit 9: Docs - Add integration guides**
```
docs: add app-builder-dev, palabra-integrate, anam-integrate guides

- Remove ANAM_STATUS.md (superseded)
- Add concise build/deploy/debug instructions
- Document both audio-only and avatar modes
- Include troubleshooting and testing checklists
```

## Testing Before Commit

### Backend Tests

```bash
# Build succeeds
docker compose down && docker compose up -d --build

# Anam session created (if ENABLE_ANAM=true)
docker logs server | grep "Anam.*session created"

# Bot joins channel
docker logs server | grep "Bot.*connected"

# Audio forwarding works
docker logs server | grep "Forwarding audio"
```

### Frontend Tests

```bash
# Build succeeds
npm run web

# Monkey-patch applied
# Browser console: [Palabra] ✓ Overridden client.subscribe()

# Translation UID blocked from auto-subscribe
# Browser console: [Palabra] 🚫 Blocking auto-subscribe

# Explicit subscription works
# Browser console: [Palabra] ✓ Playing.*audio from UID

# Avatar video displays (if ENABLE_ANAM=true)
# Browser console: [Palabra] ✓ Anam avatar video now playing
```

### Integration Tests

- [ ] Audio-only mode (ENABLE_ANAM=false) - UID 3000 works
- [ ] Avatar mode (ENABLE_ANAM=true) - UID 4000 works
- [ ] Client auto-detects mode from UID range
- [ ] Late joiner doesn't hear/see others' translations
- [ ] Stop translation restores original audio/video
- [ ] Multiple concurrent translations work

## File Checklist

**Backend files to commit**:
- ✅ services/anam_client.go
- ✅ services/agora_bot.go
- ✅ services/palabra.go (lines 300-405)
- ✅ Dockerfile (CGO_ENABLED, platform, .so files)
- ✅ .env.example (document ANAM_* variables)

**Frontend files to commit**:
- ✅ customization/palabra/TranslationProvider.tsx
- ✅ config.json (PALABRA_BACKEND_ENDPOINT)

**Documentation files**:
- ✅ docs/app-builder-dev.md
- ✅ docs/palabra-integrate.md
- ✅ docs/anam-integrate.md
- ✅ docs/CODE_CHANGES.md (this file)

**Files to delete**:
- ✅ ANAM_STATUS.md (superseded by new docs)

## Recent Improvements (January 6, 2026)

### Voice Cloning

**File**: `services/palabra.go:224-238`

Added voice cloning options to Palabra API request:
```go
Options: map[string]interface{}{
    "speech_generation": map[string]interface{}{
        "voice_cloning": true,
        "voice_timbre_detection": map[string]interface{}{
            "enabled": true,
            "high_timbre_voices": []string{"default_high"},
            "low_timbre_voices": []string{"default_low"},
        },
    },
}
```

**Impact**: Translated audio now uses voice cloning to match the original speaker's voice characteristics.

### Auto-Detect Source Language

**File**: `TranslationMenuItem.tsx:57`

Changed from hardcoded `'en'` to `'auto'`:
```typescript
await startTranslation(uidString, 'auto', languageCode);
```

**Impact**: Palabra automatically detects source language - no need for user to specify.

### Stop Translation Audio/Video Restore Fix

**File**: `TranslationProvider.tsx:599-650`

**Problem**: When stopping translation, original audio was blocked by monkey-patch because sourceUid was still in Map.

**Fix**:
1. Remove sourceUid from Map FIRST (line 601-609)
2. Update ref synchronously (line 608)
3. Then re-subscribe to audio and video (line 619-650)

**Video fit mode fix** (line 644):
```typescript
sourceUser.videoTrack.play(sourceUid, {fit: 'contain'});
```

**subscribeToUser fix** (line 337-363):
- Use native SDK's `client.remoteUsers` (not wrapper)
- Use `originalSubscribeRef.current` to bypass monkey-patch
- Don't require `audioTrack` to exist before subscribing

**Impact**: After stopping translation, user hears original audio again and video displays correctly (fit, not fill).

### Language Selector UI - 3x3 Grid

**File**: `TranslationMenuItem.tsx:92-105, 119-163`

**Changed from**: Large scrollable modal that went off-screen

**Changed to**: Compact 3x3 grid (340px wide, 96px per cell):
- 9 languages: English, Spanish, French, German, Japanese, Chinese, Portuguese, Italian, Korean
- Flag emoji (32px) above language name
- Fixed container width with calculated spacing

**Impact**: Clean, centered dropdown that fits on all screens.

## Critical Bug Fixes (January 2025)

### Fix #1: Block sourceUid in Monkey-Patch

**Problem**: If a user being translated re-publishes (e.g., turns camera off/on), client would auto-subscribe and hear BOTH original + translation audio.

**Fix** (`TranslationProvider.tsx:145-151`):
```typescript
// CRITICAL: Also block sourceUid if it's currently being translated
const isSourceBeingTranslated = activeTranslationsRef.current.has(uidString);
if (isSourceBeingTranslated) {
  console.log('[Palabra] 🚫 Blocking auto-subscribe for sourceUid being translated:', user.uid, mediaType);
  return;
}
```

**Impact**: Prevents dual audio bug when source user re-publishes streams.

### Fix #2: Re-subscribe to Video on Stop

**Problem**: When stopping translation, only audio was re-subscribed. Video remained stopped.

**Fix** (`TranslationProvider.tsx:565-578`):
```typescript
// CRITICAL FIX: Re-subscribe to video (was missing)
if (sourceUser.hasVideo) {
  try {
    const originalSubscribe = originalSubscribeRef.current;
    await originalSubscribe(sourceUser, 'video');
    if (sourceUser.videoTrack) {
      sourceUser.videoTrack.play(sourceUid);
      console.log('[Palabra] ✓ Re-subscribed to original video for UID', sourceUid);
    }
  } catch (error) {
    console.error('[Palabra] Failed to re-subscribe to video:', error);
  }
}
```

**Impact**: Restores both audio AND video when translation is stopped.

### Fix #3: Backend Task Deduplication

**Problem**: Each user requesting same translation (e.g., French of User A) created duplicate Palabra tasks, wasting API calls and resources.

**Fix** (`palabra.go:89-106, 146-166, 440-454, 574-583`):

**Added task registry**:
```go
type TaskInfo struct {
	TaskID      string
	Streams     []PalabraStreamInfo
	SourceUID   string
	Channel     string
	Language    string
}

var activeTasksByKey = make(map[string]*TaskInfo)
```

**Check before creating**:
```go
// Check if task already exists for this (channel, sourceUid, targetLanguage)
for _, targetLang := range req.TargetLanguages {
	taskKey := fmt.Sprintf("%s:%s:%s", req.Channel, req.SourceUID, targetLang)
	if existingTask, exists := activeTasksByKey[taskKey]; exists {
		// Return existing task info instead of creating new
		respondWithJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true,
			"data": map[string]interface{}{
				"taskId":  existingTask.TaskID,
				"streams": existingTask.Streams,
			},
		})
		return
	}
}
```

**Store on create, cleanup on stop**:
```go
// After successful Palabra API call
activeTasksByKey[taskKey] = &TaskInfo{...}

// In PalabraStop
delete(activeTasksByKey, taskKey)
```

**Impact**: Multiple users can share the same translation UID, reducing API costs.

## Updated Commit Strategy

### Commit 10: Critical bug fixes - subscription logic
```
fix(frontend): block sourceUid auto-subscribe when being translated

- Prevent dual audio if source user re-publishes
- Check activeTranslations Map in monkey-patch (lines 145-151)
- Fixes issue where turning camera off/on would cause dual audio
```

### Commit 11: Critical bug fix - video re-subscription
```
fix(frontend): re-subscribe to video when stopping translation

- Previously only audio was restored, video stayed off
- Added video re-subscription in stopTranslation (lines 565-578)
- Restores full UX when translation is stopped
```

### Commit 12: Optimization - backend task deduplication
```
feat(backend): add task deduplication for shared translations

- Multiple users can now share same translation UID
- Prevents duplicate Palabra API calls for same translation
- Added activeTasksByKey registry (lines 89-106)
- Check before create (lines 146-166)
- Cleanup on stop (lines 574-583)
- Reduces API costs for multi-user scenarios
```
