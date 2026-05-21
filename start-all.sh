#!/usr/bin/env bash
# Open Discord Bridge — start Factorio and the bridge together.
#
# Launches the headless server, waits for RCON to come up, then runs the bridge in the
# foreground. Ctrl-C (or the bridge exiting) tears the server down too.
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"

ENV_FILE="$REPO/bridge/.env"
if [[ -f "$ENV_FILE" ]]; then
    set -a; . "$ENV_FILE"; set +a
fi
RCON_PORT="${RCON_PORT:-27015}"

BRIDGE="$REPO/bridge/odb-bridge"
if [[ ! -x "$BRIDGE" ]]; then
    echo "ERROR: $BRIDGE missing — run ./install.sh first." >&2
    exit 1
fi

# Start the server in the background.
"$REPO/start-server.sh" &
SERVER_PID=$!

cleanup() {
    echo
    echo "Stopping Factorio server (pid $SERVER_PID) ..."
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait for RCON to accept connections (max ~60s).
echo "Waiting for RCON on 127.0.0.1:$RCON_PORT ..."
for _ in $(seq 1 60); do
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo "ERROR: server exited before RCON came up." >&2
        exit 1
    fi
    if (exec 3<>"/dev/tcp/127.0.0.1/$RCON_PORT") 2>/dev/null; then
        exec 3>&- 3<&-
        echo "RCON is up."
        break
    fi
    sleep 1
done

# Run the bridge in the foreground; when it exits, the trap stops the server.
echo "Starting bridge ..."
( cd "$REPO/bridge" && ./odb-bridge -config bridge.yaml )
