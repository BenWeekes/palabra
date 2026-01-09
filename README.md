# Palabra + Anam Integration for Agora App Builder

Real-time speech translation with optional lip-synced avatar for Agora video conferencing.

## Repository Structure

This repo contains **customization files** that overlay onto Agora App Builder:

```
palabra/                      # This repo (customizations + backend)
├── client/                   # Frontend customization files (NOT buildable standalone)
│   ├── customization/palabra/  # Translation UI components
│   └── config.json            # Frontend config
├── server/                   # Go backend (buildable with Docker)
└── docs/                     # Documentation

palabra-client-full/          # Separate: Full Agora App Builder (has package.json)
├── template/                 # App Builder source
│   ├── customization/palabra/  # ← Copy from palabra/client/customization/
│   └── ...
└── Builds/web/              # Build output → deploy to /var/www/palabra/
```

## Quick Start (Development)

### Backend
```bash
cd server

# Copy and configure environment
cp .env.example .env
# Edit .env with your Agora/Palabra/Anam credentials

# Build and run (builds both server and bot_worker binaries)
docker compose up --build
# Runs at http://localhost:7081 (external) → 8080 (container internal)
```

The Docker build creates two binaries:
- `server` - Main HTTP server (parent process)
- `bot_worker` - Agora SDK runner (child process, crash-isolated)

### Frontend

The `client/` directory contains **customization files only**. To build the frontend:

```bash
# 1. Copy customization files to App Builder
cp -r palabra/client/customization/palabra/* palabra-client-full/template/customization/palabra/

# 2. Build from App Builder directory
cd palabra-client-full/template
npm install      # First time only
npm run web:build

# 3. Deploy (production)
sudo cp -r ../Builds/web/* /var/www/palabra/
sudo chown -R www-data:www-data /var/www/palabra/
```

For development with hot reload:
```bash
cd palabra-client-full/template
npm run web
# Runs at http://localhost:9000
```

## Production Deployment (Ubuntu Linux)

### Prerequisites

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install Docker
sudo apt install -y docker.io docker-compose

# Install Node.js 18+ and npm
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt install -y nodejs

# Install Nginx
sudo apt install -y nginx

# Install Certbot (for SSL)
sudo apt install -y certbot python3-certbot-nginx
```

### 1. Clone Repository

```bash
cd /opt
sudo git clone https://github.com/BenWeekes/palabra.git
cd palabra
```

### 2. Setup Backend

```bash
cd server

# Create .env file with your credentials
sudo nano .env
```

**Required .env configuration:**
```bash
# Agora credentials
APP_ID=your_agora_app_id
APP_CERTIFICATE=your_agora_certificate
CUSTOMER_ID=your_customer_id
CUSTOMER_CERTIFICATE=your_customer_certificate

# Palabra credentials
PALABRA_CLIENT_ID=your_palabra_client_id
PALABRA_CLIENT_SECRET=your_palabra_client_secret

# Server configuration
PORT=8080
SCHEME=https
ALLOWED_ORIGIN=https://yourdomain.com

# Database
POSTGRES_USER=appbuilder
POSTGRES_PASSWORD=strong_password_here
POSTGRES_DB=appbuilder
DATABASE_URL=postgresql://appbuilder:strong_password_here@postgres:5432/appbuilder?sslmode=disable

# Translation mode
ENABLE_ANAM=false  # Set true for avatar mode

# Session protection (prevents runaway credit usage)
PALABRA_SESSION_TIMEOUT_MINUTES=10  # Max session duration
PALABRA_IDLE_TIMEOUT_SECONDS=60     # Stop after silence

# Anam credentials (only if ENABLE_ANAM=true)
ANAM_API_KEY=your_base64_key
ANAM_BASE_URL=https://api.anam.ai/v1
ANAM_AVATAR_ID=your_avatar_uuid
ANAM_QUALITY=high
ANAM_VIDEO_ENCODING=H264
```

**Start backend:**
```bash
sudo docker compose up -d --build

# Check logs
sudo docker compose logs -f
```

### 3. Setup Frontend

```bash
cd /opt/palabra/client

