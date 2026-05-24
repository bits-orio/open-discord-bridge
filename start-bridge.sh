#!/usr/bin/env bash
# Run ONLY the bridge (scenario 2). Factorio runs elsewhere — either the same box with a
# shared events file (transport: local), or a remote host (transport: sftp). The transport
# and RCON address are set in bridge/bridge.yaml (or env-var config mode if it's absent).
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
BRIDGE="$REPO/bridge/odb-bridge"
CONFIG="${BRIDGE_CONFIG:-$REPO/bridge/bridge.yaml}"

# Rebuild if sources changed (no-op without Go; see build-bridge.sh).
"$REPO/build-bridge.sh"

ENV_FILE="$REPO/bridge/.env"
if [[ -f "$ENV_FILE" ]]; then
    set -a; . "$ENV_FILE"; set +a
fi

echo "Starting bridge (config: $CONFIG — falls back to env-var config mode if absent) ..."
exec "$BRIDGE" -config "$CONFIG"
