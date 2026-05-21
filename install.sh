#!/usr/bin/env bash
# Open Discord Bridge — installer for the local MVP test rig.
#
# Builds the bridge binary, creates an isolated Factorio mods directory containing
# only the companion mod (so the test boots fast, ignoring your full mod pack), and
# generates bridge.yaml + .env pre-filled with the right paths for this machine.
set -euo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
RUN_DIR="$REPO/.run"
RUN_MODS="$RUN_DIR/mods"
FACTORIO_DATA="$HOME/factorio"                 # write-data dir (see config-path.cfg)
# Written into bridge.yaml as a literal ${HOME}/... — the bridge expands it at runtime,
# so the config stays portable and free of an absolute home path.
EVENTS_FILE='${HOME}/factorio/script-output/open-discord-bridge/events.jsonl'

# ─── 1. Build the bridge ─────────────────────────────────────────────────────
GO_BIN="$(command -v go || true)"
if [[ -z "$GO_BIN" && -x "$HOME/.local/go-sdk/go/bin/go" ]]; then
    GO_BIN="$HOME/.local/go-sdk/go/bin/go"
fi
if [[ -z "$GO_BIN" ]]; then
    echo "ERROR: Go not found (looked on PATH and in ~/.local/go-sdk/go/bin)." >&2
    exit 1
fi
echo "Building bridge with $GO_BIN ..."
( cd "$REPO/bridge" && "$GO_BIN" build -o odb-bridge ./cmd/bridge )
echo "  -> $REPO/bridge/odb-bridge"

# ─── 2. Isolated mods directory with just the companion mod ──────────────────
NAME=$(grep -o '"name": *"[^"]*"' "$REPO/companion-mod/info.json" | head -1 | sed 's/.*"\([^"]*\)"/\1/')
VERSION=$(grep -o '"version": *"[^"]*"' "$REPO/companion-mod/info.json" | head -1 | sed 's/.*"\([^"]*\)"/\1/')
LINK_NAME="${NAME}_${VERSION}"

mkdir -p "$RUN_MODS"
# Refresh the symlink to the companion mod.
for old in "$RUN_MODS/${NAME}_"*; do
    [[ -L "$old" ]] && rm "$old"
done
ln -s "$REPO/companion-mod" "$RUN_MODS/$LINK_NAME"
echo "Linked mod: $RUN_MODS/$LINK_NAME -> $REPO/companion-mod"

# Factorio reads mods enabled-state from mod-list.json; create one enabling base + ours.
cat > "$RUN_MODS/mod-list.json" <<EOF
{
  "mods": [
    { "name": "base", "enabled": true },
    { "name": "$NAME", "enabled": true }
  ]
}
EOF

# ─── 3. Minimal server settings (local, no login required) ───────────────────
mkdir -p "$RUN_DIR"
cat > "$RUN_DIR/server-settings.json" <<'EOF'
{
  "name": "Open Discord Bridge MVP",
  "description": "Local bridge test server",
  "visibility": { "public": false, "lan": true },
  "require_user_verification": false,
  "game_password": "",
  "auto_pause": false
}
EOF
echo "Wrote $RUN_DIR/server-settings.json"

# ─── 4. Bridge config + env (only create if missing) ─────────────────────────
if [[ ! -f "$REPO/bridge/.env" ]]; then
    cat > "$REPO/bridge/.env" <<EOF
# Fill in your Discord bot token. RCON password is shared with start-server.sh.
DISCORD_BOT_TOKEN=replace-with-your-bot-token
FACTORIO_RCON_PASSWORD=odb-rcon-dev
EOF
    echo "Created bridge/.env (edit DISCORD_BOT_TOKEN)"
else
    echo "Kept existing bridge/.env"
fi

if [[ ! -f "$REPO/bridge/bridge.yaml" ]]; then
    cp "$REPO/bridge/bridge.yaml.example" "$REPO/bridge/bridge.yaml"
    # Point events_file at this machine's real script-output path.
    sed -i "s|events_file:.*|events_file: $EVENTS_FILE|" "$REPO/bridge/bridge.yaml"
    echo "Created bridge/bridge.yaml (edit channel_id values)"
else
    echo "Kept existing bridge/bridge.yaml"
fi

cat <<'EOF'

Install complete.

────────────────────────────────────────────────────────────────────────────
Discord bot setup
────────────────────────────────────────────────────────────────────────────

If you DON'T have a bot yet:
  1. Open https://discord.com/developers/applications  ->  "New Application", name it.
  2. Left sidebar -> "Bot".
  3. Under "Privileged Gateway Intents", turn ON "Message Content Intent" and save.
     (Required, or Discord -> game messages will arrive empty.)
  4. Click "Reset Token", confirm, and copy the token -> this goes in bridge/.env.
  5. Invite it to your server: left sidebar -> "OAuth2" -> "URL Generator":
       - Scopes:       bot
       - Bot perms:    View Channels, Send Messages, Read Message History
     Copy the generated URL at the bottom, open it, pick your server, Authorize.

If you ALREADY have a bot:
  1. Open https://discord.com/developers/applications and select it.
  2. "Bot" -> confirm "Message Content Intent" is ON.
  3. Token: if you saved it, reuse it. If not, "Reset Token" to get a new one
     (this invalidates the old one) -> put it in bridge/.env.
  4. Make sure the bot is already in your target server with the perms above;
     if not, re-invite it using the OAuth2 URL Generator steps above.

Getting the channel ID (for bridge.yaml):
  Discord -> User Settings -> Advanced -> enable "Developer Mode".
  Right-click your target channel -> "Copy Channel ID".

────────────────────────────────────────────────────────────────────────────
Then:
  1. Edit bridge/.env        -> set DISCORD_BOT_TOKEN
  2. Edit bridge/bridge.yaml  -> set both channel_id values to your channel ID
  3. ./start-all.sh           -> launches Factorio + the bridge together

(Or run ./start-server.sh and bridge/odb-bridge separately.)
EOF
