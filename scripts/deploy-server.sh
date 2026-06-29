#!/usr/bin/env bash
# scripts/deploy-server.sh
#
# Run this script ON the server from the project root after cloning / pulling.
#
# One-time setup:
#   git clone <repo-url> ~/phoenix
#   cd ~/phoenix
#   cp scripts/env.server.example .env
#   nano .env          # set PHOENIX_SEED_USERS passwords
#   bash scripts/deploy-server.sh
#
# Subsequent deploys:
#   cd ~/phoenix && bash scripts/deploy-server.sh
#
# Requirements on the server: git, podman

set -euo pipefail

CONTAINER="phoenix"
IMAGE="localhost/phoenix:latest"
DATA_DIR="$(pwd)/data"

echo "=== Phoenix server deploy ==="

# ── Pull latest ───────────────────────────────────────────────────────────────
echo "→ Pulling latest code..."
git pull --ff-only

# ── Ensure .env exists ────────────────────────────────────────────────────────
if [ ! -f .env ]; then
    if [ -f scripts/env.server.example ]; then
        cp scripts/env.server.example .env
        echo "⚠  Created .env from env.server.example — edit it before going live:"
        echo "   nano .env"
        echo "   (then re-run this script)"
        exit 0
    else
        echo "✗ No .env file found. Create one before deploying."
        exit 1
    fi
fi

# Read port from .env for health-check (default 8090).
PORT=$(grep -E '^PHOENIX_PORT=' .env | cut -d= -f2 | tr -d '"' || echo "8090")
PORT="${PORT:-8090}"

# ── Build container ───────────────────────────────────────────────────────────
echo "→ Building container (this takes a minute on first run)..."
podman build -f Containerfile.server -t "$IMAGE" .

# ── Restart container ─────────────────────────────────────────────────────────
echo "→ Stopping old container..."
# Stop and remove by current name and any legacy names from old deploy scripts.
for name in "$CONTAINER" phoenix-vps; do
    podman stop  "$name" 2>/dev/null || true
    podman rm -f "$name" 2>/dev/null || true
done

mkdir -p "$DATA_DIR"

echo "→ Starting phoenix..."
podman run -d \
    --name "$CONTAINER" \
    --restart unless-stopped \
    -p "${PORT}:${PORT}" \
    -v "${DATA_DIR}:/data:z" \
    --env-file "$(pwd)/.env" \
    "$IMAGE"

# ── Health check ──────────────────────────────────────────────────────────────
# Any HTTP response (including 401 when auth is enabled) means the server is up.
echo "→ Waiting for phoenix to start..."
for i in $(seq 1 15); do
    CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:${PORT}/api/auth/me" 2>/dev/null || echo "000")
    if [ "$CODE" != "000" ]; then
        echo ""
        echo "✓ Phoenix is up on port ${PORT}"
        exit 0
    fi
    sleep 1
done

echo "✗ Phoenix did not start within 15 seconds"
podman logs --tail 40 "$CONTAINER" || true
exit 1
