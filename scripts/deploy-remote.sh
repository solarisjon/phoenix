#!/usr/bin/env bash
# scripts/deploy-remote.sh
#
# Deploy the latest Phoenix to the production server.
# Run from the project root:
#   make deploy-remote
#   -- or --
#   bash scripts/deploy-remote.sh
#
# Workflow:
#   1. Verifies local tree is clean and pushed to GitHub
#   2. Cross-compiles a static linux/amd64 binary
#   3. SCPs the binary + Containerfile.deploy to the server
#   4. Builds a minimal Alpine container on the server (no Go/Node needed there)
#   5. Restarts the phoenix-app container

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

echo "✓ Commit $LOCAL_SHA is pushed"

# ── Cross-compile ────────────────────────────────────────────────────────────
echo "→ Building frontend..."
cd web && npm run build --silent
cd ..
echo "→ Copying dist to embed path..."
rm -rf internal/frontend/dist
cp -r web/dist internal/frontend/dist

echo "→ Cross-compiling linux/amd64 binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o phoenix-linux ./cmd/phoenix/...
echo "✓ Built: phoenix-linux ($(du -sh phoenix-linux | cut -f1))"

# ── Upload to server ─────────────────────────────────────────────────────────
echo "→ Uploading binary and Containerfile to server..."
ssh "$SERVER" "mkdir -p $REMOTE_DIR/data"
scp -q phoenix-linux Containerfile.deploy "$SERVER:$REMOTE_DIR/"

# ── Build container + restart ─────────────────────────────────────────────────
ssh "$SERVER" bash -s <<REMOTE
set -euo pipefail

cd "$REMOTE_DIR"

echo "→ Building container image on server..."
podman build -f Containerfile.deploy -t $IMAGE .

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

echo ""
echo "✓ Deploy complete — http://172.29.72.127:$HOST_PORT"
