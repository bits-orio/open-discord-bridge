# Open Discord Bridge — Setup Wizard

Go library + standalone CLI for guided setup: validate the bot token, generate the invite
URL, pick a guild and channel, and write `bridge.yaml` + `.env`. Any portal can embed the
library (`package wizard`); self-hosters run the CLI.

See [../PLAN.md](../PLAN.md) for full design.

## CLI

```sh
cd wizard
go build -o odb-wizard ./cmd/wizard
./odb-wizard --out ../bridge        # writes ../bridge/bridge.yaml and ../bridge/.env
```

It will:
1. Take your bot token (prompt, or `DISCORD_BOT_TOKEN` env) and validate it against Discord.
2. Print the invite URL — open it, add the bot to your server.
3. Let you pick the server and a text channel.
4. Ask for the Factorio RCON address, events-file path, and RCON password.
5. Write a minimal `bridge.yaml` (catch-all route to your channel, a `!players` command)
   and a `.env` with the secrets.

Then install the companion mod and run `./start-all.sh` (or `./start-bridge.sh`).

## Library

```go
bot, err := wizard.Connect(token)   // validates the token (REST, no gateway)
url := bot.InviteURL()              // OAuth2 invite URL with the needed permissions
guilds, _ := bot.Guilds()           // servers the bot is in
channels, _ := bot.TextChannels(guildID)
yaml, _ := wizard.RenderBridgeYAML(wizard.ConfigParams{...})
```
