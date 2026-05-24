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
- [ ] Discord message → appears in-game as `[Discord] <name>: …` with a blurple `[Discord]`
      tag so it's distinct from in-game chat
- [ ] The `<name>` is your Discord **display name** (e.g. `bits-orio`), not the raw
      username (new usernames can't contain hyphens)
- [ ] If your Discord account is **linked** (see §5), your name in-game is tinted with your
      in-game chat color
- [ ] No echo loop (a Discord message produces exactly one in-game line, no Discord echo)
- [ ] Discord emoji do **not** render in-game (expected — not translated to rich text yet)

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
- [ ] **Persistence:** restart the server (same save) → the link still works (name still
      tints; `linked_discord_id` still returns the ID). Links live in the mod's save
      `storage`, so they survive restarts; a brand-new map starts with none.
- [ ] **Unlink (self):** `!unlink` in Discord (or `/odb-unlink` in-game) → removed; your
      name stops tinting
- [ ] **List (admin):** `!links` → lists `player -> discord (id)` for all links
- [ ] **Admin unlink:** `!unlink-player <name>` removes one; `!unlink-all` clears all
      (both reflected by `!links`)
- [ ] **Linked role** (set `discord.linked_role_id`, bot has Manage Roles + a higher role):
      within ~20s of linking, the user gets the role; after unlink, it's removed
- [ ] **Linked nickname** (set `discord.linked_nickname`, bot has Manage Nicknames): the
      user's nickname becomes the format (e.g. `name | FactorioPlayer`); cleared on unlink

## 6. Category label colors (optional)
- [ ] Set `discord.embed: true` (or `ODB_EMBED=true`), restart → **integrator** events
      (mts.*, oarc.*, …) render in an ANSI code block with just the `[ns → event]` label
      colored; the rest of the line is default. So all `mts.research_finished` lines share
      a color, distinct from `mts.team_created`, etc.
- [ ] The label color is **stable** per event key (deterministic, no per-mod hardcoding);
      palette is 6 ANSI colors, so distinct keys generally differ but can collide
- [ ] **Vanilla/bridge events and chat stay plain text** (no code block) — they already
      have distinctive emoji; only namespaced integrator events get the colored label
- [ ] Note: ANSI lines are **monospace code-block cards** and may show **uncolored** on some
      mobile/older clients; markdown (`**bold**`) does **not** render inside the block

## 7. MTS integration (run with multi-team-support loaded)
- [ ] Claim a team → `[mts → team created]` in Discord
- [ ] Produce first science → `[mts → milestone first] <team> was the first to …`
- [ ] **Research is team-aware:** completing a tech posts `<team> researched <tech>` (from
      MTS), and you do **not** also get a plain team-less `Research complete:` line (MTS
      disables the baseline research event)
- [ ] **Join/leave is team-aware:** connecting posts `[mts → player joined] <name> joined
      the game — <team>` (no plain `vanilla.player_joined`); disconnecting posts
      `[mts → player left] <name> left the game — <team>`. A player not on a team omits
      the `— <team>` suffix
- [ ] **No duplicate on first claim:** a brand-new player connecting produces exactly one
      join line (the team-aware connect), **not** also a separate `joined <team>` line
- [ ] **Mid-game switch:** moving a player between teams (admin move / `/mts-rejoin`) posts
      a single `[mts → player switched team] <name> switched to <team>` line
- [ ] **Baseline restored without MTS:** start the server with MTS *unloaded* → plain
      `vanilla.player_joined` / `vanilla.player_left` / `Research complete:` return
- [ ] **Category label color:** with `embed: true`, mts.* events render in an ANSI block
      with the `[mts → …]` label colored (stable per event key), the rest default

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

## 9b. Connection announcements (optional)
- [ ] Set `discord.announce_status: true` (or `ODB_ANNOUNCE_STATUS=true`), restart with
      Factorio up → a green **"Open Discord Bridge established"** posts to the channel
- [ ] Stop Factorio (leave the bridge running) → within ~15s a red **"disconnected"** posts
- [ ] Start Factorio again → **"established"** posts again (no spam in between)

## 9c. Permission preflight
- [ ] With `linked_role_id` set but the bot **lacking** Manage Roles (or its role below the
      linked role), restart → a ⚠️ warning posts to the channel and is logged on startup
- [ ] Grant the permission / fix hierarchy, restart → no warning

## 10. Resilience
- [ ] Restart the Factorio server while the bridge runs → bridge reconnects RCON; events
      resume after the mod truncates `events.jsonl`
- [ ] Restart the bridge → resumes tailing from the live end (no replay of old events)

> Notes: the wizard's token/password prompts echo (no hidden input). `/c` console
> commands need you to be a server admin. SSH streaming transport is deferred to a later
> phase.

## Known limitations / future
- **Emoji & Discord markdown** are passed through as text, not translated to Factorio
  [rich text](https://wiki.factorio.com/rich_text). Full translation (and using rich text
  to make Discord messages more distinctive) is a planned future feature.
- **Inbound message rich text isn't sanitized** — a Discord user typing `[color=…]` etc.
  is currently rendered as-is in-game. (Linked-name coloring wraps only the name to avoid
  breakage.) Sanitization/translation will come with the rich-text work.
