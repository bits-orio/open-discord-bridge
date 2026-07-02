# Deployment Guide

How to run the Open Discord Bridge in every supported topology. The system has two
pieces:

- **companion mod** — runs inside Factorio, writes events to
  `script-output/open-discord-bridge/events.jsonl`, accepts inbound messages over RCON.
- **bridge** — a Go process that tails the events file → Discord, and relays Discord →
  game over RCON. Optionally serves the HTTP **Control API**.

The mod install is the same everywhere (symlink via `companion-mod/link-mod.sh`, or drop
the folder/zip named `open-discord-bridge_<version>` into the server's `mods/`). The rest
of this guide is about running the **bridge**.

---

## You only need ONE of these

Don't let the length of this guide put you off — the many options exist so the bridge
fits any setup, but **you pick exactly one path** and ignore the rest. Quick chooser:

- **Running a server at home?** → bare metal (`start-all.sh`) or Docker Compose. Done.
- **Already run Factorio in Docker?** → add the bridge as a standalone container, or use
  the sidecar image (`deploy/Dockerfile.sidecar`).
- **On a hosting panel (Pterodactyl)?** → sidecar (bridge + Factorio in one container) via
  `run-sidecar.sh`, or a standalone bridge server via `deploy/pterodactyl-egg.json`.
- **Bridge and Factorio on different machines?** → bridge-only with the SFTP transport.

The one constant across every path is **RCON** (it carries Discord → game). Everything
below is reference detail for whichever single path you chose.

---

## 1. Configuration: two modes

The bridge picks a mode automatically based on whether the `-config` file exists.

### File mode (`bridge.yaml`)
Used when the config file is present. Secrets are referenced indirectly by env-var name
(`token_env`, `password_env`, `auth_token_env`). See
[`bridge/bridge.yaml.example`](bridge/bridge.yaml.example). Best for bare-metal and
self-host.

### Env-var mode
Used automatically when the config file is **absent**. Everything comes from environment
variables — ideal for containers and panels (Pterodactyl), where config is injected as
env vars and no file is mounted.

> Creating the Discord bot, getting its token, and inviting it to your server are the same
> in every mode and don't require the wizard — see [QUICKSTART.md §1](QUICKSTART.md) for the
> manual bot + invite-URL + channel-ID steps.

| Variable | Meaning | Default |
|---|---|---|
| `ODB_TRANSPORT` | Transport: `local` or `sftp` | `local` |
| `ODB_POLL_INTERVAL` | Local tailer poll interval | `1s` |
| `ODB_LOG_FILE` | Also write logs to this file (`-` = stdout only) | `bridge.log` next to events (local) |
| `ODB_RCON_ADDRESS` | Factorio RCON `host:port` | — (required) |
| `FACTORIO_RCON_PASSWORD` | RCON password (**secret**) | — (required) |
| `ODB_EVENTS_FILE` | Path to `events.jsonl` (supports `${ENV}` and `~/`) | — (required) |
| `ODB_REQUIRED_MOD_VERSION` | Minimum mod version (surfaced in `/v1/status`) | — |
| `DISCORD_BOT_TOKEN` | Discord bot token (**secret**) | — (required) |
| `ODB_DISCORD_GUILD_ID` | Discord server (guild) ID | — |
| `ODB_EMBED` | Color integrator-event category labels via an ANSI code block (see below) | `false` |
| `ODB_ANNOUNCE_STATUS` | Post bridge↔Factorio connect/disconnect to Discord | `false` |
| `ODB_LINKED_ROLE_ID` | Discord role assigned to linked players | — |
| `ODB_LINKED_NICKNAME` | Nickname format for linked members (`{factorio}`/`{discord}`) | — |
| `ODB_DISCORD_CHANNEL_ID` | Shortcut: one catch-all `*` route to this channel | — |
| `ODB_ROUTES` | Explicit routes: `source=channel_id,source=channel_id` | — |
| `ODB_COMMANDS` | Discord→RCON commands: `!trigger=/rcon cmd;!t2=/cmd2` (public, single-line only) | — |
| `ODB_ADMIN_ROLES` | Comma-separated admin role IDs | — |
| `ODB_ADMIN_USERS` | Comma-separated admin user IDs | — |
| `ODB_CONTROL_API_ENABLED` | Enable the Control API | `false` |
| `ODB_CONTROL_API_LISTEN` | Control API bind address | `127.0.0.1:7777` |
| `BRIDGE_CONTROL_TOKEN` | Control API bearer token (**secret**, required if enabled) | — |
| `ODB_SFTP_HOST` | SFTP `host:port` (when `ODB_TRANSPORT=sftp`) | — |
| `ODB_SFTP_USER` | SFTP user | — |
| `ODB_SFTP_KEY_PATH` | Private key file for SFTP key auth | — |
| `SFTP_PASSWORD` | SFTP password (**secret**; alternative to a key) | — |
| `ODB_SFTP_KNOWN_HOSTS` | known_hosts file, verifies the remote host's identity | — |
| `ODB_SFTP_ALLOW_INSECURE_HOST_KEY` | Explicitly accept connecting without `ODB_SFTP_KNOWN_HOSTS` (password auth only; see below) | `false` |

Provide routes via **either** `ODB_DISCORD_CHANNEL_ID` (simple, one channel) **or**
`ODB_ROUTES` (e.g. `vanilla.chat=111,mts.*=222,*=111`).

> **SFTP + password auth requires `ODB_SFTP_KNOWN_HOSTS`.** Without host key
> verification, a network attacker could MITM the connection and capture the SFTP
> password on any reconnect, so the bridge refuses to start in that combination. Set
> `ODB_SFTP_KNOWN_HOSTS` (recommended), or set `ODB_SFTP_ALLOW_INSECURE_HOST_KEY=true` to
> explicitly accept the risk (e.g. on a trusted private network). Key-only SFTP auth is
> unaffected — it keeps working with just a logged warning when `ODB_SFTP_KNOWN_HOSTS` is
> unset, same as before.

**Category label colors:** with `embed: true` (`ODB_EMBED`), integrator events (`mts.*`,
`oarc.*`, …) render in an ANSI code block with just their `[ns → event]` category label
colored (deterministic per key), so all events of one type share a color and differ from
other types. Vanilla/bridge events and chat stay plain text. No Embed Links permission is
needed. Caveats: ANSI lines are monospace code-block cards, may show uncolored on some
mobile/older clients, and Discord markdown (`**bold**`) does not render inside them.
"Chat" is any event keyed `chat` or `<namespace>.chat` (`mts.chat` → plain).

### Which setup path? (also pick one)

How you actually supply the config depends on how you run the bridge — these are
alternatives, not steps:

- **Self-hosting with a shell** (bare metal, your own Docker) → run **`./setup.sh`** (the
  guided wizard: validate token, pick server + channel, write `bridge.yaml` + `.env`), or
  hand-edit the files. *File mode.*
- **On a hosting panel** (Pterodactyl) → import **`deploy/pterodactyl-egg.json`** and fill
  its variables in the panel UI. *Env-var mode; the wizard is not used here.*
- **A managed portal** (e.g. AleForge) → the portal embeds the wizard library and presents
  its own UI; end users configure nothing by hand.

---

## 2. Discord → RCON commands

A message whose **first word** matches a configured command runs an **RCON command** and
posts the reply (other messages relay into game chat as normal). You choose exactly which
commands exist, and which require admin.

```yaml
discord:
  admins:
    roles: ["112233445566778899"]   # role IDs that count as admin
    users: ["998877665544332211"]   # user IDs
    # use_discord_permission: true   # default: Discord "Administrator" perm also = admin
  commands:
    - trigger: "!players"
      rcon: "/players online"        # public

    - trigger: "!ban"
      admin: true                    # admins only
      rcon: "/ban someone"

    - trigger: "!cleanup"            # multiline / script (sent as one RCON call)
      admin: true
      rcon: |
        /silent-command
        local n = 0
        for _, e in pairs(game.surfaces[1].find_entities_filtered{name="item-on-ground"}) do
          e.destroy(); n = n + 1
        end
        rcon.print("removed " .. n)
```

**Who is admin** (checked in order): the author's user ID is in `admins.users`; or they
hold a role in `admins.roles`; or — unless `use_discord_permission: false` — they have
Discord's **Administrator** permission. So in the common case (Discord admins = your
server admins) you configure nothing; the lists are for when those sets differ.

