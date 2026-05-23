# Verification Checklist

A concrete end-to-end pass to run after building. Most of this can't be unit-tested (it
needs a live Factorio server + Discord), so this is the manual checklist. Tick as you go.

## Setup
- [ ] `./install.sh` (builds bridge, links the companion mod, writes server-settings)
- [ ] `./setup.sh` **or** edit `bridge/bridge.yaml` + `bridge/.env` (token, channel)
- [ ] Add the example commands to `bridge.yaml` (`!players`, `!kick`, `!link`) — see
      `bridge/bridge.yaml.example`
- [ ] `./start-all.sh` → logs `connected to Discord; tailing …` and `companion mod version …`

## 1. Core relay (needs a connected Factorio client)
- [ ] In-game chat → appears in the Discord channel
- [ ] Discord message → appears in-game as `[Discord] you: …`
- [ ] No echo loop (a Discord message produces exactly one in-game line, no Discord echo)
- [ ] Special chars / emoji / multi-line Discord message arrive intact in-game

## 2. Events (game → Discord)
- [ ] Join / leave (reconnect a client)
- [ ] Death — `/c game.player.character.die()` (admin; disables achievements on that save)
- [ ] Research finished — queue a cheap tech, or `/c game.player.force.research_all_technologies()`
- [ ] `last_event_unix` in `/v1/status` advances after an event (if Control API enabled)

## 3. Commands (text)
- [ ] `!players` → bot replies with the online list
- [ ] `!kick <name>` as a **non-admin** → "admin-only" refusal
- [ ] `!kick <name>` as an **admin** (Discord Administrator, or in `admins.users/roles`) → runs
- [ ] `!kick` with no argument → usage hint
- [ ] A `/silent-command` multiline command (if configured) runs as one script

## 4. Slash commands (guild_id must be set)
- [ ] `/players` appears in Discord's command list and works
- [ ] An `admin: true` command (e.g. `/kick`) is hidden/blocked for non-admins by Discord
- [ ] An `args: true` slash command shows an `args` option and interpolates it

## 5. Player linking
- [ ] In-game: `/odb-link` → you get a 6-char code (printed to you only)
- [ ] In Discord: `!link <code>` → bot replies "Linked … to Discord user …", in-game broadcast
- [ ] Re-using the same code → "invalid or already used"
- [ ] Waiting >60s then linking → "expired"
- [ ] (For integrators) `remote.call("open-discord-bridge-v1","linked_discord_id",name)` returns the ID

## 6. Embeds (optional)
- [ ] Set `discord.embed: true` (or `ODB_EMBED=true`), restart → events render as colored
      embeds (join green, death red, rocket blue, mts.* neutral)

## 7. MTS integration (run with multi-team-support loaded)
- [ ] Claim a team → `[mts → team created]` / `[mts → player joined team]` in Discord
- [ ] Produce first science → `[mts → milestone first] <team> was the first to …`

## 8. Transports
- [ ] **Local** (default) — events flow as above
- [ ] **SFTP** — set `transport: sftp` + `factorio.sftp` (host/user/key) pointing at the
      events file on the remote; events still flow; killing/restoring the SSH path reconnects

## 9. Control API (if `control_api.enabled: true`)
```sh
set -a; . bridge/.env; set +a
curl -s -H "Authorization: Bearer $BRIDGE_CONTROL_TOKEN" :7777/v1/status | python3 -m json.tool
```
- [ ] no token → 401; valid token → 200 JSON
- [ ] `/v1/status` shows `discord.connected`, `factorio.rcon_ok`, `mod_version`, `sources`
- [ ] `GET /v1/config` (no secrets); `POST /v1/config` with new routes → applied live
- [ ] `GET /v1/discord/guilds` and `/v1/discord/channels?guild_id=…`
- [ ] `POST /v1/test` → `{outbound_ok, inbound_ok}`
- [ ] `POST /v1/restart` → 202, process exits (supervisor restarts it)

## 10. Resilience
- [ ] Restart the Factorio server while the bridge runs → bridge reconnects RCON; events
      resume after the mod truncates `events.jsonl`
- [ ] Restart the bridge → resumes tailing from the live end (no replay of old events)

> Notes: the wizard's token/password prompts echo (no hidden input). `/c` console
> commands need you to be a server admin. SSH streaming transport is deferred to a later
> phase.
