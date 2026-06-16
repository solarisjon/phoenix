#!/usr/bin/env bash
# scripts/server-setup.sh
#
# One-time bootstrap to set up Phoenix on the production server.
# Run this once from your local machine:
#   bash scripts/server-setup.sh   (or: make server-setup)
#
# What it does:
#   1. Creates ~/Prod/phoenix/{data} on the server
#   2. Copies env.example → .env (edit on server if you want overrides)
#   3. Enables systemd linger so the container survives logout
#   4. Then calls deploy-remote.sh to build and start Phoenix

set -euo pipefail

SERVER="jbowman@172.29.72.127"
REMOTE_DIR="\$HOME/Prod/phoenix"

echo "=== Phoenix server setup ==="
echo "Server  : $SERVER"
echo ""

# Create remote directories and env file
ssh "$SERVER" bash -s <<REMOTE
set -euo pipefail

echo "→ Creating directories..."
mkdir -p "$REMOTE_DIR/data"

echo "→ Enabling systemd linger (keeps containers alive after logout)..."
loginctl enable-linger "\$(whoami)" 2>/dev/null && echo "  Linger enabled" || echo "  Linger already enabled or not supported"
REMOTE

# Copy env template if .env doesn't exist yet
ssh "$SERVER" "test -f $REMOTE_DIR/.env" 2>/dev/null \
    && echo "→ .env already exists on server — skipping" \
    || (echo "→ Copying env template to server..." \
        && scp -q scripts/env.example "$SERVER:$REMOTE_DIR/.env" \
        && echo "  Created .env — edit $REMOTE_DIR/.env on server to customise")

echo ""
echo "→ Running initial deploy..."
bash scripts/deploy-remote.sh
