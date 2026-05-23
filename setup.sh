#!/usr/bin/env bash
# Interactive setup wizard: validate your Discord bot token, pick a server + channel, and
# write bridge/bridge.yaml + bridge/.env. Run this for guided configuration.
#
# For self-hosters with shell access (bare metal or your own Docker). On a hosting panel
# like Pterodactyl, use the egg's variables instead (deploy/pterodactyl-egg.json).
# (To build the bridge and link the mod, use ./install.sh.)
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"

GO_BIN="$(command -v go || true)"
if [[ -z "$GO_BIN" && -x "$HOME/.local/go-sdk/go/bin/go" ]]; then
    GO_BIN="$HOME/.local/go-sdk/go/bin/go"
fi
if [[ -z "$GO_BIN" ]]; then
    echo "ERROR: Go not found (looked on PATH and in ~/.local/go-sdk/go/bin)." >&2
    exit 1
fi

echo "Building the setup wizard ..."
( cd "$REPO/wizard" && "$GO_BIN" build -o odb-wizard ./cmd/wizard )

# Write config into bridge/ by default; pass through any extra flags (e.g. --out).
exec "$REPO/wizard/odb-wizard" --out "$REPO/bridge" "$@"