**Slash commands:** when `discord.guild_id` is set, the configured commands are also
registered as native slash commands (e.g. `/players`, `/ban`) — `admin: true` ones are
gated by Discord's own permissions, and `args: true` ones get an `args` option. The bot
needs the `applications.commands` scope (the wizard's invite URL includes it).

**Player linking:** the companion mod can map Discord users ↔ Factorio players. A player
runs `/odb-link` in-game to get a short code, then runs your link command in Discord
(e.g. `!link CODE`). Wire it as a normal command using the `{userid}` token:
```yaml
- trigger: "!link"
  args: true
  rcon: "/odb-confirm-link {1} {userid} {user}"
```
Other mods read the mapping via `remote.call("open-discord-bridge-v1", "linked_discord_id",
player_name)`. (`{userid}` is the Discord user ID; `{user}` is their name.)

**Managing links** — the mod also provides: `/odb-unlink` (in-game, unlink yourself),
`/odb-unlink-discord <id>`, `/odb-unlink-player <name>`, `/odb-unlink-all`, and `/odb-links`
(all RCON). Expose them as commands — self-serve unlink needs no typed args (it uses
`{userid}`); the rest are admin:
```yaml
- { trigger: "!unlink",        args: true,  rcon: "/odb-unlink-discord {userid}" }
- { trigger: "!links",         admin: true, rcon: "/odb-links" }
- { trigger: "!unlink-player", admin: true, args: true, rcon: "/odb-unlink-player {args}" }
- { trigger: "!unlink-all",    admin: true, rcon: "/odb-unlink-all" }
```
Links persist in the mod's save `storage` across restarts (per save/world).

**Showing linked status in Discord** — the bridge can reflect link state on the member:
- `discord.linked_role_id` — assigns/removes a role on link/unlink (colored name + member
  list grouping + optional role icon). Bot needs **Manage Roles** and a role **above** the
  target role.
- `discord.linked_nickname` — sets the member's nickname, e.g. `"{discord} | {factorio}"`
  ({factorio} = in-game name, {discord} = their Discord name); cleared on unlink. Bot needs
  **Manage Nicknames** (and can't rename the server owner).

It reconciles every ~20s from the mod's link state, so it self-heals and reflects unlinks.
Grant the extra permissions when enabling these (re-invite the bot or edit its role);
failures are logged.

**Notes:**
- Anyone in the channel can run **public** commands — keep destructive ones `admin: true`.
- `rcon` may be **multiline** (a `/silent-command` script); it's sent as a single RCON call.
- Admin-gating, multiline, and args need the **YAML file**; env mode (`ODB_COMMANDS`) is
  the simple subset (public, single-line). `ODB_ADMIN_ROLES`/`ODB_ADMIN_USERS` set admins.
- **Arguments:** set `args: true` to interpolate the message into `rcon`: `{args}` (all
  words after the trigger), `{1}`/`{2}`/… (positional), `{user}` (sender name). Example:
  `args: true`, `rcon: "/kick {1}"` → `!kick Bob` runs `/kick Bob`. User input is
  sanitized (newlines/control chars stripped, length-capped) so it can't inject a second
  RCON command — but a template like `/silent-command {args}` still hands users Lua, so
  keep arg commands `admin: true` unless the RCON command is safe.

## 3. Transports: how the bridge reads game events

Inbound (Discord → game) is **always** RCON. The transport only concerns how the bridge
reads the mod's `events.jsonl`:

| Transport | How | Status | Use when |
|---|---|---|---|
| **Local (file)** | Reads the events file from a filesystem path (`inotify`-free polling) | **Available** | Bridge shares a filesystem with Factorio — same host, or a shared/bind mount |
| **Shared mount** | Local transport against a bind-mounted Factorio data dir | **Available** (it *is* Local) | Bridge in its own container, but the host can mount Factorio's data dir into it |
| **SFTP** | Pulls the events file over SFTP (key or password auth, self-reconnecting) | **Available** | Isolated containers with no shared FS; fits Pterodactyl's per-server SFTP |
| **SSH streaming** | Streams events over a persistent SSH connection (`tail -F`) | **Planned (deprioritized, later phase)** | Large fleets where per-server SFTP polling overhead matters; an alternative to SFTP |

When Factorio and the bridge are on different hosts/containers, set `ODB_RCON_ADDRESS`
(or `factorio.rcon.address`) to the Factorio server's network address — not `127.0.0.1`.

---

## 4. Deployment methods

Target deployments, in priority order (RCON is used in all of them):

| # | What launches | Where | Transport(s) | Status |
|---|---|---|---|---|
| 1 | Factorio + bridge together | bare metal | Local (file) | `start-all.sh` ✅ |
| 2 | Bridge only | bare metal | Local (file) or SFTP | `start-bridge.sh` ✅ |
| 3 | Factorio + bridge together | single container (sidecar) | Local (file) | `run-sidecar.sh` ✅ |
| 4 | Bridge only | single container | SFTP | `docker run` (env mode) ✅ |
| 5 | Factorio + bridge | separate containers (compose) | shared volume (Local) or SFTP | `docker-compose.yml` ✅ |

SSH streaming is deliberately deferred to a later phase as an alternative to SFTP.

### A. Bare metal / local (no container)
Bridge and Factorio on the same machine; Local transport over loopback RCON.

```sh
./install.sh          # builds the bridge, links the mod, writes config + server-settings
./setup.sh            # guided wizard: validate token, pick server/channel, write config
                      # (or edit bridge/.env + bridge/bridge.yaml by hand instead)
./start-all.sh        # launches Factorio + the bridge
```
Or run the bridge by hand: `./bridge/odb-bridge -config bridge/bridge.yaml`.

**Bridge only** (Factorio runs elsewhere — same box with a shared file, or remote via
SFTP, set in `bridge.yaml`): `./start-bridge.sh`.

**Launch both as a sidecar** (bridge in background, Factorio in foreground, bridge stops
when Factorio exits) — also the building block for the single-container path:
```sh
./run-sidecar.sh <your factorio launch command...>
```

### B. Docker — standalone container (no compose)
Env-var mode, no file mounted. Mount or share the path holding `events.jsonl`.

```sh
docker build -t open-discord-bridge:latest ./bridge

docker run -d --name odb-bridge \
  -e ODB_RCON_ADDRESS=127.0.0.1:27015 \
  -e FACTORIO_RCON_PASSWORD=... \
  -e DISCORD_BOT_TOKEN=... \
  -e ODB_EVENTS_FILE=/factorio/script-output/open-discord-bridge/events.jsonl \
  -e ODB_DISCORD_CHANNEL_ID=123456789012345678 \
  -e ODB_CONTROL_API_ENABLED=true -e BRIDGE_CONTROL_TOKEN=$(openssl rand -hex 32) \
  -p 127.0.0.1:7777:7777 \
  -v /opt/factorio:/factorio:ro \
  open-discord-bridge:latest
```

### C. Docker Compose (full stack, self-host)
Brings up Factorio + the bridge together. Convenience for self-hosters only.

```sh
cp .env.example .env          # set the three secrets
mkdir -p .run && printf '%s' "$FACTORIO_RCON_PASSWORD" > .run/rconpw
# edit bridge/bridge.docker.yaml -> set channel_id
docker compose up --build
```
See [`docker-compose.yml`](docker-compose.yml).

### D. Bridge-only container (Factorio elsewhere)
The bridge is containerized; Factorio runs on another host/container.

- RCON: `ODB_RCON_ADDRESS=game-host:27015` (reachable over the network).
- Events: either bind-mount Factorio's data dir read-only (shared-mount → Local), or set
  `ODB_TRANSPORT=sftp` with `ODB_SFTP_HOST`/`ODB_SFTP_USER`/`ODB_SFTP_KEY_PATH`.

### E. Panels — Pterodactyl / Wings (sidecar)
The panel controls lifecycle. This is **AleForge's model** — no compose. A shared volume
between two containers isn't available there, so the chosen shape is a **sidecar**: the
bridge runs alongside Factorio in the **same** container, launched by a custom startup.

- Startup launches both via `run-sidecar.sh <factorio command>` (bridge background,
  Factorio foreground). Local transport — events file is local, RCON is loopback.
- Configure via egg **variables → env vars** (env-var mode; no mounted file needed).
- Panel-friendly: logs to stdout, stops on `SIGTERM`, and `POST /v1/restart` exits cleanly
  so the panel restarts it.
- The bridge binary must be present in the container (bake it into the image, or download
  it at startup).

Reference artifacts:
- **`deploy/Dockerfile.sidecar`** — a single-container image (factoriotools/factorio + the
  bridge) whose entrypoint is `run-sidecar.sh /docker-entrypoint.sh`. Build with
  `docker build -f deploy/Dockerfile.sidecar -t open-discord-bridge-sidecar .`; configure
  the bridge via env vars. Adapt the base image to your own.
- **`deploy/pterodactyl-egg.json`** — an egg for the *standalone bridge* server (separate
  container, env-var config; SFTP or shared-mount). Import it, set the variables, point
  `docker_images` at your published bridge image.

> SFTP from a separate container is a viable fallback (set `ODB_TRANSPORT=sftp`), but
> AleForge prefers the sidecar for a self-contained, low-maintenance setup.

---

## 5. Lifecycle & operations

- **Logs:** stdout/stderr (panel- and Docker-friendly).
- **Shutdown:** graceful on `SIGINT`/`SIGTERM`.
- **Restart:** `POST /v1/restart` performs a clean exit; a supervisor (systemd
  `Restart=always`, Docker/Pterodactyl restart policy) brings it back with fresh config.
- **Permission preflight:** on connect, the bridge checks its Discord permissions for the
  configured features (Send/View/Read in each bridged channel, honoring channel overrides;
  Manage Roles + role hierarchy and Manage Nicknames if linked role/nickname) and warns
  about anything missing — in the logs and as a one-off message to the bridged channel.
- **Connection announcements:** with `discord.announce_status` (or `ODB_ANNOUNCE_STATUS`),
  the bridge polls the RCON+mod handshake every ~15s and posts `bridge.established` /
  `bridge.disconnected` to Discord when the link to Factorio comes up or drops. They route
  like any event (under `bridge.*`), so a catch-all `*` route covers them.
- **Control API:** `GET /v1/status` (health, mod version, event catalog), `GET/POST
  /v1/config` (live routing reload), `/v1/discord/guilds`, `/v1/discord/channels`,
  `POST /v1/test`. Contract: [`bridge/pkg/controlapi/spec/openapi.yaml`](bridge/pkg/controlapi/spec/openapi.yaml).

## 6. Security

- Secrets only via environment variables — never in `bridge.yaml` or the image.
- Control API requires a bearer token; bind it to loopback (or a private network) and
  front it with TLS if exposed.
- Keep RCON off the public internet (loopback, private network, or tunnel).

### Command access model
- **Inbound Discord chat is never run as a game command** — only a message whose first
  word matches a configured trigger runs RCON; everything else is relayed as chat text.
- **Two trust boundaries:** in-game, mod commands guard on `cmd.player_index` so the
  RCON-only ones (`/odb-incoming`, `/odb-status`, `/odb-confirm-link`, `/odb-unlink-*`,
  `/odb-links`) can't be run from a player's console — only `/odb-link` and `/odb-unlink`
  are player self-service. In Discord, commands gated with `admin: true` require an admin.
- **`{userid}` is gateway-supplied, not from the message** — so `!unlink` can only unlink
  the caller's own account.
- **Arg interpolation is sanitized** (newlines/control chars stripped) so user input can't
  inject a second RCON line.
- **You own the config risk:** a destructive RCON command mapped **without** `admin: true`
  is runnable by anyone who can post in the channel. Keep destructive commands
  `admin: true`; only expose read-only ones (e.g. `/players`) publicly; restrict the
  bridged channel if needed. An admin-only `/c`/`/silent-command` command still grants
  admins arbitrary Lua — by design.

## 7. Releasing (maintainers)

`companion-mod/info.json`'s `version` is the single source of truth; the release tag must be
`v<that version>` (the workflow fails the build if they disagree). The full runbook is the
**bump-version** skill (`.claude/skills/bump-version.md`); the short version:

```sh
# 1. bump-version skill: bump companion-mod/info.json, add a companion-mod/changelog.txt entry, commit, push
# 2. cut the release (creates + pushes the tag after sanity checks):
./tools/release.sh
```

CI/CD lives in `.github/workflows/`:
- **`ci.yml`** — `go vet` + `go test` + `go build` on every push/PR.
- **`release.yml`** — on a `v*` tag: extracts release notes from `companion-mod/changelog.txt`, then
  - builds + pushes the bridge and sidecar images to GHCR,
  - builds cross-platform binaries and the companion-mod zip,
  - publishes a GitHub Release (notes + binaries + egg + mod zip),
  - posts the changelog to Discord (`DISCORD_WEBHOOK`),
  - uploads the companion mod to the Factorio mod portal and pings the announcements
    channel (`DISCORD_ANNOUNCEMENTS_WEBHOOK`).
- **`mod-portal-upload.yml`** — manual (workflow_dispatch) retry of just the portal upload;
  idempotent.

Publishes:
- `ghcr.io/<owner>/open-discord-bridge:{latest,v0.1.0}` — bridge
- `ghcr.io/<owner>/open-discord-bridge-sidecar:{latest,v0.1.0}` — Factorio + bridge
- binaries `odb-bridge-<os>-<arch>` / `odb-wizard-<os>-<arch>` (linux/amd64, linux/arm64,
  windows/amd64, darwin/arm64), `deploy/pterodactyl-egg.json`, and
  `open-discord-bridge_<version>.zip` (the companion mod)

**Optional integrations** (each step is skipped if its repo secret is unset, so the GitHub
release + GHCR images always publish):
- `DISCORD_WEBHOOK` — changelog channel post.
- `DISCORD_ANNOUNCEMENTS_WEBHOOK` — "live on the mod portal" post.
- `FACTORIO_API_KEY` (scope: *ModPortal: Upload Mods*) — companion-mod portal upload. The
  upload API adds a release to an **existing** mod page, so create the `open-discord-bridge`
  page on mods.factorio.com once before the first portal upload.

After the first publish, **make the GHCR package public** (GitHub → Packages → settings) so
hosts/panels can pull without auth. Ensure `deploy/pterodactyl-egg.json`'s `docker_images`
matches your published path (`<owner>` = your GitHub org/user).
