# Open Discord Bridge — Project Plan

## What This Is

An open-source bridge that mirrors chat and events between Factorio servers and Discord.
Built to be generic: works for solo self-hosters, managed hosting providers, and everything
in between. AleForge is the reference managed-hosting integrator.

The gap this fills: every existing Factorio↔Discord solution is either tightly coupled to
one admin's server, mod-heavy with no vanilla fallback, RCON-only with no rich events, or
commercial with no community ownership. This project fixes all four.

---

## Repository Structure

```
open-discord-bridge/
├── PLAN.md                  ← this file
├── companion-mod/           ← Factorio Lua mod (required, published on Mod Portal)
├── bridge/                  ← Go bridge process (the core binary)
└── wizard/                  ← Setup wizard (open CLI + importable library)
```

Each sub-project is an independent repository. They share a version compatibility matrix
maintained in this parent repo and rendered into docs.

---

## Core Architectural Decisions

Three decisions that are non-negotiable. Understand them before touching code.

### 1. Per-customer Discord bot — no central service

Each customer registers their own Discord application under their own Discord developer
account. The bot token lives with the customer (stored encrypted in AleForge's portal,
or in an env var for self-hosters). AleForge never runs a shared bot that reads multiple
customers' guild chat.

**Why:**

- **GDPR.** A central bot reading every customer's guild makes AleForge a data processor
  across all those guilds simultaneously. That triggers DPAs with every EU customer,
  72-hour breach notification covering everyone, right-to-erasure at scale, Schrems II
  compliance, and potentially a DPO requirement. Per-customer compute reframes AleForge
  as an infrastructure provider — same legal relationship as hosting their Factorio server.

- **Discord platform risk.** A central bot in 100+ guilds triggers Discord's verification
  wall (multi-week review, business docs, ToS). A single abuse report or false positive
  can suspend it and take every customer offline simultaneously. Per-customer bots fail
  independently and never hit verification.

- **Structural trust.** "Your data goes through our shared infra but we promise not to
  look" is weaker than "your data goes through compute dedicated to you, under your bot."
  The latter is architecturally enforced.

- **Self-host comes free.** If the production deployment is already one bridge process
  per Factorio server, a self-hoster runs the same binary with a config file. No separate
  edition to maintain.

- **Customer retention.** Bot persists in their Discord developer account if they cancel
  AleForge. Second-time setup is under a minute (paste token, confirm guild, pick
  channels). Real value proposition: "Cancel anytime. Your bot is yours."

### 2. Required companion Factorio mod

All game-side I/O flows through a required Lua mod published on the Factorio Mod Portal.
There is no vanilla-compatible fallback. If the mod is not installed, the bridge does not
work.

**Why:**

- **Event coverage.** Vanilla log-tail can only see chat and connect/disconnect. The
  product customers actually want — rocket launches, deaths, research milestones, custom
  mod hooks — requires a mod. Going mod-required means the full event surface is
  first-class from day one.

- **Reliability.** Vanilla log-tail is fragile across Factorio versions (log format
  changes, locale variations, buffering quirks). The mod writes structured JSONL with a
  schema we control. Factorio API changes have documented migration notes; log format
  changes do not.

- **Branding.** Every multiplayer participant sees the mod list. A required mod means the
  bridge name appears in every game it powers — on the Mod Portal, in mod sync screens,
  in the in-game mod manager. This is permanent organic discovery.

- **Inbound path.** Without a mod, Discord→game messages require RCON chat-injection
  hacks. With a mod, the bridge calls a clean `remote.call` interface. The mod owns
  formatting, name color, and prefix.

- **Third-party extension (inverted coupling).** The mod turns the bridge into a
  community *substrate*. The dependency direction is deliberate: integrator mods
  (MTS, OARC, anything) depend on the bridge, never the reverse. The bridge mod owns a
  frozen, versioned `remote` interface (`open-discord-bridge-v1`); integrators call into it to push
  events and subscribe to inbound. The bridge contains zero mod-specific code and never
  breaks when an integrator changes. Integrators guard their calls with
  `remote.interfaces["open-discord-bridge-v1"]`, so the bridge stays an *optional* dependency of
  theirs. This is a moat no competitor can easily replicate.

