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

---

## 2. Transports: how the bridge reads game events

Inbound (Discord → game) is **always** RCON. The transport only concerns how the bridge
reads the mod's `events.jsonl`:

| Transport | How | Status | Use when |
|---|---|---|---|
| **Local (file)** | Reads the events file from a filesystem path (`inotify`-free polling) | **Available** | Bridge shares a filesystem with Factorio — same host, or a shared/bind mount |
| **Shared mount** | Local transport against a bind-mounted Factorio data dir | **Available** (it *is* Local) | Bridge in its own container, but the host can mount Factorio's data dir into it |
| **SFTP** | Pulls the events file over SFTP (key or password auth, self-reconnecting) | **Available** | Isolated containers with no shared FS; fits Pterodactyl's per-server SFTP |

When Factorio and the bridge are on different hosts/containers, set `ODB_RCON_ADDRESS`
(or `factorio.rcon.address`) to the Factorio server's network address — not `127.0.0.1`.

---

## 3. Deployment methods

### A. Bare metal / local (no container)
Bridge and Factorio on the same machine; Local transport over loopback RCON.

```sh
./install.sh          # builds the bridge, links the mod, writes config + server-settings
# edit bridge/.env (DISCORD_BOT_TOKEN) and bridge/bridge.yaml (channel_id)
./start-all.sh        # launches Factorio + the bridge
```
Or run the bridge by hand: `./bridge/odb-bridge -config bridge/bridge.yaml`.

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

### E. Panels — Pterodactyl / Wings (individual containers)
Each process is its own panel-managed container; the panel controls lifecycle. This is
**AleForge's model** — no compose.

- Configure via egg **variables → env vars** (env-var mode; no mounted file needed).
- RCON over the container network (`ODB_RCON_ADDRESS` = the Factorio allocation).
- Events access: shared mount or SFTP (decision pending AleForge infra; see below).
- The container is panel-friendly: logs to stdout, stops on `SIGTERM`, and
  `POST /v1/restart` exits cleanly so the panel restarts it.

> **Both event-access paths now work:** a Wings shared mount (Local transport) **or**
> SFTP (`ODB_TRANSPORT=sftp`, pointing at Pterodactyl's per-server SFTP). The open item is
> only which one AleForge prefers operationally. A Pterodactyl egg is a planned deliverable.

---

## 4. Lifecycle & operations

- **Logs:** stdout/stderr (panel- and Docker-friendly).
- **Shutdown:** graceful on `SIGINT`/`SIGTERM`.
- **Restart:** `POST /v1/restart` performs a clean exit; a supervisor (systemd
  `Restart=always`, Docker/Pterodactyl restart policy) brings it back with fresh config.
- **Control API:** `GET /v1/status` (health, mod version, event catalog), `GET/POST
  /v1/config` (live routing reload), `/v1/discord/guilds`, `/v1/discord/channels`,
  `POST /v1/test`. Contract: [`bridge/pkg/controlapi/spec/openapi.yaml`](bridge/pkg/controlapi/spec/openapi.yaml).

## 5. Security

- Secrets only via environment variables — never in `bridge.yaml` or the image.
- Control API requires a bearer token; bind it to loopback (or a private network) and
  front it with TLS if exposed.
- Keep RCON off the public internet (loopback, private network, or tunnel).
