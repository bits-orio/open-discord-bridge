# Quick Start — self-hosted (downloaded binary)

This guide takes you from zero to a working bridge using the **published binary** — no
building, no `git clone`. It's for people running their **own** Factorio server.

> On a managed host (Pterodactyl, AleForge, …)? You don't create a config file at all —
> your panel injects configuration as environment variables. Skip this guide and see the
> "Env-var mode" section of [DEPLOYMENT.md](DEPLOYMENT.md).

**You'll need:** a Factorio headless/dedicated server you control, and a Discord account
with permission to add a bot to your server.

---

## 1. Create the Discord bot

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications) →
   **New Application** → name it → **Bot** (left sidebar).
2. **Reset Token** → copy the token somewhere safe. This is `DISCORD_BOT_TOKEN`.
3. Scroll to **Privileged Gateway Intents** and turn on **Message Content Intent**.
   ⚠️ The bridge can't read channel messages without this — it's the most common thing
   people miss.

(You'll add the bot to your server in step 4 — the wizard prints the invite link for you.)

---

## 2. Download the files

From the [Releases page](https://github.com/bits-orio/open-discord-bridge/releases), grab
the assets for your OS/arch:

| File | What it's for |
|---|---|
| `odb-bridge-<os>-<arch>` | the bridge itself (**required**) |
| `odb-wizard-<os>-<arch>` | interactive setup helper (**recommended**) |
| `open-discord-bridge_<version>.zip` | the companion mod (**required**) |
| `bridge.yaml.example` | config template (only if you configure by hand) |

On Linux/macOS, rename and mark the binaries executable:

```sh
mv odb-bridge-linux-amd64 odb-bridge
mv odb-wizard-linux-amd64 odb-wizard
chmod +x odb-bridge odb-wizard
```

---

## 3. Install the companion mod

The mod runs **inside Factorio** — it writes the events the bridge reads and adds the
`/odb-*` commands. Either:

- **Drop the zip in:** copy `open-discord-bridge_<version>.zip` (do **not** unzip it) into
  your server's `mods/` folder, **or**
- **In-game mod browser:** install *open-discord-bridge* from the
  [Mod Portal](https://mods.factorio.com/mod/open-discord-bridge).

Then restart the server so it loads the mod.

---

## 4. Enable RCON on your Factorio server

The bridge delivers Discord → game over RCON, so your server must have it on. Start it with
an RCON port and password, e.g.:

```sh
factorio --start-server my-save.zip \
  --rcon-port 27015 --rcon-password "choose-a-password"
```

Note the **port** and **password** — you'll give them to the bridge next.

---

## 5. Configure the bridge

Pick **one** of these.

### Option A — the wizard (recommended)

```sh
./odb-wizard -out .
```

It will: validate your token, print an **invite link** (open it, pick your server,
Authorize), let you pick the server and channel, then ask for the RCON address/password and
the events-file path. When it finishes you'll have a `bridge.yaml` and a `.env` in the
current folder.

### Option B — by hand

```sh
cp bridge.yaml.example bridge.yaml
```

Open `bridge.yaml` and set three things (every key is documented inline):

- `factorio.rcon.address` — `127.0.0.1:27015` (the RCON port from step 4)
- `factorio.events_file` — your server's
  `…/script-output/open-discord-bridge/events.jsonl`. If you're unsure where `script-output`
  is, it sits next to your server's `saves/`/`mods/` data dir.
- `discord.routes[].channel_id` — right-click the target Discord channel → **Copy Channel
  ID** (enable Developer Mode in Discord settings first).

Then create a `.env` file next to it for the **secrets** (never put these in the YAML):

```
DISCORD_BOT_TOKEN=your-bot-token
FACTORIO_RCON_PASSWORD=the-password-from-step-4
```

Invite the bot manually if you skipped the wizard: in the Developer Portal →
**Installation** (or OAuth2 URL Generator), scope `bot` + `applications.commands`,
permissions *View Channels, Send Messages, Read Message History*.

---

## 6. Run the bridge

The bridge reads secrets from the environment and **does not auto-load `.env`**, so source
it first:

```sh
set -a; . ./.env; set +a
./odb-bridge -config bridge.yaml
```

You should see `bridge: connected to Discord; tailing …`. (Leave it running; use a service
manager like `systemd` to keep it up — it stops cleanly on Ctrl-C / SIGTERM.)

---

## 7. Verify it works

- Type in your **Factorio** server chat → it appears in the Discord channel.
- Type in the **Discord** channel → it appears in-game.

That's the whole loop. If either direction is silent, check the bridge's log output — it
runs a startup permission preflight and tells you exactly what's missing (e.g. a Discord
permission, or RCON not reachable).

---

## Where to go next

- **Custom `!commands`, player linking, admin gating, slash commands, channel routing** —
  all configured in `bridge.yaml`. Every option is documented in `bridge.yaml.example` and
  in [DEPLOYMENT.md](DEPLOYMENT.md) §2.
- **Factorio on a different machine than the bridge** — use the SFTP transport; see
  [DEPLOYMENT.md](DEPLOYMENT.md) §3.
- **Secrets** always live in `.env` / environment variables, never in `bridge.yaml`.
