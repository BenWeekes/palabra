# Palabra - Real-Time Translation for Agora Video Conferencing

Real-time speech translation with optional lip-synced avatar integration for Agora App Builder.

## Features

- **Translate Audio** - Real-time speech translation to 9+ languages
- **Avatar Mode** - Lip-synced avatar speaks the translated audio
- **Persistent Avatar** - Avatar can run independently, switching between original and translated audio

## Repository Structure

This repository contains **customization files** that overlay onto [Agora App Builder](https://github.com/AgoraIO-Community/app-builder-core):

```
palabra/
├── client/                          # Frontend customization (overlay files)
│   └── customization/
│       ├── index.tsx                # Entry point - wraps VideoCall with TranslationProvider
│       └── palabra/
│           ├── TranslationProvider.tsx   # Core translation/avatar logic
│           └── TranslationMenuItem.tsx   # Menu UI components
├── server/                          # Go backend (runs in Docker)
├── docs/                            # Documentation
└── app-builder/                     # GITIGNORED - App Builder cloned here at build time
```

**How customization works:**

1. Agora App Builder is a standalone video conferencing app
2. App Builder supports a `customization/` folder for extending functionality
3. This repo provides customization files that add Palabra translation features
4. At build time, files from `client/customization/` are copied into App Builder's `template/customization/`
5. The `index.tsx` entry point wraps the VideoCall component with `TranslationProvider`

**App Builder source:** https://github.com/AgoraIO-Community/app-builder-core

## Quick Start

### Prerequisites

- Ubuntu 20.04+ (x86-64 architecture required)
- Docker and Docker Compose
- Node.js 18+
- Nginx
- Domain name with SSL certificate

### 1. Clone Repository

```bash
cd /home/ubuntu
git clone https://github.com/BenWeekes/palabra.git
cd palabra
```

### 2. Configure Backend

```bash
cd server
cp .env.example .env
nano .env
```

**Required `.env` settings:**

```bash
# Agora Credentials
APP_ID=<your_agora_app_id>
APP_CERTIFICATE=<your_agora_certificate>
CUSTOMER_ID=<your_customer_id>
CUSTOMER_CERTIFICATE=<your_customer_certificate>

# Palabra Credentials
PALABRA_CLIENT_ID=<your_palabra_client_id>
PALABRA_CLIENT_SECRET=<your_palabra_client_secret>

# Database
POSTGRES_USER=appbuilder
POSTGRES_PASSWORD=<strong_password>
POSTGRES_DB=appbuilder

# Server Configuration
PORT=8080
SCHEME=https
ALLOWED_ORIGIN=https://yourdomain.com:7000

# Avatar Mode (set to true to enable Anam avatar)
ENABLE_ANAM=true
ANAM_API_KEY=<your_anam_api_key>
ANAM_BASE_URL=https://api.anam.ai/v1
ANAM_AVATAR_ID=<your_anam_avatar_id>
ANAM_QUALITY=high
ANAM_VIDEO_ENCODING=H264

# Session Protection
PALABRA_SESSION_TIMEOUT_MINUTES=10
PALABRA_IDLE_TIMEOUT_SECONDS=60
```

**Start backend:**

```bash
sudo docker compose up -d --build

# Verify it's running
curl http://localhost:7080/v1/palabra/tasks
# Should return: {"success":true,"tasks":[]}
```

### 3. Build Frontend

```bash
cd /home/ubuntu/palabra

# Clone Agora App Builder
git clone https://github.com/AgoraIO-Community/app-builder-core.git app-builder
cd app-builder

# Copy customization files
cp -r ../client/customization/palabra template/customization/
cp ../client/customization/index.tsx template/customization/

# Copy config files
cp config.json template/config.json
cp theme.json template/theme.json

# Update config.json with your domain
nano template/config.json
```

**Update these values in `template/config.json`:**

```json
{
  "APP_ID": "<your_agora_app_id>",
  "FRONTEND_ENDPOINT": "https://yourdomain.com:7000",
  "BACKEND_ENDPOINT": "https://yourdomain.com:7000",
  "PALABRA_BACKEND_ENDPOINT": "https://yourdomain.com:7000"
}
```

**Build:**

```bash
# Install UI Kit
npm run uikit

# Install dependencies
cd template
npm install --legacy-peer-deps
cd ..

# Build for production
npm run web-build
```

### 4. Deploy Frontend

```bash
sudo mkdir -p /var/www/palabra
sudo cp -r Builds/web/* /var/www/palabra/
sudo chown -R www-data:www-data /var/www/palabra
```

### 5. Configure Nginx

```bash
sudo nano /etc/nginx/sites-available/palabra
```

```nginx
server {
    listen 7000 ssl;
    listen [::]:7000 ssl;

    ssl_certificate /etc/letsencrypt/live/yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yourdomain.com/privkey.pem;

    # API proxy to backend
    location /v1/ {
        proxy_pass http://localhost:7080/v1/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
    }

    location /query {
        proxy_pass http://localhost:7080/query;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # Frontend static files
    location / {
        root /var/www/palabra;
        index index.html;
        try_files $uri $uri/ /index.html;
    }

    location ~* \.(js|css|png|jpg|jpeg|gif|ico|wasm|mp4|ttf)$ {
        root /var/www/palabra;
        expires 1y;
        add_header Cache-Control "public, immutable";
    }
}
```

**Enable site:**

```bash
sudo ln -s /etc/nginx/sites-available/palabra /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### 6. Get SSL Certificate

```bash
sudo certbot --nginx -d yourdomain.com
```

### 7. Verify Deployment

```bash
# Test backend API
curl https://yourdomain.com:7000/v1/palabra/tasks

# Test avatar endpoints
curl -X POST https://yourdomain.com:7000/v1/avatar/start \
  -H "Content-Type: application/json" \
  -d '{"channel":"test","sourceUid":"123"}'
```

## Usage

1. Join a video call with 2+ participants
2. Click the **3-dot menu** on a remote participant's video
3. Select **"Start Avatar"** to show lip-synced avatar
4. Select **"Translate Audio"** and choose target language
5. Avatar will speak the translated audio

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/palabra/start` | POST | Start translation for a user |
| `/v1/palabra/stop` | POST | Stop translation |
| `/v1/palabra/tasks` | GET | List active translation tasks |
| `/v1/avatar/start` | POST | Start persistent avatar |
| `/v1/avatar/stop` | POST | Stop persistent avatar |

## Operating Modes

| Mode | Config | Description |
|------|--------|-------------|
| Audio-Only | `ENABLE_ANAM=false` | Translation audio only (UID 3000) |
| Avatar | `ENABLE_ANAM=true` | Lip-synced avatar video+audio (UID 4000) |

## Updating

```bash
cd /home/ubuntu/palabra
git pull

# Rebuild backend
cd server
sudo docker compose down
sudo docker compose up -d --build

# Rebuild frontend
cd ../app-builder
cp -r ../client/customization/palabra template/customization/
cp ../client/customization/index.tsx template/customization/
npm run web-build
sudo cp -r Builds/web/* /var/www/palabra/
```

## Troubleshooting

**Backend not responding:**
```bash
sudo docker logs server --tail 50
```

**Avatar 401 Unauthorized:**
- Verify `ANAM_API_KEY` in `.env` is correct
- Recreate container after .env changes: `sudo docker compose down && sudo docker compose up -d`

**Frontend not loading customization:**
- Ensure `template/customization/index.tsx` exists
- Rebuild: `npm run web-build`

**CORS errors:**
- Verify `ALLOWED_ORIGIN` in `.env` matches your domain
- Use same-origin setup (frontend and API on same port via nginx)

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Browser       │────▶│   Nginx :7000    │────▶│  Backend :7080  │
│   (Frontend)    │     │   (SSL + Proxy)  │     │  (Docker)       │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                          │
                                                          ▼
                                                 ┌─────────────────┐
                                                 │   Palabra API   │
                                                 │   Anam API      │
                                                 │   Agora RTC     │
                                                 └─────────────────┘
```

## Documentation

- [palabra-integrate.md](docs/palabra-integrate.md) - Detailed integration guide
- [anam-integrate.md](docs/anam-integrate.md) - Anam avatar setup
- [app-builder-dev.md](docs/app-builder-dev.md) - Development guide

## License

Copyright © 2021 Agora Lab, Inc.
