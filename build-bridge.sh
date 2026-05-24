#!/usr/bin/env bash
# Build bridge/odb-bridge if it's missing or older than its Go sources. Called by the
# start scripts so editing bridge code can't leave you silently running a stale binary.
#
# If Go isn't installed, this falls back to the existing binary (erroring only when there
# is none) — so the sidecar/production path keeps working without a Go toolchain.
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
BRIDGE="$REPO/bridge/odb-bridge"

# Same Go detection as install.sh: PATH first, then the local SDK location.
GO_BIN="$(command -v go || true)"
if [[ -z "$GO_BIN" && -x "$HOME/.local/go-sdk/go/bin/go" ]]; then
    GO_BIN="$HOME/.local/go-sdk/go/bin/go"
fi

if [[ -z "$GO_BIN" ]]; then
    if [[ -x "$BRIDGE" ]]; then
        echo "build-bridge: Go not found; using existing $BRIDGE" >&2
        exit 0
    fi
    echo "ERROR: $BRIDGE missing and Go not found (PATH or ~/.local/go-sdk/go/bin)." >&2
    exit 1
fi

# Rebuild if the binary is absent or any source (.go / go.mod / go.sum) is newer than it.
needs_build=0
if [[ ! -x "$BRIDGE" ]]; then
    needs_build=1
elif [[ -n "$(find "$REPO/bridge" \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) -newer "$BRIDGE" -print -quit)" ]]; then
    needs_build=1
fi

if [[ "$needs_build" == 1 ]]; then
    echo "build-bridge: compiling odb-bridge with $GO_BIN ..." >&2
    ( cd "$REPO/bridge" && "$GO_BIN" build -o odb-bridge ./cmd/bridge )
    echo "build-bridge: -> $BRIDGE" >&2
else
    echo "build-bridge: odb-bridge is up to date." >&2
fi
