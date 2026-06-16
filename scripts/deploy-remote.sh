#!/usr/bin/env bash
# scripts/deploy-remote.sh
#
# Deploy the latest Phoenix to the production server.
# Run from the project root:
#   make deploy-remote
#   -- or --
#   bash scripts/deploy-remote.sh
#
# What it does:
#   1. Verifies local tree is clean (no uncommitted changes)
#   2. Verifies latest commit is pushed to GitHub
#   3. SSHes to the server: git pull → podman build → restart container

set -euo pipefail

SERVER="jbowman@172.29.72.127"
REMOTE_DIR="\$HOME/Prod/phoenix"
IMAGE="localhost/phoenix:latest"
CONTAINER="phoenix-app"
HOST_PORT=8090

# ── Pre-flight checks ────────────────────────────────────────────────────────
echo "=== Phoenix remote deploy ==="

if ! git diff --quiet HEAD; then
    echo "✗ Uncommitted changes detected. Commit or stash before deploying."
    exit 1
fi

LOCAL_SHA=$(git rev-parse HEAD)
REMOTE_SHA=$(git ls-remote origin HEAD 2>/dev/null | awk '{print $1}')
if [ "$LOCAL_SHA" != "$REMOTE_SHA" ]; then
    echo "✗ Local HEAD ($LOCAL_SHA) is not pushed to origin ($REMOTE_SHA)."
    echo "  Run: git push"
    exit 1
fi

echo "✓ Local HEAD $LOCAL_SHA is pushed"
echo "→ Deploying to $SERVER..."

# ── Remote build + restart ───────────────────────────────────────────────────
ssh "$SERVER" bash -s <<REMOTE
set -euo pipefail

cd "$REMOTE_DIR"

echo "→ Pulling latest..."
git pull

echo "→ Building container image..."
podman build -t $IMAGE .

echo "→ Restarting container..."
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
    echo "✓ Phoenix is up at http://172.29.72.127:$HOST_PORT"
else
    echo "✗ Phoenix did not start — check: podman logs $CONTAINER"
    podman logs --tail 30 $CONTAINER || true
    exit 1
fi
REMOTE
