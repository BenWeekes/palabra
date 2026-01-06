# Palabra + Anam Integration for Agora App Builder

Real-time speech translation with optional lip-synced avatar for Agora video conferencing.

## Quick Start (Development)

### Backend
```bash
cd server
docker compose up --build
# Runs at http://localhost:8080
```

### Frontend
```bash
cd client
npm install
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

**Update config.json:**
```json
{
  "PALABRA_BACKEND_ENDPOINT": "https://yourdomain.com/api"
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

**Nginx configuration:**
```nginx
# Redirect HTTP to HTTPS
server {
    listen 80;
    server_name yourdomain.com;
    return 301 https://$server_name$request_uri;
}

# HTTPS server
server {
    listen 443 ssl http2;
    server_name yourdomain.com;

    # SSL certificates (Let's Encrypt - configured in step 5)
    ssl_certificate /etc/letsencrypt/live/yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yourdomain.com/privkey.pem;

    # SSL settings
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;

    # Frontend - serve React build
    location / {
        root /var/www/palabra;
        try_files $uri $uri/ /index.html;

        # Security headers
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-XSS-Protection "1; mode=block" always;
    }

    # Backend API - proxy to Docker
    location /api/ {
        proxy_pass http://localhost:8080/;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;

        # Timeout settings for long-running requests
        proxy_read_timeout 300s;
        proxy_connect_timeout 75s;
    }

    # Client logs size limit
    client_max_body_size 10M;
}
```

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
# Check backend is running
curl http://localhost:8080/health  # Should return 200 OK

# Check frontend files
ls -la /var/www/palabra

# Check Nginx is serving
curl -I http://yourdomain.com  # Should redirect to HTTPS

# Check SSL
curl -I https://yourdomain.com  # Should return 200 OK

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