**Cost:** Excludes pure-vanilla admins (small audience; they have log-tail tools). Setup
is two touchpoints (mod install + bot setup) instead of one — the wizard must handle
both ends.

### 3. Open control API — no AleForge moat

The bridge exposes a versioned HTTP control API (`/v1/config`, `/v1/status`, etc.)
documented as an OpenAPI spec in this repo. Any portal — AleForge's, a competitor's, a
Pterodactyl plugin, the open-source CLI — uses the same API. AleForge gets no privileged
endpoints.

AleForge's moat is their execution (UX, auto-provisioning, billing integration, support),
not the protocol. If they need a new capability, it lands in the open spec first.

The setup wizard logic (token validation, install URL generation, guild polling, channel
selection) lives in `wizard/` as a Go library. AleForge's portal embeds it; the
standalone CLI wraps it. Same state machine, different UI shells.

---

## System Architecture

### Diagram

```
                                         ┌──────────────────────────┐
                                         │      Discord (SaaS)      │
                                         └────────────┬─────────────┘
                                                      │ Gateway + REST
                                                      │ (one connection per bot)
                                                      │
┌─────────────────────────────────────────────────────┴──────────────────────────────┐
│          Customer-dedicated compute (AleForge container OR self-host box)           │
│                                                                                     │
│   ┌──────────────────────────┐              ┌─────────────────────────────────┐    │
│   │  Factorio server         │◄─ RCON ─────►│   Bridge process                │    │
│   │  + companion mod         │  remote.call │   - customer's bot token        │    │
│   │                          │  localhost   │   - one Discord gateway conn    │    │
│   │  Mod owns:               │              │   - pluggable Transport layer   │    │
│   │  - all event capture     │  JSONL ──────►   - open Control API (HTTP)     │    │
│   │  - inbound rendering     │  (see        │                                 │    │
│   │  - player-link state     │  Transport   │                                 │    │
│   │  - remote.call API       │  section)    │                                 │    │
│   └──────────────────────────┘              └──────────────────┬──────────────┘    │
│                                                                │                    │
└────────────────────────────────────────────────────────────────┼────────────────────┘
                                                                 │ HTTPS, token auth
                                                                 ▼
                                               ┌─────────────────────────────┐
                                               │  Portal (AleForge / CLI /   │
                                               │  any Control API consumer)  │
                                               │  - wizard/  library inside  │
                                               │  - stores bot token         │
                                               │  - manages routing config   │
                                               └─────────────────────────────┘
```

### Components

**`companion-mod/`** — Factorio Lua mod, required, published on Mod Portal. Two layers:
- **Substrate layer** — owns the frozen `open-discord-bridge-v1` `remote` interface that integrator
  mods call into (`emit`, `register_source`, `get_event_id`); raises an `on_incoming`
  custom event that integrators subscribe to for game-side delivery
- **Baseline layer** — self-captures vanilla events (chat, join/leave, deaths, rockets,
  research) with no integrator present, so the bridge is useful standalone (self-host)
- Funnels both layers through one path: all events become structured JSONL in
  `script-output/bridge/events.jsonl`
- Ships a default `on_incoming` handler (print to all players); integrators may override
  with context-aware delivery (e.g. MTS routes to the right team chat)
- Maintains player Discord↔Factorio link state in `global`

**`bridge/`** — Go binary, one process per Factorio server
- Holds one Discord bot token, opens one gateway connection
- Reads events via pluggable Transport (Local / SFTP / SSH — see below)
- Sends inbound messages via RCON `remote.call`
- Exposes open HTTP Control API for portal/CLI config management
- Reads `bridge.yaml` for static config (self-host) or is driven via Control API (portal)

**`wizard/`** — Go library + standalone CLI
- Implements setup wizard state machine: token validation, install URL generation, guild
  polling, channel selection, round-trip test
