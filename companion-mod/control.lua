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
    message    = args.message,
    channel    = args.channel,
    avatar_url = args.avatar_url,
  })

  -- Default delivery. Integrators that subscribe to on_incoming may do their own
  -- (e.g. MTS routing into a specific team's chat) in addition to this.
  local user = args.user or "Discord"
  local msg  = args.message or ""
  game.print(string.format("[color=114,137,218][Discord][/color] %s: %s", user, msg))
end

-- ─── Storage / custom event id ───────────────────────────────────────────────

local function ensure_storage()
  storage.odb         = storage.odb or {}
  storage.odb.sources = storage.odb.sources or {}
  storage.odb.links   = storage.odb.links or {}
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

  -- Player-link query (linking flow itself is Phase 2).
  linked_discord_id = function(player_name)
    return storage.odb.links[player_name]
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
