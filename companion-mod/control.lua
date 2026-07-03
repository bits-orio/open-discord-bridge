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
  -- for_player=0: write only the server's copy. Without it, every connected client in a
  -- multiplayer game independently writes its own copy of the file, which is wasteful
  -- and can race/corrupt on servers with multiple clients.
  helpers.write_file(EVENTS_FILE, line, truncated_this_session, 0)
  truncated_this_session = true
end

-- ─── Inbound (Discord → game) ────────────────────────────────────────────────
-- The bridge process delivers messages by running the /odb-incoming console command
-- over RCON (achievement-safe; no /silent-command). We raise our own custom event so
-- integrators can override delivery, then run a default "print to all" handler.

-- Discord message text and display names are untrusted: strip newlines and other control
-- characters, which would otherwise forge a second chat line ("hi\nBob: gg" renders a fake
-- line as Bob). Factorio rich-text tags ([color=...], [img=...], [entity=...]) are KEPT on
-- purpose — they're a supported feature (https://wiki.factorio.com/rich_text), and in-game
-- players can type them in chat anyway. After this filter a Discord message can express
-- exactly what an in-game chat line can: tags yes, extra lines no.
-- RCON command args are sanitized separately by the bridge process before they ever reach
-- here (see interpolate/sanitizeArg in cmd/bridge/main.go).
local function sanitize_for_chat(text)
  if type(text) ~= "string" then return text end
  return text:gsub("%c+", " ")
end