- Importable as a Go package by any portal (AleForge embeds it)
- Ships as a standalone `bridge-wizard` CLI binary for self-hosters
- Same state machine, different UI shell — portal and CLI stay in sync automatically

### Data flows

**Game → Discord (baseline event):**
Mod's own `on_rocket_launched` hook → `vanilla.rocket_launched` → append JSONL → Transport
reads → bridge Router → Discord REST embed

**Game → Discord (integrator event):**
MTS calls `remote.call("open-discord-bridge-v1", "emit", { event = "mts.team_milestone", data = {...} })`
→ mod appends JSONL → bridge picks up → route config forwards to channel. The bridge never
references MTS; it routes on the `mts.*` namespace the integrator declared via
`register_source`.

**Discord → game:**
User types → Discord gateway → bridge → `remote.call("open-discord-bridge-v1", "incoming", {...})` via
RCON → mod raises `on_incoming` custom event + runs default handler → integrators that
subscribed (MTS) render context-aware delivery in-game

---

## Transport Modes

The bridge has a pluggable Transport layer. Config picks the mode. The rest of the bridge
(Router, Discord gateway, Control API) does not change. Phased in priority order.

### Phase 1 — Local (sidecar)

Bridge runs on the **same VPS** as the Factorio server. Reads JSONL by watching the file
directly via `inotify`. Writes inbound messages via loopback RCON.

```yaml
transport: local
factorio:
  rcon:
    address: 127.0.0.1:27015
    password_env: FACTORIO_RCON_PASSWORD
  events_file: /opt/factorio/script-output/bridge/events.jsonl
```

- **Latency:** <100ms (kernel inotify notification)
- **Overhead:** Near zero when idle; inotify sleeps until file changes
- **RCON:** Loopback only — never exposed to network
- **Best for:** Self-hosters; AleForge free-tier value-add on existing server VPS

### Phase 2 — SFTP

Bridge runs on **separate infra** from the Factorio server. Polls JSONL file via SFTP on
a configurable interval. Writes inbound messages via RCON (exposed on internal network,
or SSH-tunneled for public internet deployments).

```yaml
transport: sftp
factorio:
  rcon:
    address: game-vps-host:27015
    password_env: FACTORIO_RCON_PASSWORD
  sftp:
    host: game-vps-host:22
    user: factorio-bridge
    key_path: /secrets/bridge_ed25519
    events_file: /opt/factorio/script-output/bridge/events.jsonl
    poll_interval: 2s
```

- **Latency:** Up to poll interval (1–5s typical)
- **Overhead:** Constant polling round-trips even when server is idle
- **Best for:** AleForge addon-product model; one bridge serving multiple customer servers;
  customers who want to self-host the bridge against a remotely hosted Factorio server
- **Advantage over Local:** 1:N topology — one bridge instance can manage N of a
  customer's Factorio servers by polling each over SFTP

### Phase 3 — SSH Streaming

Bridge runs on **separate infra**. Opens a persistent SSH connection and streams the JSONL
file via `tail -F` running on the Factorio VPS. Push-based — zero overhead when idle.
Writes inbound messages via RCON (same as SFTP mode).

```yaml
transport: ssh
factorio:
  rcon:
    address: game-vps-host:27015
    password_env: FACTORIO_RCON_PASSWORD
  ssh:
    host: game-vps-host:22
    user: factorio-bridge
    key_path: /secrets/bridge_ed25519
    events_file: /opt/factorio/script-output/bridge/events.jsonl
    keepalive_interval: 30s
```

- **Latency:** <10ms on same datacenter LAN
- **Overhead:** Near zero when idle — `tail -F` sleeps in inotify on the remote host;
  bridge goroutine blocks on SSH stdout read; wakes only on actual events
- **Best for:** AleForge's bridge fleet at scale (50+ servers per bridge host)
- **Scale advantage:** At 200 servers, SFTP fires 200 polling loops/second regardless of
  activity. SSH streaming fires zero wakeups when all 200 servers are idle.

#### SSH security hardening

Dedicated low-privilege user on each Factorio VPS:

