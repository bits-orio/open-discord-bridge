-- Open Discord Bridge — companion mod
--
-- Two layers:
--   1. Baseline   — captures vanilla events (chat, join/leave, deaths, rockets,
--                   research) on its own, so the bridge works with no integrators.
--   2. Substrate  — owns the frozen `open-discord-bridge-v1` remote interface that
--                   integrator mods (MTS, OARC, ...) call into to emit events and
--                   subscribe to inbound Discord messages.
--
-- All events funnel through one JSONL writer at
-- script-output/open-discord-bridge/events.jsonl, which the bridge process tails.

local INTERFACE   = "open-discord-bridge-v1"
local EVENTS_FILE = "open-discord-bridge/events.jsonl"

-- Custom event id for inbound Discord messages. Generated at module-load time, which
-- runs every session (both on_init and on save load), and kept in a Lua local — NOT in
-- storage. A generate_event_name() id is only valid to raise in the session that
-- generated it, so persisting it and skipping regeneration after a load leaves a stale,
-- invalid id (raise_event then errors and crashes the server). The integer may differ
-- across sessions, so integrators must resolve it via get_event_id, never hardcode it.
-- (Mirrors the mts-v1 pattern: generate at load, keep in a Lua local, expose the id
-- through the remote interface.)
local on_incoming_event = script.generate_event_name()

-- Truncate the JSONL once per game session, then append. This is a session-scoped
-- Lua local (NOT in storage): it resets to false on every load, so the first event
-- after a new game or a load overwrites the stale file and the bridge sees a fresh
-- byte-count it can detect as a truncation.
local truncated_this_session = false

local function write_event(event_key, data, surface_name)
  local line = helpers.table_to_json({
    event   = event_key,
    ts      = game.tick,
    surface = surface_name,
    data    = data or {},
  }) .. "\n"
  -- append == truncated_this_session: first write of the session passes append=false
  -- (overwrite), every later write passes append=true.
  helpers.write_file(EVENTS_FILE, line, truncated_this_session)
  truncated_this_session = true
end

-- ─── Inbound (Discord → game) ────────────────────────────────────────────────
-- The bridge process delivers messages by running the /odb-incoming console command
-- over RCON (achievement-safe; no /silent-command). We raise our own custom event so
-- integrators can override delivery, then run a default "print to all" handler.

