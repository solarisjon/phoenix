#!/usr/bin/env bash
# scripts/deploy-remote.sh
#
# Deploy latest Phoenix to the production server.
# Usage: make deploy-remote  (or: bash scripts/deploy-remote.sh)
#
# Workflow:
#   1. Verify local tree is clean and pushed
#   2. Cross-compile a static linux/amd64 binary + build frontend
#   3. SCP binary + Containerfile.deploy to server
#   4. Build minimal Alpine container on server
#   5. Restart the phoenix-app container

set -euo pipefail

SERVER="jbowman@172.29.72.127"
IMAGE="localhost/phoenix:latest"
CONTAINER="phoenix-app"
HOST_PORT=8090

echo "=== Phoenix remote deploy ==="

# ── Pre-flight ───────────────────────────────────────────────────────────────
if ! git diff --quiet HEAD; then
    echo "✗ Uncommitted changes. Commit or stash before deploying."
    exit 1
fi

LOCAL_SHA=$(git rev-parse HEAD)
REMOTE_SHA=$(git ls-remote origin HEAD 2>/dev/null | awk '{print $1}')
if [ "$LOCAL_SHA" != "$REMOTE_SHA" ]; then
    echo "✗ Local HEAD ($LOCAL_SHA) not pushed to origin ($REMOTE_SHA). Run: git push"
    exit 1
fi
echo "✓ Commit $LOCAL_SHA is pushed"

# ── Build ────────────────────────────────────────────────────────────────────
echo "→ Building frontend..."
(cd web && npm run build --silent)
rm -rf internal/frontend/dist
cp -r web/dist internal/frontend/dist

echo "→ Cross-compiling linux/amd64 binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o phoenix-linux ./cmd/phoenix/...
echo "✓ Binary: phoenix-linux ($(du -sh phoenix-linux | cut -f1))"

# ── Upload ───────────────────────────────────────────────────────────────────
echo "→ Uploading to server..."
ssh "$SERVER" 'mkdir -p ~/Prod/phoenix/data'
scp -q phoenix-linux Containerfile.deploy "$SERVER:Prod/phoenix/"

# ── Container build + restart ────────────────────────────────────────────────
echo "→ Building container and restarting on server..."
ssh "$SERVER" bash << 'REMOTE'
set -euo pipefail
cd ~/Prod/phoenix
echo "  Building image..."
podman build -f Containerfile.deploy -t localhost/phoenix:latest .
echo "  Restarting container..."
podman stop phoenix-app 2>/dev/null || true
podman rm   phoenix-app 2>/dev/null || true
podman run -d \
    --name phoenix-app \
    --restart unless-stopped \
    -p 8090:8090 \
    -v "$HOME/Prod/phoenix/data:/data:z" \
    --env-file "$HOME/Prod/phoenix/.env" \
    localhost/phoenix:latest
sleep 3
if curl -sf http://localhost:8090/api/agents > /dev/null; then
    echo "✓ Phoenix is up at http://172.29.72.127:8090"
else
    echo "✗ Phoenix did not start"
    podman logs --tail 30 phoenix-app || true
    exit 1
fi
REMOTE

echo ""
echo "✓ Deploy complete → http://172.29.72.127:8090"