```bash
useradd --system --no-create-home --shell /sbin/nologin factorio-bridge
setfacl -m u:factorio-bridge:r /opt/factorio/script-output/bridge/
```

`authorized_keys` entry with forced command + restrictions:

```
restrict,from="10.0.1.0/24",command="tail -F -n 0 /opt/factorio/script-output/bridge/events.jsonl" ssh-ed25519 AAAA...
```

- `restrict` — disables port forwarding, agent forwarding, X11, PTY
- `command=` — server runs exactly this command regardless of what client requests;
  client cannot run anything else
- `from=` — key only valid from AleForge's bridge service network range; leaked key
  is useless from elsewhere
- `tail -F` (capital F) — re-opens file on truncation/rotation; handles game restarts

For multiple operations (if RCON is not used for inbound), replace `command=` with a
dispatcher script:

```bash
#!/bin/bash
# /usr/local/bin/factorio-bridge-shell
case "$SSH_ORIGINAL_COMMAND" in
    stream-events)  exec tail -F -n 0 /opt/factorio/script-output/bridge/events.jsonl ;;
    write-incoming) exec tee /opt/factorio/script-output/bridge/incoming.jsonl >/dev/null ;;
    *)              exit 1 ;;
esac
```

#### SSH reconnect logic

Simple retry loop with exponential backoff — roughly 30 lines:

```go
func streamEvents(addr string, cfg *ssh.ClientConfig, onEvent func(string)) {
    backoff := time.Second
    for {
        if err := stream(addr, cfg, onEvent); err != nil {
            log.Printf("disconnected: %v — reconnecting in %v", err, backoff)
            time.Sleep(backoff)
            if backoff < 30*time.Second {
                backoff *= 2
            }
        } else {
            backoff = time.Second
        }
    }
}

func stream(addr string, cfg *ssh.ClientConfig, onEvent func(string)) error {
    client, err := ssh.Dial("tcp", addr, cfg)
    if err != nil {
        return err
    }
    defer client.Close()

    session, err := client.NewSession()
    if err != nil {
        return err
    }
    defer session.Close()

    stdout, _ := session.StdoutPipe()
    session.Start("") // server ignores this; forced command runs

    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        onEvent(scanner.Text())
    }
    return scanner.Err()
}
```

On reconnect, `tail -F -n 0` skips history and picks up from current end of file.
No duplicate events. No offset tracking needed.

### Transport comparison

| | Local | SFTP | SSH Streaming |
|---|---|---|---|
| Latency | <100ms | 1–5s (poll interval) | <10ms |
| Idle CPU (bridge) | Near zero | Constant (timer loops) | Near zero |
| Idle CPU (game VPS) | Near zero | Handles stat requests | Near zero (inotify) |
| Idle network | None | Constant round-trips | Keepalives only (~1/min) |
| Topology | 1:1 (sidecar) | 1:N possible | 1:N possible |
| Scale to 500+ servers | N/A (one per VPS) | Polling overhead × N | Minimal — wakes on events |
| Reconnect complexity | N/A | Simple (next poll) | Simple retry loop (~30 LOC) |
| SSH key needed | No | Yes (SFTP) | Yes (restricted command) |
| Phase | 1 (MVP) | 2 | 3 |

---

## Sub-project Details

### `companion-mod/`

The mod is a two-layer event broker. The **substrate layer** is a frozen public
interface that integrator mods (MTS, OARC, …) call into; the **baseline layer** captures
vanilla events on its own so the bridge works with zero integrators. Both layers funnel
through one JSONL writer, so the bridge process sees a uniform stream.

Keep the substrate surface minimal — once published it is a frozen contract that other
mods hard-code against (the same discipline MTS applied to its own `mts-v1` interface).

#### Substrate layer — `open-discord-bridge-v1` interface (integrators call in)

