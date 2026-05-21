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
   to your server with `bot` scope + Send/Read Messages.
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

## Layout

```
cmd/bridge/         main: config load + wiring + event formatting
internal/config/    YAML config + env-resolved secrets + validation
internal/transport/ local.go — polling JSONL tailer (truncation-aware)
internal/rcon/      reconnecting Factorio RCON client
internal/discord/   discordgo gateway + REST, inbound message handler
internal/router/    event-key → channel matching
```
