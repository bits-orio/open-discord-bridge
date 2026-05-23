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

| Variable | Meaning | Default |
|---|---|---|
| `ODB_TRANSPORT` | Transport: `local` or `sftp` | `local` |
| `ODB_POLL_INTERVAL` | Local tailer poll interval | `1s` |
| `ODB_RCON_ADDRESS` | Factorio RCON `host:port` | — (required) |
| `FACTORIO_RCON_PASSWORD` | RCON password (**secret**) | — (required) |
| `ODB_EVENTS_FILE` | Path to `events.jsonl` (supports `${ENV}` and `~/`) | — (required) |
| `ODB_REQUIRED_MOD_VERSION` | Minimum mod version (surfaced in `/v1/status`) | — |
| `DISCORD_BOT_TOKEN` | Discord bot token (**secret**) | — (required) |
| `ODB_DISCORD_GUILD_ID` | Discord server (guild) ID | — |
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
| `ODB_SFTP_KNOWN_HOSTS` | known_hosts file; omit to skip host-key check (logs a warning) | — |

Provide routes via **either** `ODB_DISCORD_CHANNEL_ID` (simple, one channel) **or**
`ODB_ROUTES` (e.g. `vanilla.chat=111,mts.*=222,*=111`).

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
- **Control API:** `GET /v1/status` (health, mod version, event catalog), `GET/POST
  /v1/config` (live routing reload), `/v1/discord/guilds`, `/v1/discord/channels`,
  `POST /v1/test`. Contract: [`bridge/pkg/controlapi/spec/openapi.yaml`](bridge/pkg/controlapi/spec/openapi.yaml).

## 6. Security

- Secrets only via environment variables — never in `bridge.yaml` or the image.
- Control API requires a bearer token; bind it to loopback (or a private network) and
  front it with TLS if exposed.
- Keep RCON off the public internet (loopback, private network, or tunnel).

## 7. Releasing (maintainers)

CI/CD lives in `.github/workflows/` (runs once the repo is on GitHub):
- **`ci.yml`** — `go vet` + `go test` + `go build` on every push/PR.
- **`release.yml`** — on a `v*` tag: builds and pushes the bridge and sidecar images to
  GHCR, and publishes cross-platform binaries (+ the egg) to a GitHub Release.

Cut a release:
```sh
git tag v0.1.0 && git push origin v0.1.0
```
Publishes:
- `ghcr.io/<owner>/open-discord-bridge:{latest,v0.1.0}` — bridge
- `ghcr.io/<owner>/open-discord-bridge-sidecar:{latest,v0.1.0}` — Factorio + bridge
- binaries `odb-bridge-<os>-<arch>` (linux/amd64, linux/arm64, windows/amd64, darwin/arm64)

After the first publish, **make the GHCR package public** (GitHub → Packages → settings) so
hosts/panels can pull without auth. Ensure `deploy/pterodactyl-egg.json`'s `docker_images`
matches your published path (`<owner>` = your GitHub org/user).