```lua
-- Push an outbound event. Namespace the key by mod (mts.*, oarc.*).
remote.call("open-discord-bridge-v1", "emit", {
    event = "mts.team_milestone",
    data  = { team = "north", milestone = "first_rocket" }
})

-- Declare an event catalog so the control plane can render routable toggles
-- WITHOUT the bridge hardcoding any mod. Call once at init.
remote.call("open-discord-bridge-v1", "register_source", {
    namespace = "mts",
    events = {
        { key = "team_milestone",  description = "Team production milestone" },
        { key = "player_joined_team", description = "Player joined a team" },
    }
})

-- Get the event id for an inbound subscription (Factorio custom-event pattern)
local incoming_id = remote.call("open-discord-bridge-v1", "get_event_id", "on_incoming")
```

Integrators MUST guard calls so the bridge stays an optional dependency:

```lua
if remote.interfaces["open-discord-bridge-v1"] then
    remote.call("open-discord-bridge-v1", "emit", { event = "mts.team_milestone", data = {...} })
end
```

#### Inbound — `on_incoming` custom event (bridge raises, integrators subscribe)

The bridge process delivers Discord→game messages by calling `incoming` over RCON; the
mod raises `on_incoming` and runs its default handler. Integrators subscribe to override
delivery (MTS routes to the right team/surface chat):

```lua
-- Called by the bridge process via RCON (not by integrators)
remote.call("open-discord-bridge-v1", "incoming", { user, avatar_url, message, channel })
    -- internally: script.raise_event(on_incoming, {...}) + default "print to all" handler

-- Integrator subscribes for context-aware delivery
script.on_event(remote.call("open-discord-bridge-v1", "get_event_id", "on_incoming"), function(e)
    -- MTS: deliver e.message into the team chat for e's routed force
end)
```

#### Baseline layer — vanilla events (no integrator needed)

The mod hooks vanilla Factorio events itself and emits them under the `vanilla.*`
namespace through the same path, so a bare self-host install is useful immediately:

| Event key | Trigger | Key data |
|---|---|---|
| `vanilla.chat` | Player console chat | player, message |
| `vanilla.player_joined` | Player connects | player, online_count |
| `vanilla.player_left` | Player disconnects | player, reason, online_count |
| `vanilla.player_died` | Player death | player, cause, killer |
| `vanilla.rocket_launched` | Rocket launch | surface, rocket_name, flight_count |
| `vanilla.research_finished` | Tech completed | tech_name, level |
| `vanilla.game_started` | New game / load | map_name, mod_list |

Each JSONL line: `{"event": "<namespace>.<key>", "ts": <tick>, "surface": "...", "data": {...}}`

#### Player linking (Phase 2)

```lua
remote.call("open-discord-bridge-v1", "link_request",     { player_name })  → token string
remote.call("open-discord-bridge-v1", "confirm_link",     { player_name, discord_id })
local discord_id = remote.call("open-discord-bridge-v1", "linked_discord_id", player_name)
```

**Operational notes:**
- JSONL file is append-only; bridge tracks byte offset for crash-safe tailing (Local mode)
- Mod truncates JSONL on every game start; bridge detects truncation via byte-count reset
- All state (player links, registered sources, filter settings) stored in Factorio
  `global` — survives save/load
- `register_source` declarations are exposed via the bridge's Control API `/v1/status`
  so portals discover the live event catalog of whatever mods are running
- Mod refuses to load if Factorio API version is unsupported; prints clear error with
  link to compatibility matrix

### `bridge/`

Single Go binary. Config-driven. One process per Factorio server.

**Internal structure:**

```
bridge/
├── cmd/bridge/           main binary
├── internal/
│   ├── transport/
│   │   ├── local.go      inotify-based file tail
│   │   ├── sftp.go       SFTP polling
│   │   └── ssh.go        SSH streaming (tail -F)
│   ├── discord/          gateway client + REST
│   ├── rcon/             RCON client
│   ├── router/           event → channel mapping
│   └── controlapi/       HTTP server (/v1/*)
├── pkg/controlapi/spec/  OpenAPI YAML (the open spec)
└── bridge.yaml.example
```