local function handle_incoming(args)
  if type(args) ~= "table" then return end

  -- Integrators receive the RAW user/message (they may need exact content, e.g. for
  -- command parsing). Anything an integrator prints into game chat must strip newlines
  -- like sanitize_for_chat does — raw Discord text can forge extra chat lines. (Rich-text
  -- tags are a supported feature and need no stripping.)
  script.raise_event(on_incoming_event, {
    user       = args.user,
    user_id    = args.user_id,
    message    = args.message,
    channel    = args.channel,
    avatar_url = args.avatar_url,
  })

  -- Default delivery. Integrators that subscribe to on_incoming may do their own
  -- (e.g. MTS routing into a specific team's chat) in addition to this.
  local user = sanitize_for_chat(args.user) or "Discord"
  local msg  = sanitize_for_chat(args.message) or ""

  -- If this Discord user is linked to a player, tint their name with the player's
  -- in-game chat color so it reads like that player speaking.
  local name = user
  if args.user_id and storage.odb and storage.odb.links then
    for player_name, link in pairs(storage.odb.links) do
      if link.discord_id == args.user_id then
        local p = game.get_player(player_name)
        if p and p.valid then
          local c = p.chat_color
          name = string.format("[color=%.3f,%.3f,%.3f]%s[/color]", c.r, c.g, c.b, player_name)
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
  storage.odb                  = storage.odb or {}
  storage.odb.sources          = storage.odb.sources or {}
  storage.odb.links            = storage.odb.links or {}            -- player_name -> { discord_id, discord_name }
  storage.odb.pending          = storage.odb.pending or {}          -- game-initiated: link code -> { player, expires }
  storage.odb.discord_pending  = storage.odb.discord_pending or {}  -- discord-initiated: link code -> { discord_id, discord_name, expires }
  storage.odb.baseline_disabled = storage.odb.baseline_disabled or {} -- vanilla event key -> true
  -- force name -> last announced launch MARK. Seeded from each force's current
  -- total on FIRST creation (fresh install / migration) so adding or updating the
  -- mod on a live save doesn't re-announce the current neighbourhood -- it stays
  -- quiet until the next genuine mark. New forces start at 0 (no launches yet).
  -- Mirrors multi-team-support's identical seed so the two mods behave the same.
  if not storage.odb.rocket_announced_total then
    storage.odb.rocket_announced_total = {}
    for _, force in pairs(game.forces) do
      if force.valid and (force.rockets_launched or 0) > 0 then
        storage.odb.rocket_announced_total[force.name] = force.rockets_launched
      end
    end
  end
  storage.odb.rocket_last_announced = nil -- migrated from the old tick-based throttle
end

-- baseline emits a built-in vanilla.* event unless an integrator has disabled that key via
-- the set_baseline interface (so e.g. MTS can own research announcements with team info).
local function baseline(key, data, surface_name)
  if storage.odb and storage.odb.baseline_disabled and storage.odb.baseline_disabled[key] then
    return
  end
  write_event("vanilla." .. key, data, surface_name)
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
  -- toggles without hardcoding any mod. ensure_storage() guards against an integrator
  -- calling in before our own on_init has run (mod load order is not guaranteed).
  register_source = function(args)
    if type(args) ~= "table" or type(args.namespace) ~= "string" then return end
    ensure_storage()
    storage.odb.sources[args.namespace] = args.events or {}
  end,

  -- Enable/disable a built-in baseline event (e.g. "research_finished") so an integrator
  -- can announce it itself with richer context. { event = "research_finished", enabled = false }
  set_baseline = function(args)
    if type(args) ~= "table" or type(args.event) ~= "string" then return end
    ensure_storage()
    if args.enabled == false then
      storage.odb.baseline_disabled[args.event] = true
    else
      storage.odb.baseline_disabled[args.event] = nil
    end
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

  -- Returns the linked Discord user ID for a player, or nil. ensure_storage() guards
  -- against this being called before our own on_init has run (mod load order is not
  -- guaranteed), same as register_source/set_baseline above.
  linked_discord_id = function(player_name)
    ensure_storage()
    local link = storage.odb.links[player_name]
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
  local links = {}
  if storage.odb and storage.odb.links then
    for player_name, link in pairs(storage.odb.links) do
      if link.discord_id then
        links[#links + 1] = {
          discord_id   = link.discord_id,
          player       = player_name,
          discord_name = link.discord_name,
        }
      end
    end
  end
  local players = {}
  for _, p in pairs(game.connected_players) do
    players[#players + 1] = p.name
  end
  rcon.print(helpers.table_to_json({
    mod_version = script.active_mods["open-discord-bridge"],
    interface   = INTERFACE,
    sources     = (storage.odb and storage.odb.sources) or {},
    links       = links,
    players     = players,
    mods        = script.active_mods,
    ticks       = game.tick,
  }))
end)

-- ─── Player linking (Discord ↔ Factorio) ─────────────────────────────────────
-- A player runs /odb-link in-game to get a short code, then runs the bridge's link
-- command in Discord (e.g. "!link CODE"), which calls /odb-confirm-link over RCON with
-- the Discord user id/name. The code is short-lived and drawn from Factorio's
-- math.random, which the engine replaces with its own deterministic, multiplayer-synced
-- generator (see the runtime API docs on math.random) — not real entropy, but not
-- predictable/enumerable from outside the game either, which is enough here since this
-- isn't a cryptographic secret.

local LINK_TTL_TICKS = 60 * 60 -- ~60 seconds
local CODE_CHARS = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
-- Six independent draws rather than one math.random(0, 36^6-1): Factorio's API types
-- math.random bounds as a signed 32-bit int, and 36^6-1 (2,176,782,335) exceeds that.
-- Entropy is identical (36^6), and the per-character draw stays well inside int range.
local function make_link_code()
  local code = ""
  for _ = 1, 6 do
    local r = math.random(1, #CODE_CHARS)
    code = code .. string.sub(CODE_CHARS, r, r)
  end
  return code
end

-- Sweep tick-expired entries out of a pending-link table. Called at the point a new code
-- is created, piggybacking on that natural churn instead of a separate periodic scan, so
-- storage.odb.pending / discord_pending don't grow unbounded on a long-running server.
local function purge_expired(pending_table)
  local now = game.tick
  for code, entry in pairs(pending_table) do
    if now > entry.expires then
      pending_table[code] = nil
    end
  end
end

local ODB_LINK_FRAME = "odb_link_frame"

local function open_link_gui(player, code)
  local screen = player.gui.screen
  if screen[ODB_LINK_FRAME] then screen[ODB_LINK_FRAME].destroy() end

  local frame = screen.add{type = "frame", name = ODB_LINK_FRAME,
                            caption = "Discord Account Link", direction = "vertical"}
  frame.auto_center = true

  local content = frame.add{type = "flow", direction = "vertical"}
  content.style.padding = 8
  content.style.vertical_spacing = 6

  content.add{type = "label", caption = "Paste this command into Discord:"}

  local tf = content.add{type = "textfield", name = "odb_link_code", text = "!link " .. code}
  tf.style.width = 220

  content.add{type = "label", caption = "[color=255,165,0]Expires in ~60 seconds.[/color]"}

  content.add{type = "button", name = "odb_link_close", caption = "Close", style = "back_button"}
end

-- /odb-link — run by a player in-game to start linking their Discord account.
commands.add_command("odb-link", "Get a code to link your Discord account", function(cmd)
  local player = cmd.player_index and game.get_player(cmd.player_index)
  if not player then return end -- must be a player (not RCON)
  storage.odb.pending = storage.odb.pending or {}
  purge_expired(storage.odb.pending)
  local code = make_link_code()
  storage.odb.pending[code] = { player = player.name, expires = game.tick + LINK_TTL_TICKS }
  open_link_gui(player, code)
end)

script.on_event(defines.events.on_gui_click, function(e)
  if e.element.name == "odb_link_close" then
    local player = game.get_player(e.player_index)
    if player and player.gui.screen[ODB_LINK_FRAME] then
      player.gui.screen[ODB_LINK_FRAME].destroy()
    end
  end
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
  -- Discord display names are attacker-controlled: strip newlines at print time so a
  -- name can't forge extra chat lines (rich-text tags render by design).
  game.print("[color=114,137,218][Discord][/color] " .. pend.player .. " linked to " .. sanitize_for_chat(who))
  write_event("odb.link_confirmed", {
    player       = pend.player,
    discord_id   = discord_id,
    discord_name = who,
    text         = pend.player .. " linked to Discord user " .. who .. ".",
  })
  rcon.print("Linked " .. pend.player .. " to Discord user " .. who .. ".")
end)

-- /odb-register-discord-code <code> <discord_id> <discord_name...> — bridge calls this over RCON
-- to pre-register a code issued on the Discord side so /odb-discord-link can complete the link.
commands.add_command("odb-register-discord-code", "Open Discord Bridge: register Discord-initiated link code (RCON)", function(cmd)
  if cmd.player_index then return end -- RCON only
  local code, discord_id, discord_name =
    string.match(cmd.parameter or "", "^(%S+)%s+(%S+)%s*(.*)$")
  if not code then
    rcon.print("ERROR: usage /odb-register-discord-code <code> <discord_id> <name>")
    return
  end
  storage.odb.discord_pending = storage.odb.discord_pending or {}
  purge_expired(storage.odb.discord_pending)
  storage.odb.discord_pending[string.upper(code)] = {
    discord_id   = discord_id,
    discord_name = discord_name,
    expires      = game.tick + LINK_TTL_TICKS,
  }
end)

-- /odb-discord-link <code> — run by a player in-game to complete a Discord-initiated link.
commands.add_command("odb-discord-link", "Complete Discord account linking with a code from Discord", function(cmd)
  local player = cmd.player_index and game.get_player(cmd.player_index)
  if not player then return end
  local code = string.upper(string.match(cmd.parameter or "", "%S+") or "")
  if code == "" then
    player.print("[Discord link] Usage: /odb-discord-link <code>")
    return
  end
  storage.odb.discord_pending = storage.odb.discord_pending or {}
  local pend = storage.odb.discord_pending[code]
  if not pend then
    player.print("[Discord link] That code is invalid or has already been used.")
    return
  end
  if game.tick > pend.expires then
    storage.odb.discord_pending[code] = nil
    player.print("[Discord link] That code has expired — type !link in Discord to get a new one.")
    return
  end
  storage.odb.discord_pending[code] = nil
  storage.odb.links = storage.odb.links or {}
  storage.odb.links[player.name] = { discord_id = pend.discord_id, discord_name = pend.discord_name }
  local who = (pend.discord_name ~= "" and pend.discord_name) or pend.discord_id
  -- Same line-forgery guard as the /odb-confirm-link announcement above.
  game.print("[color=114,137,218][Discord][/color] " .. player.name .. " linked to " .. sanitize_for_chat(who))
  write_event("odb.link_confirmed", {
    player       = player.name,
    discord_id   = pend.discord_id,
    discord_name = who,
    text         = player.name .. " linked to Discord user " .. who .. ".",
  })
end)

-- /odb-unlink — a player removes their own link.
commands.add_command("odb-unlink", "Unlink your Discord account", function(cmd)
  local player = cmd.player_index and game.get_player(cmd.player_index)
  if not player then return end
  storage.odb.links = storage.odb.links or {}
  local existing_link = storage.odb.links[player.name]
  if existing_link then
    storage.odb.links[player.name] = nil
    player.print("[Discord link] Your Discord account has been unlinked.")
    write_event("odb.link_removed", {
      player     = player.name,
      discord_id = existing_link.discord_id,
      text       = player.name .. " unlinked their Discord account.",
    })
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
  for _, player_name in ipairs(removed) do
    storage.odb.links[player_name] = nil
    write_event("odb.link_removed", { player = player_name, discord_id = id })
  end
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
  local pl = storage.odb.links[name]
  if pl then
    storage.odb.links[name] = nil
    write_event("odb.link_removed", { player = name, discord_id = pl.discord_id })
    rcon.print("Unlinked player " .. name .. ".")
  else
    rcon.print("Player " .. name .. " is not linked.")
  end
end)

-- /odb-unlink-all — RCON; clear every link.
commands.add_command("odb-unlink-all", "Open Discord Bridge: clear all links (RCON)", function(cmd)
  if cmd.player_index then return end
  storage.odb.links = storage.odb.links or {}
  local removed = {}
  for player_name, link in pairs(storage.odb.links) do
    removed[#removed + 1] = { player = player_name, discord_id = link.discord_id }
  end
  storage.odb.links = {}
  for _, r in ipairs(removed) do
    write_event("odb.link_removed", { player = r.player, discord_id = r.discord_id })
  end
  rcon.print("Cleared " .. #removed .. " link(s).")
end)

-- /odb-set-link <player> <discord_id> <discord_name...> — RCON; restore a link without emitting
-- an event. Called by the bridge on connection to repopulate the mod's session storage from
-- the bridge's persistent links.json file.
commands.add_command("odb-set-link", "Open Discord Bridge: restore a player link (RCON)", function(cmd)
  if cmd.player_index then return end
  local player_name, discord_id, discord_name =
    string.match(cmd.parameter or "", "^(%S+)%s+(%S+)%s*(.*)$")
  if not player_name then
    rcon.print("ERROR: usage /odb-set-link <player> <discord_id> <name>")
    return
  end
  storage.odb.links = storage.odb.links or {}
  storage.odb.links[player_name] = { discord_id = discord_id, discord_name = discord_name }
  rcon.print("OK")
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
  -- player_index is absent for messages typed directly into the server's console
  -- (stdin), not just for players — relay those too, tagged as "Server".
  if not e.player_index then
    baseline("chat", { player = "Server", message = e.message })
    return
  end
  local player = game.get_player(e.player_index)
  if not player then return end
  baseline("chat", { player = player.name, message = e.message }, player.surface.name)
end)

-- Emit one game_started event per session (server start / load), on the first player to
-- join. Previously this fired from on_tick, but a headless server auto-pauses at 0
-- connected players and on_tick does not fire while paused — so on an empty server it
-- never fired. Joining is what unpauses the server, so on_player_joined_game always
-- fires exactly when "the game has started" first becomes true/observable.
local game_started_emitted = false

script.on_event(defines.events.on_player_joined_game, function(e)
  local player = game.get_player(e.player_index)
  if not player then return end
  baseline("player_joined", {
    player       = player.name,
    online_count = #game.connected_players,
  })

  if not game_started_emitted then
    game_started_emitted = true
    baseline("game_started", { online_count = #game.connected_players })
  end
end)

script.on_event(defines.events.on_player_left_game, function(e)
  local player = game.get_player(e.player_index)
  if not player then return end
  baseline("player_left", {
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
  baseline("player_died", { player = player.name, cause = cause }, player.surface.name)
end)

-- Space Age's automated cargo rockets can launch repeatedly in quick succession,
-- which would otherwise flood Discord with one message per launch. The
-- announcement cadence WIDENS in tiers as a force's running total grows, so the
-- post rate stays bounded no matter how big the base gets. This mirrors
-- multi-team-support's rocket throttle so a server running both mods behaves
-- identically:
--
--   total ≤ 10     every launch
--   total ≤ 100    one per 5
--   total ≤ 500    one per 25
--   total ≤ 1000   one per 50
--   total > 1000   one per 100   (terminal cadence, forever)
--
-- A 45k-launch megabase posts once every ~100 rockets. Count-based (not the old
-- time-based cooldown) so the cadence tracks progress, not wall-clock.
local ROCKET_TIERS = {
  { upto = 10,        step = 1   },
  { upto = 100,       step = 5   },
  { upto = 500,       step = 25  },
  { upto = 1000,      step = 50  },
  { upto = math.huge, step = 100 },
}

--- The announcement step for a given running total (see ROCKET_TIERS).
local function rocket_announce_step(total)
  for _, tier in ipairs(ROCKET_TIERS) do
    if total <= tier.upto then return tier.step end
  end
  return 100 -- unreachable: the math.huge tier catches everything; defensive
end

-- Report the HIGHEST step-multiple the counter has reached that hasn't been
-- announced yet, rather than testing total % step == 0. force.rockets_launched
-- skips and duplicates values when several rockets resolve close together, so a
-- plain exact-multiple test goes silent (skipped multiple) or double-posts
-- (duplicated multiple); and reporting the raw overshoot reads untidy.
-- floor(total/step)*step is the highest multiple reached; report it when it
-- exceeds the last reported mark (stored as `last`). The feed never goes silent,
-- never double-posts, always lands on a step multiple past 10 (…45300, 45400) —
-- ending in 0 or 5 — and a fresh install / reset on an already-huge force jumps
-- straight to the current neighbourhood instead of crawling up from a low number.
-- Mirrors multi-team-support's rocket_announce_mark exactly. Returns the mark, or nil.
local function rocket_announce_mark(force_name, total)
  storage.odb.rocket_announced_total = storage.odb.rocket_announced_total or {}
  local last = storage.odb.rocket_announced_total[force_name] or 0
  local mark
  if total < last then                 -- counter reset (force recreated): re-arm on raw count
    mark = total
  elseif total <= 10 then              -- pre-alignment tier: every launch, raw count
    mark = (total > last) and total or nil
  else
    local step = rocket_announce_step(total)
    local m = math.floor(total / step) * step   -- highest step multiple the counter has reached
    mark = (m > last) and m or nil
  end
  if mark then storage.odb.rocket_announced_total[force_name] = mark end
  return mark
end

script.on_event(defines.events.on_rocket_launched, function(e)
  local rocket = e.rocket
  if not (rocket and rocket.valid) then return end
  local mark = rocket_announce_mark(rocket.force.name, rocket.force.rockets_launched)
  if not mark then return end
  baseline("rocket_launched", {
    surface       = rocket.surface.name,
    flight_count  = mark,
  }, rocket.surface.name)
end)

script.on_event(defines.events.on_research_finished, function(e)
  local research = e.research
  if not research then return end
  baseline("research_finished", { tech_name = research.name, level = research.level })
end)