# Update backend endpoint
sudo nano customization/config.json
```

**Update config.json (same-origin setup):**
```json
{
  "FRONTEND_ENDPOINT": "https://yourdomain.com:7000",
  "BACKEND_ENDPOINT": "https://yourdomain.com:7000",
  "PALABRA_BACKEND_ENDPOINT": "https://yourdomain.com:7000"
}
```

**Build production frontend:**
```bash
npm install
npm run build

# Copy build to web server directory
sudo mkdir -p /var/www/palabra
sudo cp -r dist/* /var/www/palabra/
sudo chown -R www-data:www-data /var/www/palabra
```

### 4. Configure Nginx

```bash
sudo nano /etc/nginx/sites-available/palabra
```

**Nginx configuration (Same-Origin Setup - Recommended):**

This configuration serves both frontend and API from the same origin (port 7000), eliminating CORS issues:

```nginx
# Palabra Frontend + Backend API - Port 7000 (same-origin)
server {
    listen [::]:7000 ssl ipv6only=on;
    listen 7000 ssl;
    ssl_certificate /etc/letsencrypt/live/yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yourdomain.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    # API requests - proxy to backend (same-origin, no CORS)
    location /v1/ {
        proxy_pass http://localhost:7081/v1/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_connect_timeout 75s;
    }

    # Static files - serve frontend build
    location / {
        root /var/www/palabra;
        index index.html;
        try_files $uri $uri/ /index.html;
    }

    # Cache static assets
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|wasm|mp4|ttf)$ {
        root /var/www/palabra;
        expires 1y;
        add_header Cache-Control "public, immutable";
    }

    client_max_body_size 10M;
}

# Backend direct access - Port 7080 (optional, for debugging)
server {
    listen [::]:7080 ssl ipv6only=on;
    listen 7080 ssl;
    ssl_certificate /etc/letsencrypt/live/yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yourdomain.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    location / {
        proxy_pass http://localhost:7081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_read_timeout 300s;
        proxy_connect_timeout 75s;
    }

    client_max_body_size 10M;
}
```

**Key Points:**
- Port 7000: Frontend + API (same-origin, no CORS)
- Port 7080: Direct backend access (for debugging/testing)
- Port 7081: Backend Docker container (internal)
- All API requests use `/v1/` prefix and are proxied to backend

**Enable site:**
```bash
sudo ln -s /etc/nginx/sites-available/palabra /etc/nginx/sites-enabled/
sudo nginx -t  # Test configuration
sudo systemctl restart nginx
```

### 5. Setup SSL (Let's Encrypt)

```bash
# Get SSL certificate (interactive - follow prompts)
sudo certbot --nginx -d yourdomain.com

# Verify auto-renewal is enabled
sudo systemctl status certbot.timer

# Test renewal (dry run)
sudo certbot renew --dry-run
```

### 6. Firewall Configuration

```bash
# Allow HTTP, HTTPS, and SSH
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

### 7. Start on Boot

```bash
# Docker containers already restart automatically (check docker-compose.yml)
# Nginx starts on boot by default

# Verify services
sudo systemctl enable nginx
sudo systemctl enable docker

# Check status
sudo systemctl status nginx
sudo systemctl status docker
```

### 8. Verify Deployment

```bash
# Check backend is running (internal port)
curl http://localhost:7081/health  # Should return 200 OK

# Check frontend files
ls -la /var/www/palabra

# Check combined frontend + API (same-origin)
curl -k https://localhost:7000/           # Frontend - should return HTML
curl -k https://localhost:7000/v1/health  # API via proxy - should return OK

# Check direct backend access (optional debug port)
curl -k https://localhost:7080/health     # Should return OK

# Monitor logs
sudo docker compose logs -f  # Backend logs
sudo tail -f /var/log/nginx/access.log  # Nginx access logs
sudo tail -f /var/log/nginx/error.log   # Nginx error logs
```

## Updating Production

```bash
cd /opt/palabra

# Pull latest changes
sudo git pull

# Update backend
cd server
sudo docker compose down
sudo docker compose up -d --build

# Update frontend
cd ../client
npm install
npm run build
sudo cp -r dist/* /var/www/palabra/

# Reload Nginx (zero downtime)
sudo nginx -s reload
```

## System Requirements

**Minimum:**
- Ubuntu 20.04+ (x86-64 architecture)
- 2 CPU cores
- 4GB RAM
- 20GB storage
- Public IP with domain name

**Recommended:**
- Ubuntu 22.04 LTS (x86-64)
- 4 CPU cores
- 8GB RAM
- 50GB SSD storage
- CDN for frontend (optional)

**Important**: Server must be x86-64 architecture (not ARM) due to Agora SDK binary requirements.

### Agora SDK Requirements (Avatar Mode)

For avatar mode (`ENABLE_ANAM=true`), the backend requires Agora RTC Server SDK native libraries:

**What's included in the repository:**
- `server/vendor_sdk/` - Go SDK wrapper (253MB)
  - `go_sdk/rtc/` - Go bindings for Agora RTC
  - `agora_sdk/` - Native SDK v4.4.32 (headers + libraries)
  - Used by `go.mod` replace directive

- `server/agora_sdk/` - Native SDK with macOS binaries (243MB)
  - Headers: `include/c/api2/` and `include/c/base/`
  - Libraries: `libagora_rtc_sdk.so` (Linux) and `.dylib` (macOS)
  - Platform: Linux x86-64 for production, macOS for local dev

**Why two SDK directories?**
- `vendor_sdk` = Go SDK module (imports native SDK internally)
- `agora_sdk` = Native SDK used by CGO during build and at runtime

**Build requirements:**
- Docker with CGO enabled (configured in Dockerfile)
- Build platform must be `linux/amd64`

**The Dockerfile handles:**
1. Copy `vendor_sdk/` for Go module resolution
2. Copy `agora_sdk/` for CGO compilation (headers) and runtime (libraries)
3. Set CGO flags to find headers and link libraries
4. Copy `.so` files to runtime container
5. Set `LD_LIBRARY_PATH` for runtime

**No manual SDK installation needed** - everything is included in the repo and handled by Docker.

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

📖 **[palabra-architecture.md](docs/palabra-architecture.md)** - Backend process architecture (crash isolation)

## Architecture

### Backend Process Model

The backend uses a **parent/child process architecture** for crash isolation:

```
┌─────────────────────────────────────────────────────────────┐
│                    PARENT PROCESS (HTTP Server)              │
│  - Handles API requests (/v1/palabra/start, /stop)          │
│  - Spawns isolated child processes for each session         │
│  - Survives child crashes (no 502 errors)                   │
│                                                              │
│  Built binary: /go/bin/server                               │
└─────────────────────────────────────────────────────────────┘
         │ stdin/stdout (FlatBuffers IPC)
         ▼
┌─────────────────────────────────────────────────────────────┐
│                    CHILD PROCESS (bot_worker)                │
│  - Runs Agora SDK (can crash with segfaults)                │
│  - Connects to Anam WebSocket                               │
│  - Forwards audio from Palabra to Anam                      │
│  - Auto-stops on: timeout, idle, or target-left             │
│                                                              │
│  Built binary: /go/bin/bot_worker                           │
└─────────────────────────────────────────────────────────────┘
```

**Why this design?** The Agora Go SDK wraps native C code that can crash with segfaults. Go's `recover()` cannot catch these. By isolating the SDK in a child process, crashes only affect that session - the HTTP server stays up.

See [palabra-architecture.md](docs/palabra-architecture.md) for full details.

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

✅ **Session protection** - Three-layer safeguard against runaway sessions:
  - Session timeout (default 10 min) - max duration
  - Idle detection (default 60s) - stops if no audio activity
  - Target-left detection - stops immediately if Palabra bot leaves channel

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
  "FRONTEND_ENDPOINT": "https://yourdomain.com:7000",
  "BACKEND_ENDPOINT": "https://yourdomain.com:7000",
  "PALABRA_BACKEND_ENDPOINT": "https://yourdomain.com:7000"
}
```

**Same-Origin API Routing**: In production, all endpoints use the same origin (port 7000). Nginx proxies `/v1/*` requests to the backend (port 7081), eliminating CORS issues.

**Port Note**: Development backend runs on port **7080** (not 8080/8081) to avoid conflicts with other services. For production with Nginx, use port 7000 for everything.

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
