# Open Discord Bridge

Mirror your Factorio server's chat and events to a Discord channel — and relay Discord
messages back into the game. Two-way, in real time, achievement-safe.

[![Discord](https://img.shields.io/badge/Discord-join%20the%20server-5865F2?logo=discord&logoColor=white)](https://discord.gg/tWz4FT74pH)

![In-game chat and events flowing into a Discord channel](https://raw.githubusercontent.com/bits-orio/open-discord-bridge/main/docs/aleforge/images/18-verify-game-to-discord.png)

## ⚠️ This mod needs a small companion program

On its own, the mod records events to a file. The piece that talks to Discord is the
**bridge** — a single small program you run next to your server (no database, no cloud
service, your bot token stays with you). Downloads, a setup wizard, and instructions for
every hosting style are on GitHub:

→ https://github.com/bits-orio/open-discord-bridge

**Playing on [AleForge](https://aleforge.net/factorio/)?** The bridge is built into their
panel — toggle it on, paste your bot token and channel ID, reinstall, done. Follow the
illustrated 10–15 minute guide:
https://github.com/bits-orio/open-discord-bridge/blob/main/docs/aleforge/SETUP.md

## What you get out of the box

- **Two-way chat** — game chat appears in Discord; Discord messages appear in-game as
  `[Discord] name: message`. Factorio rich text (`[item=...]`, `[color=...]`) typed in
  Discord renders properly in game chat.
- **Server events** — joins and leaves, deaths, research, rocket launches (rate-limited so
  a late-game base doesn't flood your channel), server up/down.
- **Discord commands with zero setup** — `!players`, plus full account linking:
  `!link CODE` / `!unlink`, and admin-only `!links`, `!unlink-player`, `!unlink-all`
  ("admin" = Discord's Administrator permission by default). Define your own custom
  commands too, optionally as native slash commands.
- **Player linking** — players connect their Discord and Factorio identities in-game with
  `/odb-link`; optionally give linked players a Discord role and synced nickname.
- **Mod events flow through automatically** — mods that integrate with the bridge (e.g.
  Multi-Team Support) post their own team-aware events with no extra setup, routable to
  separate channels.

## Built to be trusted with your server

- In-game players can't ping `@everyone`, `@here`, or roles from game chat — mention
  parsing is off by default and only deliberate features (like linked-player mentions)
  can ping.
- Discord users can't forge fake chat lines in-game — newlines are stripped before
  printing (rich text stays; it's a feature).
- Secrets never live in config files — the bot token and RCON password come from
  environment variables only.
- When something's misconfigured, the bridge writes a `bridge.effective.yaml` snapshot
  showing exactly what settings it resolved and which secret is missing — debugging takes
  minutes, not days.

## Hosting options

- **[AleForge](https://aleforge.net/factorio/)** — first-class panel integration (guide above). The first host to ship it;
  others will follow.
- **Self-host** — download the binary, run the setup wizard:
  https://github.com/bits-orio/open-discord-bridge/blob/main/QUICKSTART.md
- **Docker / Pterodactyl panels** — images on GHCR, a reference egg, and an env-var-only
  config mode built for panels:
  https://github.com/bits-orio/open-discord-bridge/blob/main/DEPLOYMENT.md

## For mod authors

Push your own events into the bridge through the frozen `open-discord-bridge-v1` remote
interface (namespaced events, custom labels, channel routing, inbound Discord message
subscription) — the bridge contains zero mod-specific code, so your mod integrates *into*
it, not the other way around. API docs and copy-paste patterns:
https://github.com/bits-orio/open-discord-bridge/blob/main/companion-mod/README.md

---

Join the Discord: https://discord.gg/tWz4FT74pH

Bug reports and feature requests: https://github.com/bits-orio/open-discord-bridge/issues
