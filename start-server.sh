#!/usr/bin/env bash
# Open Discord Bridge — start the headless Factorio test server.
#
# Runs the binary at ~/factorio/bin against your real ~/factorio data dir: the full
# mods dir (so MTS + the companion mod load) and saves under ~/factorio/saves.
# RCON port/password are shared with the bridge via bridge/.env.
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
FACTORIO_DATA="$HOME/factorio"

FACTORIO_BIN="$FACTORIO_DATA/bin/x64/factorio"
SAVE="${ODB_SAVE:-$FACTORIO_DATA/saves/odb-test.zip}"
SETTINGS="$FACTORIO_DATA/server-settings.json"

# Mods dir to launch with. Defaults to your real ~/factorio/mods (full pack, so MTS and
# the companion mod both load). Override with ODB_MOD_DIR to point elsewhere.
MODS="${ODB_MOD_DIR:-$FACTORIO_DATA/mods}"

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
if [[ ! -d "$MODS" ]]; then
    echo "ERROR: mods dir not found: $MODS" >&2
    exit 1
fi
if [[ ! -f "$SETTINGS" ]]; then
    echo "ERROR: $SETTINGS missing — run ./install.sh first." >&2
    exit 1
fi

# Create the test map on first run.
if [[ ! -f "$SAVE" ]]; then
    echo "Creating test save: $SAVE"
    mkdir -p "$(dirname "$SAVE")"
    "$FACTORIO_BIN" --create "$SAVE" --mod-directory "$MODS"
fi

echo "Starting Factorio server (RCON 127.0.0.1:$RCON_PORT) ..."
exec "$FACTORIO_BIN" \
    --start-server "$SAVE" \
    --mod-directory "$MODS" \
    --server-settings "$SETTINGS" \
    --rcon-port "$RCON_PORT" \
    --rcon-password "$RCON_PASSWORD"
