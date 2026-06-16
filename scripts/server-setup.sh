#!/usr/bin/env bash
# scripts/server-setup.sh
#
# One-time bootstrap to set up Phoenix on the production server.
# Run once from the project root:
#   bash scripts/server-setup.sh   (or: make server-setup)

set -euo pipefail

SERVER="jbowman@172.29.72.127"

echo "=== Phoenix server setup ==="
echo "Server  : $SERVER"
echo ""

echo "→ Creating directories on server..."
ssh "$SERVER" 'mkdir -p ~/Prod/phoenix/data && echo "  dirs ok"'

echo "→ Enabling systemd linger..."
ssh "$SERVER" 'loginctl enable-linger "$(whoami)" 2>/dev/null && echo "  linger enabled" || echo "  linger already on"'

echo "→ Copying env template to server..."
HAS_ENV=$(ssh "$SERVER" 'test -f ~/Prod/phoenix/.env && echo yes || echo no')
if [ "$HAS_ENV" = "no" ]; then
    scp -q scripts/env.example "$SERVER:Prod/phoenix/.env"
    echo "  Created ~/Prod/phoenix/.env — edit to customise if needed"
else
    echo "  .env already exists — skipping"
fi

echo ""
echo "→ Running initial deploy..."
bash scripts/deploy-remote.sh