**Control API endpoints (open spec, any portal can implement against):**

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/config` | Push new config (token, routing, transport) |
| `GET` | `/v1/config` | Current config (token redacted) |
| `GET` | `/v1/status` | Connection states, last event timestamp, mod version |
| `POST` | `/v1/restart` | Graceful restart |
| `POST` | `/v1/test` | Send round-trip test message |
| `GET` | `/v1/discord/guilds` | Proxy: list guilds the bot is in |
| `GET` | `/v1/discord/channels` | Proxy: list channels in a guild |

**Config shape:**

```yaml
factorio:
  rcon:
    address: 127.0.0.1:27015
    password_env: FACTORIO_RCON_PASSWORD
  required_mod_version: ">=1.0.0"   # bridge refuses to start if mod missing/outdated
  events_file: /opt/factorio/script-output/bridge/events.jsonl

transport: local    # local | sftp | ssh

# transport: sftp
# sftp:
#   host: host:22
#   user: factorio-bridge
#   key_path: /secrets/bridge_ed25519
#   poll_interval: 2s

# transport: ssh
# ssh:
#   host: host:22
#   user: factorio-bridge
#   key_path: /secrets/bridge_ed25519
#   keepalive_interval: 30s

discord:
  token_env: DISCORD_BOT_TOKEN
  guild_id: "123456789"
  routes:
    - source: chat
      channel_id: "987654321"
    - source: events.rocket_launched
      channel_id: "987654322"
      format: embed

control_api:
  enabled: false
  listen: 127.0.0.1:7777
  auth_token_env: BRIDGE_CONTROL_TOKEN
