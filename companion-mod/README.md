# Open Discord Bridge — Companion Mod

Mirror your Factorio server's chat and events to a Discord channel, and relay Discord
messages back into the game. Works on a vanilla server, and lets other mods
(multi-team-support, OARC, …) post their own events too.

> ### ⚠️ This mod needs a companion program to actually reach Discord.
> On its own the mod just records events to a file. The piece that connects to Discord is
> the **Open Discord Bridge** program, which you run alongside your server. Downloads,
> a one-command installer, a setup wizard, and full instructions for every hosting style
> (self-host, Docker, or a game-host panel like Pterodactyl) are on GitHub:
>
> # → https://github.com/bits-orio/open-discord-bridge

## Setup in three steps

1. **Install this mod** (you've done this — or get it from the in-game mod browser).
2. **Enable RCON** on your server — the bridge uses it to deliver Discord messages in-game.
   The repo shows exactly how for each hosting style.
3. **Run the bridge** from [the GitHub repo](https://github.com/bits-orio/open-discord-bridge)
   and point it at your server. Its setup wizard handles the Discord bot token and channel.

Once it's connected you'll see in-game chat appear in Discord and vice versa. Everything
else — commands, player linking, deployment options — is documented in the repo.

---

## For mod authors — integrator API (`open-discord-bridge-v1`)

Other mods push their own events into the bridge through a frozen remote interface; the
bridge itself contains no mod-specific code. Always guard calls so the bridge stays an
*optional* dependency of your mod:

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

### JSONL line shape

The mod writes events to `script-output/open-discord-bridge/events.jsonl`; the bridge tails
it:

```json
{"event":"vanilla.chat","ts":1234,"surface":"nauvis","data":{"player":"Bob","message":"hi"}}
```

### Inbound (Discord → game)

The bridge delivers Discord messages by running the console command
`/odb-incoming {"user":"…","message":"…"}` over RCON (achievement-safe — no
`/silent-command`). The mod parses it, raises `on_incoming` for integrators, and prints a
default `[Discord] user: message` line to all players.

> **Integrator security note:** the `on_incoming` event carries the **raw** Discord
> `user` and `message` (integrators that parse commands need exact content). If your mod
> prints either into game chat, strip newlines/control characters first, or Discord users
> can forge extra chat lines (e.g. `"hi\nBob: gg"` renders a fake line as Bob) through
> your mod. Factorio [rich text](https://wiki.factorio.com/rich_text) tags are a supported
> feature — no need to strip those. The mod's own default delivery already does this.

See the [GitHub repo](https://github.com/bits-orio/open-discord-bridge) for the full design
and deployment docs.
