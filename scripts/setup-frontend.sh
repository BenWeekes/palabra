#!/bin/bash
# Setup script for Palabra frontend
# Run from the palabra repo root: ./scripts/setup-frontend.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
APP_BUILDER_DIR="$REPO_ROOT/app-builder"

echo "=== Palabra Frontend Setup ==="
echo "Repo root: $REPO_ROOT"

# Check if App Builder already exists
if [ -d "$APP_BUILDER_DIR" ]; then
    echo "App Builder directory already exists at $APP_BUILDER_DIR"
    read -p "Update customization files only? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Copying customization files..."
        cp -r "$REPO_ROOT/client/customization/palabra/"* "$APP_BUILDER_DIR/template/customization/palabra/"
        echo "Done! Run 'cd $APP_BUILDER_DIR && npm run web-build' to rebuild."
        exit 0
    fi
    read -p "Delete and re-clone? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 1
    fi
    rm -rf "$APP_BUILDER_DIR"
fi

# Clone App Builder
echo "Cloning Agora App Builder..."
echo "Note: You need access to the App Builder repo."
echo "If you have a local copy, you can copy it to: $APP_BUILDER_DIR"
read -p "Enter App Builder git URL (or press Enter to skip clone): " APP_BUILDER_URL

if [ -n "$APP_BUILDER_URL" ]; then
    git clone "$APP_BUILDER_URL" "$APP_BUILDER_DIR"
else
    echo "Skipping clone. Please manually copy App Builder to: $APP_BUILDER_DIR"
    echo "Expected structure:"
    echo "  $APP_BUILDER_DIR/"
    echo "  ├── template/"
    echo "  │   ├── customization/"
    echo "  │   ├── src/"
    echo "  │   └── package.json"
    echo "  └── package.json"
    exit 0
fi

# Create customization directory if needed
mkdir -p "$APP_BUILDER_DIR/template/customization/palabra"

# Copy customization files
echo "Copying Palabra customization files..."
cp -r "$REPO_ROOT/client/customization/palabra/"* "$APP_BUILDER_DIR/template/customization/palabra/"
cp "$REPO_ROOT/client/config.json" "$APP_BUILDER_DIR/template/config.json"

# Install dependencies
echo "Installing dependencies..."
cd "$APP_BUILDER_DIR"
npm install

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "1. Edit config.json with your domain:"
echo "   nano $APP_BUILDER_DIR/template/config.json"
echo ""
echo "2. Build the frontend:"
echo "   cd $APP_BUILDER_DIR && npm run web-build"
echo ""
echo "3. Deploy to web server:"
echo "   sudo cp -r $APP_BUILDER_DIR/Builds/web/* /var/www/palabra/"
echo "   sudo chown -R www-data:www-data /var/www/palabra"
