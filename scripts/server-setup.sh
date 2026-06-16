#!/usr/bin/env bash
# scripts/server-setup.sh
#
# One-time bootstrap to set up Phoenix on the production server.
# Run this once from your local machine:
#   bash scripts/server-setup.sh
#
# What it does:
#   1. Creates ~/Prod/phoenix/ on the server
#   2. Clones the Phoenix repo
#   3. Creates the data directory for SQLite persistence
#   4. Copies env.example → .env (edit it on the server after running)
#   5. Enables systemd user linger so the container survives logout
#   6. Builds the container image and starts Phoenix on port 8090

set -euo pipefail

SERVER="jbowman@172.29.72.127"
REMOTE_DIR="\$HOME/Prod/phoenix"
REPO="https://github.com/solarisjon/phoenix.git"
IMAGE="localhost/phoenix:latest"
CONTAINER="phoenix-app"
HOST_PORT=8090
DATA_DIR="\$HOME/Prod/phoenix/data"

echo "=== Phoenix server setup ==="
echo "Server  : $SERVER"
echo "Port    : $HOST_PORT"
echo ""

ssh "$SERVER" bash -s <<REMOTE
set -euo pipefail

echo "→ Creating deploy directory..."
mkdir -p "\$HOME/Prod/phoenix/data"

if [ -d "\$HOME/Prod/phoenix/.git" ]; then
    echo "→ Repo already cloned — pulling latest..."
    cd "\$HOME/Prod/phoenix" && git pull
else
    echo "→ Cloning repo..."
    git clone $REPO "\$HOME/Prod/phoenix"
fi

echo "→ Setting up env file..."
if [ ! -f "\$HOME/Prod/phoenix/.env" ]; then
    cp "\$HOME/Prod/phoenix/scripts/env.example" "\$HOME/Prod/phoenix/.env"
    echo "  Created .env — review/edit \$HOME/Prod/phoenix/.env if needed"
else
    echo "  .env already exists — skipping"
fi

echo "→ Enabling systemd linger (allows containers to survive logout)..."
loginctl enable-linger "\$(whoami)" 2>/dev/null || true

echo "→ Building container image (this takes a few minutes)..."
cd "\$HOME/Prod/phoenix"
podman build -t $IMAGE .

echo "→ Starting Phoenix container..."
podman stop $CONTAINER 2>/dev/null || true
podman rm   $CONTAINER 2>/dev/null || true

podman run -d \
    --name $CONTAINER \
    --restart unless-stopped \
    -p $HOST_PORT:$HOST_PORT \
    -v "\$HOME/Prod/phoenix/data:/data:z" \
    --env-file "\$HOME/Prod/phoenix/.env" \
    $IMAGE

echo "→ Waiting for startup..."
sleep 3

if curl -sf http://localhost:$HOST_PORT/api/agents > /dev/null; then
    echo ""
    echo "✓ Phoenix is up!"
    echo "  URL      : http://172.29.72.127:$HOST_PORT"
    echo "  Logs     : podman logs -f $CONTAINER"
    echo "  Data dir : \$HOME/Prod/phoenix/data/"
else
    echo "✗ Phoenix did not start — check: podman logs $CONTAINER"
    exit 1
fi
REMOTE
