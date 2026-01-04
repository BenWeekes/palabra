# Palabra + Anam Integration for Agora App Builder

Real-time speech translation with optional lip-synced avatar for Agora video conferencing.

## Quick Start

### Backend
```bash
cd app-builder-backend
docker compose up --build
# Runs at http://localhost:8081
```

### Frontend
```bash
cd app-builder-core/template
npm install
npm run web
# Runs at http://localhost:9000
```

## Two Operating Modes

### Audio-Only Mode (ENABLE_ANAM=false)
- Palabra translates speech → Client receives audio stream (UID 3000)
- User sees original video, hears translated audio
- Cost-effective translation

### Avatar Mode (ENABLE_ANAM=true)
- Palabra translates → Bot forwards to Anam → Lip-synced avatar (UID 4000)
- User sees/hears French-speaking avatar in original user's tile
- Premium experience with video

**Switch modes**: Change `ENABLE_ANAM` in backend `.env` and restart

## Documentation

📖 **[app-builder-dev.md](docs/app-builder-dev.md)** - Development setup, build, deploy, debug

📖 **[palabra-integrate.md](docs/palabra-integrate.md)** - Palabra integration (both modes)

📖 **[anam-integrate.md](docs/anam-integrate.md)** - Anam avatar integration (WebSocket, bot)

📖 **[CODE_CHANGES.md](docs/CODE_CHANGES.md)** - Git commit reference, file changes

## Architecture

### UID Ranges

| Range | Purpose | Auto-Subscribe |
|-------|---------|----------------|
| 1-2999 | Normal users | ✅ Yes |
| 3000-3999 | Palabra audio-only | ❌ No |
| 4000-4999 | Anam avatar | ❌ No |
| 5000+ | Backend bot | ❌ No |

### Key Features

✅ **Monkey-patch subscription** - Blocks auto-subscribe for UIDs 3000-4999 (privacy)

✅ **Mode auto-detection** - Client detects from UID range (no config needed)

✅ **Video replacement** - Avatar plays in source user's tile (seamless UX)

✅ **Late-arrival handling** - Handles race conditions (UID publishes before API response)

## Configuration

### Backend (.env)
```bash
# Mode selection
ENABLE_ANAM=false    # Set true for avatar mode

# Palabra credentials (required)
PALABRA_CLIENT_ID=your_id
PALABRA_CLIENT_SECRET=your_secret

# Anam credentials (only if ENABLE_ANAM=true)
ANAM_API_KEY=base64_key
ANAM_AVATAR_ID=uuid

# Agora credentials
APP_ID=your_app_id
APP_CERTIFICATE=your_certificate
```

### Frontend (config.json)
```json
{
  "PALABRA_BACKEND_ENDPOINT": "http://localhost:8081"
}
```

## Testing

### Expected Logs (Audio-Only)
```
[Palabra] ✓ Playing translation audio from UID 3000
```

### Expected Logs (Avatar)
```
[Palabra] ✓ Playing Anam avatar audio from UID 4000
[Palabra] ✓ Anam avatar video now playing in tile for UID 100
[Anam] Connected to Anam WebSocket
[AgoraBot] Connected to channel as UID 5000
```

## File Structure

```
app-builder-backend/
├── services/
│   ├── palabra.go          # Translation integration + mode switch
│   ├── anam_client.go      # WebSocket client (avatar mode)
│   └── agora_bot.go        # Audio forwarder (avatar mode)
└── .env                    # Configuration (ENABLE_ANAM flag)

app-builder-core/template/
└── customization/palabra/
    └── TranslationProvider.tsx   # Subscription logic, video replacement
```

## Key Code Changes

**Backend** (services/palabra.go:300-405):
- When `ENABLE_ANAM=true`: Creates bot (UID 5000) + Anam session, returns UID 4000
- When `ENABLE_ANAM=false`: Returns UID 3000 (Palabra audio-only)

**Frontend** (TranslationProvider.tsx):
- Lines 109-148: Monkey-patch `client.subscribe()` to filter UIDs 3000-4999
- Lines 612-662: Explicit subscription on `user-published` event
- Lines 455-502: Late-arrival handling for race conditions

## Troubleshooting

### No Translation Audio
- Check backend logs: `docker logs server -f | grep Palabra`
- Verify `PALABRA_BACKEND_ENDPOINT` in config.json
- Check browser console for subscription errors

### No Avatar Video (ENABLE_ANAM=true)
- Check backend logs: `docker logs server -f | grep Anam`
- Verify Anam credentials in .env
- Check bot joined: `docker logs server | grep "Bot.*connected"`

### Token Errors (Error Code 5)
- Ensure APP_ID matches in backend .env and frontend config.json
- Restart both backend and frontend after .env changes

## References

- **Agora SDK**: https://docs.agora.io/en/video-calling/
- **App Builder**: https://appbuilder-docs.agora.io/
- **Palabra API**: Contact Palabra team for documentation
- **Anam API**: Contact Anam team for documentation

## License

Copyright © 2021 Agora Lab, Inc. See individual files for license details.
