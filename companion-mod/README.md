# Open Discord Bridge — Companion Mod

Required Factorio 2.0 mod. It does two things:

1. **Baseline** — captures vanilla events (chat, join/leave, deaths, rockets, research)
   on its own and writes them as JSONL to
   `script-output/open-discord-bridge/events.jsonl`. The bridge process tails that file.
2. **Substrate** — exposes the frozen `open-discord-bridge-v1` remote interface so other
   mods (MTS, OARC, …) can push their own events and receive inbound Discord messages.
   The bridge never contains mod-specific code.

See [../PLAN.md](../PLAN.md) for full design.

## Install

Copy this folder into your Factorio `mods/` directory as `open-discord-bridge_0.1.0`
(the folder/zip name must be `name_version` from `info.json`):

```
mods/open-discord-bridge_0.1.0/
  info.json
  control.lua
```

Restart the server. On the first tick of a session the mod writes a `vanilla.game_started`
line, which also creates the events file.

## Integrator API (`open-discord-bridge-v1`)

Always guard calls so the bridge stays an *optional* dependency of your mod:

```lua
if remote.interfaces["open-discord-bridge-v1"] then
  -- Push an outbound event (namespace the key by your mod).
  --   text   — the human sentence the bridge shows (falls back to a key=value summary).
  --            You can use Discord markdown (**bold**, `code`, …) — but it won't render
  --            inside the embed-mode ANSI block.
  --   label  — optional: overrides the event-name in the bridge's "[ns → …]" tag, e.g.
  --            label="🔬" renders "[mts → 🔬]".
  --
  -- Emoji in `label` (and `text`) — read this, custom emoji are fussy:
  --   • A bare :shortcode: (e.g. ":lab:") will NOT render from a bot — it shows literally.
  --   • Unicode emoji (🔬, 🚀, …) always work, everywhere, with no setup.
  --   • A CUSTOM server emoji must be written in its raw form <:name:id> AND must live on a
  --     server the bot is a member of (typically your own). Emoji from a server the bot is
  --     NOT in (e.g. the official Factorio server's :lab:) will NOT render — Discord shows
  --     ":lab:". No bot permission enables that; it's server membership, not a permission.
  --   • Same-server custom emoji need NO special permission (not even Use External Emoji).
  --   • To get the raw <:name:id>: in any Discord channel type a backslash first — "\:lab:"
  --     — and send it; Discord posts "<:lab:123456789>". Copy that as the label value.
  remote.call("open-discord-bridge-v1", "emit", {
    event = "mts.team_milestone",
    data  = { team = "north", milestone = "first_rocket",
              label = "🚀", text = "north launched its **first rocket**" },
  })

  -- Declare your event catalog once at init (lets portals offer routable toggles).
  remote.call("open-discord-bridge-v1", "register_source", {
    namespace = "mts",
    events = { { key = "team_milestone", description = "Team production milestone" } },
  })

  -- Take over a built-in baseline event: disable it here and emit your own richer
  -- version (e.g. MTS owns research so it can include the team).
  remote.call("open-discord-bridge-v1", "set_baseline", { event = "research_finished", enabled = false })
end

-- Chat vs notable events: name a chat-style relay event "<namespace>.chat" (e.g.
-- "mts.chat"). The bridge renders any "chat"/"*.chat" event as plain chat text; notable
-- events (e.g. "mts.team_created") get the bolded "[ns → …]" tag, and with decoration on
-- (embed:true) that tag is ANSI-colored per event type.

-- Subscribe to inbound Discord messages for context-aware delivery.
script.on_event(
  remote.call("open-discord-bridge-v1", "get_event_id", "on_incoming"),
  function(e) --[[ e.user, e.message, e.channel ]] end
)
```

## JSONL line shape

```json
{"event":"vanilla.chat","ts":1234,"surface":"nauvis","data":{"player":"Bob","message":"hi"}}
```

## Inbound (Discord → game)

The bridge delivers Discord messages by running the console command
`/odb-incoming {"user":"…","message":"…"}` over RCON (achievement-safe — no
`/silent-command`). The mod parses it, raises `on_incoming` for integrators, and prints a
default `[Discord] user: message` line to all players.