local function handle_incoming(args)
  if type(args) ~= "table" then return end

  script.raise_event(on_incoming_event, {
    user       = args.user,
    user_id    = args.user_id,
    message    = args.message,
    channel    = args.channel,
    avatar_url = args.avatar_url,
  })

  -- Default delivery. Integrators that subscribe to on_incoming may do their own
  -- (e.g. MTS routing into a specific team's chat) in addition to this.
  local user = args.user or "Discord"
  local msg  = args.message or ""

  -- If this Discord user is linked to a player, tint their name with the player's
  -- in-game chat color so it reads like that player speaking.
  local name = user
  if args.user_id and storage.odb and storage.odb.links then
    for player_name, link in pairs(storage.odb.links) do
      if link.discord_id == args.user_id then
        local p = game.get_player(player_name)
        if p and p.valid then
          local c = p.chat_color
          name = string.format("[color=%.3f,%.3f,%.3f]%s[/color]", c.r, c.g, c.b, user)
        end
        break
      end
    end
  end

  -- The blurple [Discord] tag keeps Discord messages visually distinct from in-game chat.
  game.print(string.format("[color=114,137,218][Discord][/color] %s: %s", name, msg))
end

-- ─── Storage / custom event id ───────────────────────────────────────────────

local function ensure_storage()
  storage.odb              = storage.odb or {}
  storage.odb.sources      = storage.odb.sources or {}
  storage.odb.links        = storage.odb.links or {}   -- player_name -> { discord_id, discord_name }
  storage.odb.pending      = storage.odb.pending or {} -- link code -> { player, expires }
  storage.odb.link_counter = storage.odb.link_counter or 0
end

script.on_init(ensure_storage)
script.on_configuration_changed(ensure_storage)

-- ─── Substrate interface: open-discord-bridge-v1 ─────────────────────────────

remote.add_interface(INTERFACE, {
  -- Push an outbound event. Namespace the key by mod, e.g. "mts.team_milestone".
  emit = function(args)
    if type(args) ~= "table" or type(args.event) ~= "string" then return end
    write_event(args.event, args.data, args.surface)
  end,

  -- Declare an event catalog so the bridge / control plane can offer routable
  -- toggles without hardcoding any mod. Call once at init.
  register_source = function(args)
    if type(args) ~= "table" or type(args.namespace) ~= "string" then return end
    storage.odb.sources[args.namespace] = args.events or {}
  end,

  -- Return a custom event id for subscription (Factorio custom-event pattern).
  get_event_id = function(name)
    if name == "on_incoming" then
      return on_incoming_event
    end
    return nil
  end,

  -- Called by the bridge process over RCON (also callable by other mods).
  incoming = function(args)
    handle_incoming(args)
  end,

  -- Returns the linked Discord user ID for a player, or nil.
  linked_discord_id = function(player_name)
    local link = storage.odb.links and storage.odb.links[player_name]
    return link and link.discord_id or nil
  end,
})

-- ─── Inbound RCON command ────────────────────────────────────────────────────
-- The bridge sends: /odb-incoming {"user":"Bob","message":"hello"}
-- Registered as a console command so it is achievement-safe over RCON.

commands.add_command("odb-incoming", "Open Discord Bridge: inject an incoming Discord message (RCON)", function(cmd)
  if cmd.player_index then return end -- RCON / server only
  if not cmd.parameter or cmd.parameter == "" then return end
  local ok, args = pcall(helpers.json_to_table, cmd.parameter)
  if ok and type(args) == "table" then
    handle_incoming(args)
  end
end)

-- Report mod status as JSON for the bridge's version handshake and Control API
-- /v1/status. The bridge runs this over RCON.
commands.add_command("odb-status", "Open Discord Bridge: report mod status as JSON (RCON)", function(cmd)
  if cmd.player_index then return end -- RCON / server only
  rcon.print(helpers.table_to_json({
    mod_version = script.active_mods["open-discord-bridge"],
    interface   = INTERFACE,
    sources     = (storage.odb and storage.odb.sources) or {},
  }))
end)

-- ─── Player linking (Discord ↔ Factorio) ─────────────────────────────────────
-- A player runs /odb-link in-game to get a short code, then runs the bridge's link
-- command in Discord (e.g. "!link CODE"), which calls /odb-confirm-link over RCON with
-- the Discord user id/name. The code is derived deterministically (no math.random, so
-- it's multiplayer-safe) and is short-lived.

local LINK_TTL_TICKS = 60 * 60 -- ~60 seconds
local CODE_CHARS = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

local function make_link_code(counter)
  local mixed = (counter * 2654435761 + game.tick) % 2176782336 -- 36^6
  local code = ""
  for _ = 1, 6 do
    local r = mixed % 36
    code = string.sub(CODE_CHARS, r + 1, r + 1) .. code
    mixed = math.floor(mixed / 36)
  end
  return code
end

-- /odb-link — run by a player in-game to start linking their Discord account.
commands.add_command("odb-link", "Get a code to link your Discord account", function(cmd)
  local player = cmd.player_index and game.get_player(cmd.player_index)
  if not player then return end -- must be a player (not RCON)
  storage.odb.pending = storage.odb.pending or {}
  storage.odb.link_counter = (storage.odb.link_counter or 0) + 1
  local code = make_link_code(storage.odb.link_counter)
  storage.odb.pending[code] = { player = player.name, expires = game.tick + LINK_TTL_TICKS }
  player.print("[Discord link] Code: " .. code
    .. " — in Discord run:  !link " .. code .. "   (expires in ~60s)")
end)

-- /odb-confirm-link <code> <discord_id> <discord_name...> — called by the bridge over RCON.
commands.add_command("odb-confirm-link", "Open Discord Bridge: confirm a player link (RCON)", function(cmd)
  if cmd.player_index then return end -- RCON / server only
  local code, discord_id, discord_name =
    string.match(cmd.parameter or "", "^(%S+)%s+(%S+)%s*(.*)$")
  if not code then
    rcon.print("ERROR: usage /odb-confirm-link <code> <discord_id> <name>")
    return
  end
  code = string.upper(code)
  storage.odb.pending = storage.odb.pending or {}
  storage.odb.links = storage.odb.links or {}
  local pend = storage.odb.pending[code]
  if not pend then
    rcon.print("That link code is invalid or already used.")
    return
  end
  storage.odb.pending[code] = nil
  if game.tick > pend.expires then
    rcon.print("That link code has expired — run /odb-link in-game again.")
    return
  end
  storage.odb.links[pend.player] = { discord_id = discord_id, discord_name = discord_name }
  local who = (discord_name ~= "" and discord_name) or discord_id
  game.print("[color=114,137,218][Discord][/color] " .. pend.player .. " linked to " .. who)
  rcon.print("Linked " .. pend.player .. " to Discord user " .. who .. ".")
end)

-- /odb-unlink — a player removes their own link.
commands.add_command("odb-unlink", "Unlink your Discord account", function(cmd)
  local player = cmd.player_index and game.get_player(cmd.player_index)
  if not player then return end
  storage.odb.links = storage.odb.links or {}
  if storage.odb.links[player.name] then
    storage.odb.links[player.name] = nil
    player.print("[Discord link] Your Discord account has been unlinked.")
  else
    player.print("[Discord link] You weren't linked.")
  end
end)

-- /odb-unlink-discord <discord_id> — RCON; remove the link for a Discord user (self-serve).
commands.add_command("odb-unlink-discord", "Open Discord Bridge: unlink a Discord user (RCON)", function(cmd)
  if cmd.player_index then return end
  local id = cmd.parameter and string.match(cmd.parameter, "%S+")
  if not id then rcon.print("ERROR: usage /odb-unlink-discord <discord_id>"); return end
  storage.odb.links = storage.odb.links or {}
  local removed = {}
  for player_name, link in pairs(storage.odb.links) do
    if link.discord_id == id then removed[#removed + 1] = player_name end
  end
  for _, player_name in ipairs(removed) do storage.odb.links[player_name] = nil end
  if #removed > 0 then
    rcon.print("Unlinked: " .. table.concat(removed, ", "))
  else
    rcon.print("No link found for that Discord user.")
  end
end)

-- /odb-unlink-player <name> — RCON; remove a specific player's link.
commands.add_command("odb-unlink-player", "Open Discord Bridge: unlink a player (RCON)", function(cmd)
  if cmd.player_index then return end
  local name = cmd.parameter and cmd.parameter:match("^%s*(.-)%s*$")
  if not name or name == "" then rcon.print("ERROR: usage /odb-unlink-player <name>"); return end
  storage.odb.links = storage.odb.links or {}
  if storage.odb.links[name] then
    storage.odb.links[name] = nil
    rcon.print("Unlinked player " .. name .. ".")
  else
    rcon.print("Player " .. name .. " is not linked.")
  end
end)

-- /odb-unlink-all — RCON; clear every link.
commands.add_command("odb-unlink-all", "Open Discord Bridge: clear all links (RCON)", function(cmd)
  if cmd.player_index then return end
  storage.odb.links = storage.odb.links or {}
  local n = 0
  for _ in pairs(storage.odb.links) do n = n + 1 end
  storage.odb.links = {}
  rcon.print("Cleared " .. n .. " link(s).")
end)

-- /odb-links — RCON; list all current links.
commands.add_command("odb-links", "Open Discord Bridge: list all links (RCON)", function(cmd)
  if cmd.player_index then return end
  storage.odb.links = storage.odb.links or {}
  local lines = {}
  for player_name, link in pairs(storage.odb.links) do
    local who = (link.discord_name and link.discord_name ~= "" and link.discord_name) or link.discord_id
    lines[#lines + 1] = player_name .. " -> " .. who .. " (" .. (link.discord_id or "?") .. ")"
  end
  if #lines > 0 then
    rcon.print(table.concat(lines, "\n"))
  else
    rcon.print("No links.")
  end
end)

-- ─── Baseline layer: vanilla events ──────────────────────────────────────────

script.on_event(defines.events.on_console_chat, function(e)
  if not e.player_index then return end
  local player = game.get_player(e.player_index)
  if not player then return end
  write_event("vanilla.chat", { player = player.name, message = e.message }, player.surface.name)
end)

script.on_event(defines.events.on_player_joined_game, function(e)
  local player = game.get_player(e.player_index)
  if not player then return end
  write_event("vanilla.player_joined", {
    player       = player.name,
    online_count = #game.connected_players,
  })
end)

script.on_event(defines.events.on_player_left_game, function(e)
  local player = game.get_player(e.player_index)
  if not player then return end
  write_event("vanilla.player_left", {
    player       = player.name,
    reason       = e.reason,
    online_count = #game.connected_players,
  })
end)

script.on_event(defines.events.on_player_died, function(e)
  local player = game.get_player(e.player_index)
  if not player then return end
  local cause
  if e.cause and e.cause.valid then
    cause = (e.cause.type == "character" and e.cause.player and e.cause.player.name)
      or e.cause.name
  end
  write_event("vanilla.player_died", { player = player.name, cause = cause }, player.surface.name)
end)

script.on_event(defines.events.on_rocket_launched, function(e)
  local rocket = e.rocket
  if not (rocket and rocket.valid) then return end
  write_event("vanilla.rocket_launched", {
    surface       = rocket.surface.name,
    flight_count  = rocket.force.rockets_launched,
  }, rocket.surface.name)
end)

script.on_event(defines.events.on_research_finished, function(e)
  local research = e.research
  if not research then return end
  write_event("vanilla.research_finished", { tech_name = research.name, level = research.level })
end)

-- Emit one game_started event per session (server start / load). on_tick MUST stay
-- registered unconditionally: a self-unregistering handler makes the registered-event
-- set differ between the server and a freshly-loading client, which breaks multiplayer
-- joins ("mod event handlers are not identical"). The session-local flag (not stored)
-- re-arms on each load and gates the one-time write, so the handler is a cheap no-op
-- afterward.
local game_started_emitted = false
script.on_event(defines.events.on_tick, function()
  if game_started_emitted then
    return
  end
  game_started_emitted = true
  write_event("vanilla.game_started", {
    online_count = #game.connected_players,
  })
end)
