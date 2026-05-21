#!/usr/bin/env bash
# Open Discord Bridge — start the headless Factorio test server.
#
# Uses the binary at ~/factorio/bin, the isolated mods dir from install.sh, and a
# dedicated test save. RCON port/password are shared with the bridge via bridge/.env.
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
RUN_DIR="$REPO/.run"
RUN_MODS="$RUN_DIR/mods"

FACTORIO_BIN="$HOME/factorio/bin/x64/factorio"
SAVE="${ODB_SAVE:-$RUN_DIR/odb-test.zip}"
SETTINGS="$RUN_DIR/server-settings.json"

# Shared secrets / ports.
ENV_FILE="$REPO/bridge/.env"
if [[ -f "$ENV_FILE" ]]; then
    set -a; . "$ENV_FILE"; set +a
fi
RCON_PORT="${RCON_PORT:-27015}"
RCON_PASSWORD="${FACTORIO_RCON_PASSWORD:-odb-rcon-dev}"

# Preflight.
if [[ ! -x "$FACTORIO_BIN" ]]; then
    echo "ERROR: Factorio binary not found at $FACTORIO_BIN" >&2
    exit 1
fi
if [[ ! -d "$RUN_MODS" ]]; then
    echo "ERROR: $RUN_MODS missing — run ./install.sh first." >&2
    exit 1
fi

# Create the test map on first run (with the isolated mod set).
if [[ ! -f "$SAVE" ]]; then
    echo "Creating test save: $SAVE"
    "$FACTORIO_BIN" --create "$SAVE" --mod-directory "$RUN_MODS"
fi

echo "Starting Factorio server (RCON 127.0.0.1:$RCON_PORT) ..."
exec "$FACTORIO_BIN" \
    --start-server "$SAVE" \
    --mod-directory "$RUN_MODS" \
    --server-settings "$SETTINGS" \
    --rcon-port "$RCON_PORT" \
    --rcon-password "$RCON_PASSWORD"
