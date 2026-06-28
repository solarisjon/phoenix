#!/usr/bin/env bash
# scripts/deploy-vps.sh
#
# Deploy Phoenix to the personal VPS at 72.60.163.231 (solarisjon).
# Usage: make deploy-vps  (or: bash scripts/deploy-vps.sh)
#
# Workflow:
#   1. Verify local tree is clean and pushed
#   2. Fetch the server's CA bundle (for TLS in the container)
#   3. Cross-compile a static linux/amd64 binary + build frontend
#   4. SCP binary + Containerfile.deploy + CA bundle to server
#   5. Build a FROM-scratch container on the server
#   6. Restart phoenix-vps container

set -euo pipefail

SERVER="solarisjon@72.60.163.231"
IMAGE="localhost/phoenix:latest"
CONTAINER="phoenix-vps"
HOST_PORT=8090

echo "=== Phoenix VPS deploy ==="

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

# ── Fetch CA bundle from server ──────────────────────────────────────────────
echo "→ Fetching CA bundle from server..."
scp -q "$SERVER:scs-containers/ca-certificates.crt" ca-certificates.crt 2>/dev/null || {
    echo "  (no CA bundle found — using system certs)"
    cp /etc/ssl/cert.pem ca-certificates.crt 2>/dev/null || \
    cp /etc/ssl/certs/ca-certificates.crt ca-certificates.crt 2>/dev/null || \
    touch ca-certificates.crt
}
echo "✓ CA bundle ready"

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
scp -q phoenix-linux Containerfile.deploy ca-certificates.crt "$SERVER:Prod/phoenix/"

# ── Container build + restart ────────────────────────────────────────────────
echo "→ Building container and restarting on server..."
ssh "$SERVER" bash << 'REMOTE'
set -euo pipefail
cd ~/Prod/phoenix

# Ensure .env exists from example if not yet created.
if [ ! -f .env ]; then
    if [ -f env.vps.example ]; then
        cp env.vps.example .env
        echo "⚠  Created .env from env.vps.example — edit it to set real passwords before users log in."
    fi
fi

echo "  Building image (FROM scratch — no package installs)..."
podman build -f Containerfile.deploy -t localhost/phoenix:latest .
echo "  Restarting container..."
podman stop phoenix-vps 2>/dev/null || true
podman rm   phoenix-vps 2>/dev/null || true
podman run -d \
    --name phoenix-vps \
    --restart unless-stopped \
    -p 8090:8090 \
    -v "$HOME/Prod/phoenix/data:/data:z" \
    --env-file "$HOME/Prod/phoenix/.env" \
    localhost/phoenix:latest
sleep 3
if curl -sf http://localhost:8090/api/auth/me > /dev/null 2>&1 || curl -sf http://localhost:8090/api/agents > /dev/null 2>&1; then
    echo "✓ Phoenix is up at http://72.60.163.231:8090"
else
    echo "✗ Phoenix did not start"
    podman logs --tail 30 phoenix-vps || true
    exit 1
fi
REMOTE

# Clean up local temp files
rm -f ca-certificates.crt phoenix-linux

echo ""
echo "✓ Deploy complete → http://72.60.163.231:8090"