```

### `wizard/`

Go library + CLI binary for bot setup. AleForge embeds the library; self-hosters
use the CLI. Same state machine, different UI shell.

**Setup wizard steps:**

1. Detect if Bridge Mod is installed (poll for first JSONL write, or verify via RCON)
2. Link to Discord Developer Portal; wait for user to create application + bot
3. Accept bot token; call `GET /users/@me` to validate
4. Generate install URL with scopes/perms baked in; open in browser or display
5. Poll `GET /users/@me/guilds` until bot appears in target guild
6. Fetch guild channels; display picker
7. Accept channel mappings; push to bridge via `POST /v1/config`
8. Send round-trip test message; confirm both directions work
9. Save config (portal DB or `bridge.yaml`)

**Re-onboarding flow** (returning customer reusing existing bot):
- Wizard detects existing bot via pasted token
- Skips steps 2 and 4 (application already exists, bot already in guild)
- Drops to ~60 seconds total

---

## Security and Compliance

**Token storage:**
- AleForge portal: encrypted at rest in their secret manager; never logged
- Self-host: environment variable; never in `bridge.yaml`

**Logging hygiene:**
- Message content logged only at DEBUG level, off by default
- DEBUG logs never shipped to AleForge-level telemetry
- No message content in health checks, status endpoints, or error reports

**RCON:**
- Local mode: loopback only, never network-exposed
- Remote modes: RCON on internal AleForge network; or SSH-tunnel for public internet

**GDPR posture:**
- Per-customer compute → AleForge is processor on customer's behalf on isolated infra
- Bridge holds no persistent message store; in-memory routing buffers only
- SSH streaming mode: no file system access beyond the one JSONL file; enforced by
  filesystem ACL + SSH authorized_keys restrictions
- Self-host option available: customer can run bridge on their own hardware, AleForge
  has zero contact with their chat data

**No phone-home:**
- Bridge makes no connections to AleForge, upstream services, or telemetry endpoints
- Update checks are user-initiated only

---

## AleForge Integration Model

AleForge is one consumer of the open Control API. They build:
- Portal UI (closed source, their competitive surface)
- Auto-provisioning (Factorio server + Bridge Mod + bridge sidecar, all wired during
  server creation for Local mode; or bridge fleet + SFTP/SSH config for addon model)
- Token storage in their secret manager
- Customer-facing setup wizard UI wrapping the open `wizard/` library

AleForge does **not** fork the bridge. They run upstream releases. Improvements they
need go upstream first. They do not add proprietary Control API endpoints.

**Two billing models AleForge can offer:**

| Model | Transport | Billing | Benefit |
|---|---|---|---|
| Included (sidecar) | Local | Free with server plan | Zero extra setup, instant provisioning |
| Addon product | SFTP or SSH | Separate line item | One bridge serves customer's N servers |

---

## Phasing

### Phase 1 — MVP (target: 6–8 weeks part-time)

- `companion-mod` v0.1: baseline layer (`vanilla.*` chat, join/leave, player_died,
  rocket_launched, research_finished) + substrate layer (`open-discord-bridge-v1` `emit`,
  `register_source`, `on_incoming` event); accepts `incoming` RCON call. Ship one real
  integrator from day one — MTS pushing `mts.*` events — as the headline demo
  competitors can't show
- `bridge`: Local transport only; Discord gateway + REST; two slash commands (`/players`,
  `/status`); Control API stubbed (spec written, endpoints return 501)
- `wizard`: CLI wizard; Local-mode only (mod-install guidance + bot setup)
- Docker image with mod pre-installed; docker-compose example
- Mod Portal listing (reserve name now)
- Docs site with version compatibility matrix

**Validates:** end-to-end chat via mod path; self-host story; Mod Portal flow;
AleForge can begin portal work against the Control API spec.

### Phase 2 — SFTP transport + Control API

- `bridge`: SFTP transport; full Control API (`/v1/config`, `/v1/status`, `/v1/test`,
  `/v1/discord/*`)
- `wizard`: Control API integration (wizard drives bridge via API, not just config file)
- Player linking (`/link discord` flow): `link_request`, `confirm_link`, `linked_discord_id`
- Multi-channel routing + embed formatting, keyed on the `register_source` event catalog
- Control API surfaces the live event catalog (from `register_source`) so portals render
  per-server routable toggles without hardcoding any mod
- AleForge portal integration (embeds `wizard/` library, drives Control API)

### Phase 3 — SSH streaming transport

- `bridge`: SSH streaming transport with reconnect logic and security hardening
- SSH key generation in `wizard/` CLI and portal
- AleForge bridge fleet deployment (one bridge host serving N customer game VPSes)
- Multiple Factorio servers per bridge process (for addon-product topology)
- Web admin UI for self-hosters (community contribution welcome)
- Additional chat platforms (Slack, Matrix — community-driven)

---

## Open Questions

1. **Mod Portal name** — reserve early; carries the brand permanently. Should be short
   and not collide with existing community bridges.

2. **License** — RESOLVED 2026-08-31: **MIT**. Apache 2.0 was the early lean, for its
   patent grant around the AleForge integration, but the mod portal had listed this mod
   as MIT since first publish and the whole family standardised on MIT. LICENSE, README,
   the portal metadata and the control-API spec all say MIT.

3. **Mod-bridge version compatibility handshake** — bridge checks mod version on startup
   via RCON and refuses if below `required_mod_version`. Mismatches must surface clearly
   in logs and the Control API `/v1/status` response, not silently fail.

4. **JSONL truncation vs rotation** — current plan: mod truncates on game start (simple,
   avoids unbounded growth). If anyone wants the file as a persistent audit log, rotation
   is an option. Decide before cutting the mod's first stable API.

5. **SFTP vs RCON for inbound in remote modes** — current plan: RCON for all inbound
   (both SFTP and SSH modes). If RCON is not available (customer's host blocks it),
   the SSH dispatcher script can handle inbound via a write-incoming channel. Worth
   designing the SSH dispatcher script as Phase 3 standard even if not needed initially.

6. **AleForge billing model** — sidecar-included vs addon-product changes the provisioning
   flow significantly. Worth aligning with AleForge on which they want to ship first
   before writing the portal integration.

7. **Multi-server per bridge process** — current plan is 1:1 (one bridge per Factorio
   server). SFTP and SSH modes naturally support 1:N (one bridge polls/streams N servers).
   The bridge should be designed with this in mind from Phase 2 even if the UI only
   exposes 1:1 initially.
