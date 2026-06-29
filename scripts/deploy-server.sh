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
podman stop  "$CONTAINER" 2>/dev/null || true
podman rm -f "$CONTAINER" 2>/dev/null || true

# Kill any non-podman process still holding the port.
if ss -tlnp "sport = :${PORT}" 2>/dev/null | grep -q ":${PORT}"; then
    echo "→ Port ${PORT} still bound — killing occupying process..."
    fuser -k "${PORT}/tcp" 2>/dev/null || true
    sleep 1
fi

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
echo "→ Waiting for phoenix to start..."
for i in $(seq 1 10); do
    if curl -sf "http://localhost:${PORT}/api/auth/me" > /dev/null 2>&1 || \
       curl -sf "http://localhost:${PORT}/api/agents"   > /dev/null 2>&1; then
        echo ""
        echo "✓ Phoenix is up on port ${PORT}"
        exit 0
    fi
    sleep 1
done

echo "✗ Phoenix did not start within 10 seconds"
podman logs --tail 40 "$CONTAINER" || true
exit 1
