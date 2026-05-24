# Open Discord Bridge — Bridge Process

Go binary. One process per Factorio server. Reads game events from the companion mod's
JSONL file and relays them to Discord; relays Discord messages back into the game over
RCON.

> **MVP status:** Local transport only (sidecar — bridge runs on the same host as the
> Factorio server). SFTP and SSH streaming are later phases. See [../PLAN.md](../PLAN.md).

## Build

Needs Go 1.23+.

```sh
cd bridge
go build -o odb-bridge ./cmd/bridge
```

## Configure

```sh
cp bridge.yaml.example bridge.yaml   # edit channel IDs + paths
cp .env.example .env                 # put your real token + RCON password here
```

- `factorio.events_file` → the mod's JSONL, e.g.
  `<factorio>/script-output/open-discord-bridge/events.jsonl`
- `discord.routes` → which event maps to which channel. First match wins; patterns are
  exact (`vanilla.chat`), namespace globs (`mts.*`), or catch-all (`*`). Every channel
  listed also accepts Discord messages back into the game.

Secrets are read from the env vars named in `bridge.yaml` (`token_env`,
`rcon.password_env`) — never put them in the YAML.

## Run

```sh
set -a; . ./.env; set +a      # load DISCORD_BOT_TOKEN + FACTORIO_RCON_PASSWORD
./odb-bridge -config bridge.yaml
```

A successful start logs `connected to Discord; tailing <events_file>`.

## End-to-end MVP checklist

1. **Discord bot:** create an application at <https://discord.com/developers/applications>,
   add a bot, enable the **Message Content Intent**, copy the token into `.env`. Invite it
   with scopes `bot` + `applications.commands`, and permissions View Channels, Send
   Messages, Read Message History, and (for marking linked players) Manage Roles + Manage
   Nicknames.
2. **Factorio server:** install the companion mod (see
   [../companion-mod/README.md](../companion-mod/README.md)) and launch headless with RCON:
   ```
   factorio --start-server save.zip --rcon-port 27015 --rcon-password "$FACTORIO_RCON_PASSWORD"
   ```
3. **Bridge:** point `events_file` at that server's `script-output/...`, set channel IDs,
   run the binary.
4. **Test outbound:** type in the game chat → it appears in the Discord channel.
5. **Test inbound:** type in the Discord channel → it appears in-game as
   `[Discord] you: message`.
6. **Bonus:** type `!players` in Discord → the bridge runs `/players online` over RCON and
   replies with the online list.

## Docker

A multi-stage `Dockerfile` builds a ~16 MB image (static binary on Alpine, non-root).
The repo-root `docker-compose.yml` is a full-stack example: a headless Factorio server
with the companion mod mounted, plus the bridge sidecar sharing a volume for the events
file and reaching RCON over the compose network.

```sh
cp .env.example .env                       # set DISCORD_BOT_TOKEN, FACTORIO_RCON_PASSWORD, BRIDGE_CONTROL_TOKEN
mkdir -p .run && printf '%s' "$FACTORIO_RCON_PASSWORD" > .run/rconpw   # factoriotools reads this
# edit bridge/bridge.docker.yaml -> set the channel_id
docker compose up --build
```

The bridge reads `bridge/bridge.docker.yaml` (mounted), which points RCON at the
`factorio` service and the events file at the shared `/factorio/script-output/...`. The
Control API is published on `127.0.0.1:7777`. `restart: unless-stopped` is also what makes
`POST /v1/restart` round-trip.

**Bridge-only variant** (Factorio already running on the host): drop the `factorio`
service, set `factorio.rcon.address: host.docker.internal:27015` in `bridge.docker.yaml`,
and bind-mount the host's `script-output` into the bridge container.

Build just the image:
```sh
docker build -t open-discord-bridge:latest ./bridge
```

### Standalone container (panels like Pterodactyl)

`docker-compose.yml` is a **self-host convenience**. Panels that run each process as its
own container (Pterodactyl/Wings) instead manage the bridge as an individual container:

```sh
docker run -d --name odb-bridge \
  -e DISCORD_BOT_TOKEN=... -e FACTORIO_RCON_PASSWORD=... -e BRIDGE_CONTROL_TOKEN=... \
  -p 127.0.0.1:7777:7777 \
  -v /path/to/bridge.yaml:/etc/open-discord-bridge/bridge.yaml:ro \
  open-discord-bridge:latest
```

The container is panel-friendly by design: it logs to stdout, stops cleanly on `SIGTERM`,
and `POST /v1/restart` is a graceful exit so the panel restarts it.

Because the bridge and Factorio are then **separate, isolated containers**:
- **RCON:** set `factorio.rcon.address` to the Factorio container's network address (its
  allocation / internal DNS name), not `127.0.0.1`.
- **Events file:** the bridge needs a path to the mod's `events.jsonl`. Either bind-mount
  the Factorio server's data dir into the bridge container read-only (Local transport), or
  use a remote transport (SFTP — planned, fits Pterodactyl's per-server SFTP). This choice
  is deployment-specific.

## Layout

```
cmd/bridge/         main: config load + wiring + event formatting
internal/config/    YAML config + env-resolved secrets + validation
internal/transport/ local.go — polling JSONL tailer (truncation-aware)
internal/rcon/      reconnecting Factorio RCON client
internal/discord/   discordgo gateway + REST, inbound message handler
internal/router/    event-key → channel matching
internal/controlapi/ HTTP Control API (/v1/*); spec in pkg/controlapi/spec/
```
