# Open Discord Bridge

Mirror chat and events between a Factorio server and Discord — and let **other mods plug
their own events into it**.

[![mod portal](https://img.shields.io/badge/Factorio%20Mod%20Portal-open--discord--bridge-5b91b6?logo=factorio)](https://mods.factorio.com/mod/open-discord-bridge)
[![ci](https://github.com/bits-orio/open-discord-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/bits-orio/open-discord-bridge/actions/workflows/ci.yml)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Factorio 2.0](https://img.shields.io/badge/Factorio-2.0-orange.svg)

Open Discord Bridge relays in-game chat and events (joins, deaths, rockets, research, …) to
a Discord channel, and delivers Discord messages back into the game. It's built to be
**generic**: it works standalone for a solo self-hoster, scales to managed hosting, and
exposes a frozen remote interface so mods like [multi-team-support][mts] or OARC integrate
*into* the bridge rather than the other way around. The bridge contains zero mod-specific
code.

[mts]: https://github.com/bits-orio/multi-team-support

## Features

- **Two-way chat** — game chat → Discord, Discord → game (over RCON, achievement-safe).
- **Baseline events with no setup** — chat, join/leave, deaths, rocket launches, research,
  server up — captured by the companion mod itself, so it works on a vanilla server.
- **Integrator events** — other mods push their own events (and receive inbound Discord
  messages) through the frozen `open-discord-bridge-v1` remote interface. An integrator can
  also take over a baseline event to enrich it (e.g. team-aware research).
- **Channel routing** — map events to channels by exact key, namespace glob (`mts.*`), or
  catch-all.
- **Configurable Discord → RCON commands** — public or admin-only, single- or multi-line,
  with argument interpolation; optionally exposed as Discord slash commands.
- **Player linking** — link a Discord account to an in-game player, with an optional synced
  role and nickname; linked players' names are tinted in-game.
- **Optional polish** — colored category labels for integrator events, connect/disconnect
  announcements, a startup permission preflight that tells you exactly what's missing, and
  an optional HTTP control API.

## How it works

```
Factorio  ──(events.jsonl)──▶  Bridge (Go)  ──▶  Discord
   ▲                              │
   └────────────(RCON)────────────┘   ◀── Discord messages / commands
```

- **Companion mod** (`companion-mod/`, a small Factorio 2.0 Lua mod) writes events to a
  JSONL file and owns the `open-discord-bridge-v1` interface for other mods. Required —
  install it from the in-game mod browser or the
  [Factorio Mod Portal](https://mods.factorio.com/mod/open-discord-bridge).
- **Bridge** (`bridge/`, a single Go binary) tails that file, posts to Discord, and relays
  Discord messages back via RCON. One process per server.
- **Wizard** (`wizard/`) — an optional setup CLI (and importable library for hosting panels)
  that creates the bot invite, picks a channel, and writes the config.

Events stream over a **local file** (bridge on the same host) or **SFTP** (remote host);
RCON is always used for inbound delivery.

## Quick start (local, one box)

You need Go 1.23+ and a Factorio headless server. Then:

```sh
./install.sh     # builds the bridge, links the companion mod, writes server settings
./setup.sh       # wizard: bot token → pick guild/channel → writes bridge/bridge.yaml + .env
./start-all.sh   # starts Factorio + the bridge together
```

Prefer to wire it by hand? Copy `bridge/bridge.yaml.example` → `bridge/bridge.yaml` and
`bridge/.env.example` → `bridge/.env` (token + RCON password go in `.env` — **never** in
`bridge.yaml`), then `./start-bridge.sh`.

Discord bot permissions: **View Channels, Send Messages, Read Message History**, and — only
if you use linked-player role/nickname — **Manage Roles + Manage Nicknames** (the bot's role
must sit above the linked role). The bridge logs (and posts) a warning on startup if any are
missing.

## Deployment

Bare-metal, a single container, Docker Compose, or a **Pterodactyl sidecar** (bridge in the
same container as the Factorio server) are all supported. Config can come from
`bridge.yaml` **or** `ODB_*` environment variables. See **[DEPLOYMENT.md](DEPLOYMENT.md)**
for every model, the full env-var reference, the control API, and transports.

Container images are published to GHCR on each release:
`ghcr.io/bits-orio/open-discord-bridge` (bridge) and `-sidecar` (Factorio + bridge).

## Integrating your mod

Guard every call so the bridge stays an *optional* dependency:

```lua
if remote.interfaces["open-discord-bridge-v1"] then
  remote.call("open-discord-bridge-v1", "emit", {
    event = "mymod.boss_defeated",
    data  = {
      text  = "**North** defeated the demolisher",  -- the sentence shown in Discord
      label = "💀",                                  -- optional emoji for the [ns → …] tag
    },
  })
end
```

The full interface — declaring an event catalog, taking over a baseline event, subscribing
to inbound Discord messages, and the emoji/markdown rules — is documented in
**[companion-mod/README.md](companion-mod/README.md)**.

## Project layout

| Path | What |
|---|---|
| `companion-mod/` | the required Factorio mod (baseline events + integrator interface) |
| `bridge/` | the Go bridge process |
| `wizard/` | setup CLI + importable library |
| `deploy/` | Dockerfiles, Pterodactyl egg |
| `tools/` | release + mod-portal upload scripts |
| `*.sh` | `install`, `setup`, `start-all`, `start-bridge`, `run-sidecar`, … |

## Docs

- **[DEPLOYMENT.md](DEPLOYMENT.md)** — deployment models, env vars, control API, releasing.
- **[companion-mod/README.md](companion-mod/README.md)** — the integrator API.
- **[TESTING.md](TESTING.md)** — end-to-end verification checklist.
- **[PLAN.md](PLAN.md)** — design rationale and roadmap.

## Releasing

`companion-mod/info.json`'s `version` is the source of truth; tag `v<version>` to publish.
Tagging builds the binaries, companion-mod zip, and GHCR images, drafts a GitHub Release
from `companion-mod/changelog.txt`, announces to Discord, and uploads the companion mod to the Factorio
mod portal. See the **bump-version** flow in [DEPLOYMENT.md](DEPLOYMENT.md#7-releasing-maintainers).

## License

[MIT](LICENSE) © bits-orio
