#!/usr/bin/env bash
# Sidecar launcher (scenario 3): start the bridge in the background, then run Factorio in
# the foreground; stop the bridge when Factorio exits. The bridge tolerates Factorio not
# being ready yet (RCON reconnects; the events file appears later).
#
# Usage:  run-sidecar.sh <factorio launch command...>
#
# As a single-container entrypoint / Pterodactyl custom startup, pass your image's normal
# Factorio command, e.g.:
#   run-sidecar.sh /opt/factorio/bin/x64/factorio --start-server /data/save.zip \
#       --rcon-port 27015 --rcon-password "$FACTORIO_RCON_PASSWORD"
#
# Requires the bridge binary present (BRIDGE_BIN) and configured (BRIDGE_CONFIG file, or
# env-var config mode if the file is absent — handy for panels). Bridge logs go to this
# process's stdout, interleaved with Factorio; redirect the bridge below if you want a
# clean Factorio console.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"

if [[ $# -eq 0 ]]; then
    echo "usage: $0 <factorio launch command...>" >&2
    exit 2
fi

BRIDGE_BIN="${BRIDGE_BIN:-$HERE/bridge/odb-bridge}"
BRIDGE_CONFIG="${BRIDGE_CONFIG:-$HERE/bridge/bridge.yaml}"

if [[ ! -x "$BRIDGE_BIN" ]]; then
    echo "ERROR: bridge binary not found at $BRIDGE_BIN" >&2
    exit 1
fi

# Start the bridge in the background (config file, else env-var config mode).
if [[ -f "$BRIDGE_CONFIG" ]]; then
    "$BRIDGE_BIN" -config "$BRIDGE_CONFIG" &
else
    "$BRIDGE_BIN" &
fi
BRIDGE_PID=$!
echo "sidecar: bridge started (pid $BRIDGE_PID)"

stop_bridge() {
    kill "$BRIDGE_PID" 2>/dev/null || true
    wait "$BRIDGE_PID" 2>/dev/null || true
}
trap stop_bridge EXIT

# Factorio in the foreground; forward stop signals so it saves and exits cleanly.
"$@" &
FACTORIO_PID=$!
trap 'kill -TERM "$FACTORIO_PID" 2>/dev/null || true' INT TERM

# `wait` returns early (exit status >128) when interrupted by a caught signal, even though
# the child is still shutting down/saving. Under `set -e` that would trip the EXIT trap and
# tear down the bridge (and PID 1) before Factorio finishes. Keep waiting until the process
# has actually exited.
set +e
wait "$FACTORIO_PID"
FACTORIO_EXIT=$?
while [[ $FACTORIO_EXIT -gt 128 ]] && kill -0 "$FACTORIO_PID" 2>/dev/null; do
    wait "$FACTORIO_PID"
    FACTORIO_EXIT=$?
done
set -e
exit "$FACTORIO_EXIT"
