#!/bin/bash
# Build script for Palabra frontend
# Syncs customization files and builds
# Run from palabra repo root: ./scripts/build-frontend.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
APP_BUILDER_DIR="$REPO_ROOT/app-builder"

echo "=== Palabra Frontend Build ==="

# Check App Builder exists
if [ ! -d "$APP_BUILDER_DIR" ]; then
    echo "Error: App Builder not found at $APP_BUILDER_DIR"
    echo "Run ./scripts/setup-frontend.sh first"
    exit 1
fi

# Sync customization files
echo "Syncing customization files..."
cp -r "$REPO_ROOT/client/customization/palabra/"* "$APP_BUILDER_DIR/template/customization/palabra/"

# Build
echo "Building frontend..."
cd "$APP_BUILDER_DIR"
npm run web-build

echo ""
echo "=== Build Complete ==="
echo "Output: $APP_BUILDER_DIR/Builds/web/"
echo ""
echo "Deploy with:"
echo "  sudo cp -r $APP_BUILDER_DIR/Builds/web/* /var/www/palabra/"
echo "  sudo chown -R www-data:www-data /var/www/palabra"
