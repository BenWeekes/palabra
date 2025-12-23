# Palabra Real-Time Translation Integration for Agora App Builder

This repository contains the integration code for adding Palabra's real-time speech translation to Agora App Builder.

## Repository Structure

```
palabra-repo/
├── server/              # Backend (Go) integration files
│   ├── services/       # Palabra API handlers and auth
│   ├── cmd/            # Server entry point
│   ├── utils/          # Token generation utilities
│   ├── .env            # Environment configuration
│   └── docker-compose.yml
│
├── client/              # Frontend (React/TypeScript) integration files
│   ├── customization/  # App Builder customization layer
│   │   ├── index.tsx   # Main customization entry point
│   │   └── palabra/    # Translation UI components
│   ├── src/
│   │   ├── subComponents/caption/  # Palabra API hooks
│   │   └── pages/      # Modified VideoCall component
│   └── config.json     # Frontend configuration
│
├── palabra-integrate.md  # Detailed integration guide
├── CLAUDE.md            # Project notes and troubleshooting
└── README.md            # This file
```

## Key Files

### Backend
- **services/palabra.go** - Main Palabra API integration with task registry and deduplication
- **services/auth.go** - Stub authentication endpoints for local development
- **cmd/video_conferencing/server.go** - Route registration
- **.env** - Agora and Palabra credentials (Docker configuration)

### Frontend
- **customization/index.tsx** - Wraps VideoCall with TranslationProvider
- **customization/palabra/TranslationProvider.tsx** - Translation state management and RTC subscription logic
- **customization/palabra/TranslationMenuItem.tsx** - "Translate Audio" menu UI
- **src/subComponents/caption/usePalabraAPI.tsx** - Palabra API client hook
- **config.json** - Agora credentials and backend endpoint

## Features

- **Real-time speech translation** - Live translation of audio streams during video calls
- **Task deduplication** - Multiple users can subscribe to the same translation without creating duplicate Palabra tasks
- **Late joiner discovery** - Users joining a channel can discover existing translation tasks
- **Selective subscription** - Each user independently chooses which translations to hear
- **UID range management** - Reserved UIDs (3000-3099) for Palabra translation streams

## Quick Start

See `palabra-integrate.md` for detailed integration instructions and `CLAUDE.md` for troubleshooting.

## Documentation

- **palabra-integrate.md** - Complete integration guide with architecture details
- **CLAUDE.md** - Development notes, bug fixes, and troubleshooting timeline

## License

Copyright © 2021 Agora Lab, Inc. See individual files for license details.
