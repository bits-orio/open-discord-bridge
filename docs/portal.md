# Open Discord Bridge

> Your server's chat and events, in your Discord channel. And back again.

[![Discord](https://img.shields.io/badge/Discord-join%20the%20server-5865F2?logo=discord&logoColor=white)](https://discord.gg/tWz4FT74pH) [![GitHub](https://img.shields.io/badge/GitHub-source-181717?logo=github&logoColor=white)](https://github.com/bits-orio/open-discord-bridge)

Mirror your Factorio server's chat and events to a Discord channel — and relay Discord messages back into the game. Two-way, in real time, achievement-safe.

It works on a plain vanilla server with no other mods installed, and it contains no mod-specific code: other mods push their own events *into* it through a public interface.

## Hosted on AleForge — first-class integration

Playing on [AleForge](https://aleforge.net/factorio/)? The bridge is built into their panel — toggle it on, paste your bot token and channel ID, reinstall, done. Follow the illustrated [10–15 minute setup guide](https://github.com/bits-orio/open-discord-bridge/blob/main/docs/aleforge/SETUP.md).

AleForge was the first host to ship it, and others are welcome. **If you run a hosting panel and want the bridge in it, come talk to me on [Discord](https://discord.gg/tWz4FT74pH)** — the integration is documented end to end, and I will help you land it.

## This mod needs a small companion program

On its own, the mod records events to a file. The piece that talks to Discord is the **bridge** — a single small program you run next to your server. No database, no cloud service, and your bot token stays with you.

Running on public servers today. On AleForge the bridge is already installed; everywhere else you run one binary.

## Quick start

1. Install the mod on your Factorio server.
2. Create a Discord bot and invite it to your channel.
3. Run the bridge next to your server — the [setup wizard](https://github.com/bits-orio/open-discord-bridge/blob/main/QUICKSTART.md) walks you through it, or flip the toggle in the AleForge panel.
4. Paste your bot token and channel ID.
5. Say something in game and watch it land in Discord.

## What you get out of the box

- **Two-way chat** — game chat appears in Discord; Discord messages appear in-game as `[Discord] name: message`. Factorio rich text (`[item=...]`, `[color=...]`) typed in Discord renders properly in game chat.
- **Server events** — joins and leaves, deaths, research, rocket launches (rate-limited so a late-game base doesn't flood your channel), server up/down.
- **Discord commands with zero setup** — `!players`, plus full account linking: `!link CODE` / `!unlink`, and admin-only `!links`, `!unlink-player`, `!unlink-all` ("admin" = Discord's Administrator permission by default). Define your own custom commands too, optionally as native slash commands.
- **Player linking** — players connect their Discord and Factorio identities in-game with `/odb-link`; optionally give linked players a Discord role and synced nickname.
- **Mod events flow through automatically** — mods that integrate with the bridge (e.g. [Multi-Team Support](https://mods.factorio.com/mod/multi-team-support)) post their own team-aware events with no extra setup, routable to separate channels.

## Built to be trusted with your server

- In-game players can't ping `@everyone`, `@here`, or roles from game chat — mention parsing is off by default and only deliberate features (like linked-player mentions) can ping.
- Discord users can't forge fake chat lines in-game — newlines are stripped before printing (rich text stays; it's a feature).
- Secrets never live in config files — the bot token and RCON password come from environment variables only.
- When something's misconfigured, the bridge writes a `bridge.effective.yaml` snapshot showing exactly what settings it resolved and which secret is missing — debugging takes minutes, not days.

## Hosting options

- **[AleForge](https://aleforge.net/factorio/)** — first-class panel integration (guide above). The first host to ship it; others will follow.
- **Self-host** — download the binary, run the [setup wizard](https://github.com/bits-orio/open-discord-bridge/blob/main/QUICKSTART.md).
- **Docker / Pterodactyl panels** — images on GHCR, a reference egg, and an env-var-only config mode built for panels: [deployment guide](https://github.com/bits-orio/open-discord-bridge/blob/main/DEPLOYMENT.md).

## For mod authors

Push your own events into the bridge through the frozen `open-discord-bridge-v1` remote interface — namespaced events, custom labels, channel routing, inbound Discord message subscription. The bridge contains zero mod-specific code, so your mod integrates *into* it, not the other way around. [API docs and copy-paste patterns](https://github.com/bits-orio/open-discord-bridge/blob/main/companion-mod/README.md).

Already integrated: [Multi-Team Support](https://mods.factorio.com/mod/multi-team-support) and OARC.

## Links

- [Source on GitHub](https://github.com/bits-orio/open-discord-bridge)
- [Report a bug](https://github.com/bits-orio/open-discord-bridge/issues)
- [Community Discord](https://discord.gg/tWz4FT74pH)

## Development

Developed with AI coding assistants alongside human review and in-game testing. Issues and pull requests are welcome on [GitHub](https://github.com/bits-orio/open-discord-bridge).

License: MIT
