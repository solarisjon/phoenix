#!/usr/bin/env bash
# scripts/server-setup-vps.sh
#
# One-time bootstrap of the personal VPS at 72.60.163.231.
# Run once before the first deploy-vps.sh.
# Usage: make server-setup-vps

set -euo pipefail

SERVER="solarisjon@72.60.163.231"

echo "=== Phoenix VPS first-time setup ==="

# Copy env example to server.
echo "→ Copying env example to server..."
scp -q scripts/env.vps.example "$SERVER:Prod/phoenix/env.vps.example" 2>/dev/null || {
    ssh "$SERVER" 'mkdir -p ~/Prod/phoenix/data'
    scp -q scripts/env.vps.example "$SERVER:Prod/phoenix/env.vps.example"
}
ssh "$SERVER" 'mkdir -p ~/Prod/phoenix/data'

echo ""
echo "✓ Server ready. Edit ~/Prod/phoenix/.env on the server with your passwords, then run: make deploy-vps"
echo ""
echo "  Next steps:"
echo "  1. ssh $SERVER"
echo "  2. cp ~/Prod/phoenix/env.vps.example ~/Prod/phoenix/.env"
echo "  3. nano ~/Prod/phoenix/.env   # set real passwords in PHOENIX_SEED_USERS"
echo "  4. exit"
echo "  5. make deploy-vps"
