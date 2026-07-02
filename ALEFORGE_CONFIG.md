# Open Discord Bridge — config guide for AleForge

Short version of what the bridge needs to run, and which parts belong in **your UI form
fields** vs. a **free-form box**. Written for the AleForge integration; nothing here needs
the end user to touch a file by hand.

---

## 1. There is only ONE config schema (don't let the repo confuse you)

The repo has several `*.yaml` files. **They are the same schema** — just pre-filled for
different topologies. You do **not** pick between them; you produce one config.

| File in repo | What it is | Relevant to AleForge? |
|---|---|---|
| `bridge/bridge.yaml.example` | The **fully-annotated** reference schema | ✅ this is the reference |
| `bridge/bridge.yaml` | A live local instance (my server's values) | reference only |
| `bridge/bridge-local.yaml` | Same schema, preset for "Factorio on the same box" | ignore |
| `bridge/bridge.docker.yaml` | Same schema, preset for docker-compose | ignore |
| `bridge/bridge.sftp.yaml` | Same schema, preset for the **SFTP** transport | only if you use SFTP |
| `docker-compose.yml`, `pkg/.../openapi.yaml`, `.github/workflows/*.yml` | **Not bridge config** | ignore entirely |

So: **one schema, shown in `bridge.yaml.example`.** Everything below is that schema.

---

## 2. The real choice for AleForge: file mode vs. env-var mode

The bridge auto-detects how it's configured, based purely on **whether the `-config` file
exists** at the path it's pointed at:

- **File mode** — a `bridge.yaml` exists at the `-config` path. Full feature set. This is
  what your UI should **generate**.
- **Env-var mode** — *no* file at that path; every setting comes from environment variables
  (`ODB_*` + secrets). Good for a pure "panel variables" UI, **but** it can't express the
  full custom-commands feature (see §4), and the shipped egg only wires a subset (see §3).

> ⚠️ **The two modes are mutually exclusive per run — they do NOT blend.** If a file exists
> at the `-config` path, **every `ODB_*` env var is ignored.** The reference egg starts the
> bridge with `-config /home/container/bridge.yaml`, so if your UI writes a file there you
> are in *file mode* and the panel variables do nothing. Pick one mode deliberately: either
> write the file (and put everything in it), or make sure no file exists at that path and
> drive it entirely from env vars.
>
> Running env-var mode? Set **`ODB_CONFIG=none`** in the startup env. It forces env-var
> mode even if a `bridge.yaml` appears (e.g. recreated by a panel config-file processor),
> so a stray file can never silently flip the bridge into file mode.

**Debugging aid:** at every startup — including failed ones — the bridge writes
`bridge.effective.yaml` next to the binary (`/home/container/`): the fully-resolved config
it actually used, with a header stating the mode (file vs env-var), the validation result,
each secret's status (`SET (n chars)` / `MISSING` — names only, never values), and warnings
for unknown config keys (e.g. values injected at a wrong key path). It's output only, never
read back. Point support tickets at this file first.

**Secrets are never in the YAML, in either mode.** The YAML only names the env var to read
(`token_env: DISCORD_BOT_TOKEN`); the actual secret is supplied as an environment variable.
So "Bot token" / "RCON password" form fields always flow into env vars, never the file.
(Note: Pterodactyl-style panel variables are *plaintext*, not a secret store — if your UI
surfaces these, mask/handle them as secrets.)

---

## 3. The structured settings → your UI form fields

These are scalar values with a clean 1:1 mapping. Each row is one form field. The YAML key
and the env-var name are two encodings of the **same** setting — pick whichever mode you run.

| Form field | YAML key | Env var (env mode) | Required? | Notes |
|---|---|---|---|---|
| **Discord bot token** 🔒 | `discord.token_env` → secret | `DISCORD_BOT_TOKEN` | ✅ | secret |
| **RCON password** 🔒 | `factorio.rcon.password_env` → secret | `FACTORIO_RCON_PASSWORD` | ✅ | secret; must match the Factorio server |
| **RCON address** | `factorio.rcon.address` | `ODB_RCON_ADDRESS` | ✅ | `host:port`; loopback for the sidecar |
| **Events file path** | `factorio.events_file` | `ODB_EVENTS_FILE` | ✅ | path to the mod's `events.jsonl` |
| **Links file path** | `factorio.links_file` | `ODB_LINKS_FILE` | – | **persist on a volume** (see §7); default `links.json` next to the binary |
| **Transport** | `transport` | `ODB_TRANSPORT` | – | `local` (default) or `sftp` |
| **Poll interval** | `poll_interval` | `ODB_POLL_INTERVAL` | – | default `1s` |
| **Discord guild (server) ID** | `discord.guild_id` | `ODB_DISCORD_GUILD_ID` | – | **required for** slash commands AND linked role/nickname (see §7) |
| **Channel ID** (single channel) | `discord.routes` (one `*` route) | `ODB_DISCORD_CHANNEL_ID` | ✅* | the simple "everything → one channel" case |
| **Routes** (per-source channels) | `discord.routes` | `ODB_ROUTES` | ✅* | `vanilla.chat=111,mts.*=222,*=111`; **order matters** (see §7) |
| **Built-in commands** | `discord.commands` (copied from example) | `ODB_DEFAULT_COMMANDS` | – | default **on** (env mode): `!players` + full linking family, correctly gated |
| **Color event labels** | `discord.embed` | `ODB_EMBED` | – | default `false` |
| **Announce connect/disconnect** | `discord.announce_status` | `ODB_ANNOUNCE_STATUS` | – | default `false` |
| **Live channel-topic status** | `discord.channel_topic_status` | `ODB_CHANNEL_TOPIC_STATUS` | – | **ON by default; needs Manage Channels** (see §7) |
| **Linked-player role ID** | `discord.linked_role_id` | `ODB_LINKED_ROLE_ID` | – | needs Manage Roles + role above target; needs `guild_id` |
| **Linked-player nickname** | `discord.linked_nickname` | `ODB_LINKED_NICKNAME` | – | needs Manage Nicknames; needs `guild_id`; e.g. `{discord} \| {factorio}` |
| **Admin role IDs** | `discord.admins.roles` | `ODB_ADMIN_ROLES` | – | comma-separated |
| **Admin user IDs** | `discord.admins.users` | `ODB_ADMIN_USERS` | – | comma-separated |
| **Control API enabled** | `control_api.enabled` | `ODB_CONTROL_API_ENABLED` | – | default `false` |
| **Control API bind addr** | `control_api.listen` | `ODB_CONTROL_API_LISTEN` | – | default `127.0.0.1:7777` |
| **Control API token** 🔒 | `control_api.auth_token_env` → secret | `BRIDGE_CONTROL_TOKEN` | if enabled | secret |

\* You need a channel **one way or the other**: either a single channel ID **or** explicit
routes. At least one route must exist.

**SFTP fields** (only if Transport = `sftp` — the bridge runs in a *separate* container from
Factorio rather than as a sidecar):

| Form field | YAML key | Env var | Notes |
|---|---|---|---|
| SFTP host | `factorio.sftp.host` | `ODB_SFTP_HOST` | `host:port` (Pterodactyl SFTP is often `:2022`) |
| SFTP user | `factorio.sftp.user` | `ODB_SFTP_USER` | |
| SFTP key path | `factorio.sftp.key_path` | `ODB_SFTP_KEY_PATH` | OR use a password ↓ |
| SFTP password 🔒 | `factorio.sftp.password_env` → secret | `SFTP_PASSWORD` | secret; alternative to a key |
| SFTP known_hosts | `factorio.sftp.known_hosts_path` | `ODB_SFTP_KNOWN_HOSTS` | omit to skip host-key check (logs a warning) |

> **The shipped `pterodactyl-egg.json` is a deliberate subset** — it wires only ~16 of these
> variables (token, RCON pw/addr, events file, channel, routes, transport, SFTP host/user/key,
> control-API enable/token, admin roles/users, config mode `ODB_CONFIG=none`, built-in
> commands toggle `ODB_DEFAULT_COMMANDS`). It does **not** include `ODB_DISCORD_GUILD_ID`,
> `ODB_EMBED`, `ODB_ANNOUNCE_STATUS`, `ODB_CHANNEL_TOPIC_STATUS`, `ODB_LINKED_*`,
> `ODB_POLL_INTERVAL`, `ODB_COMMANDS`, `SFTP_PASSWORD`, `ODB_SFTP_KNOWN_HOSTS`, or
> `ODB_CONTROL_API_LISTEN`. If you want those features in env-var mode, add the variables
> yourself. (Notably: the egg has no `ODB_COMMANDS` for extra custom commands — though
> `!players` + the linking family are built in by default — and no `SFTP_PASSWORD`, so
> SFTP works by key only.)

---

## 4. The free-form setting → custom commands

This is the part that **does not fit into simple form fields** — your instinct is right.

A "command" maps a Discord trigger (e.g. `!players`) to an RCON command, with these optional
powers:

- `admin: true` — restrict to admins
- `args: true` — interpolate user input (`{args}`, `{1}`, `{user}`, …) into the RCON command
- multiline `rcon:` — a whole `/silent-command` Lua script in one command
- `usage_hint:` — custom "how to use" text shown when required args are missing
- `discord_link: true` — wire up the reverse Discord→game account-linking flow

```yaml
discord:
  commands:
    - trigger: "!players"
      rcon: "/players online"          # public, simple

    - trigger: "!kick"
      admin: true                      # admins only
      args: true                       # "!kick Bob" -> "/kick Bob"
      rcon: "/kick {1}"

    - trigger: "!cleanup"              # multiline script, sent as one RCON call
      admin: true
      rcon: |
        /silent-command
        local n = 0
        for _, e in pairs(game.surfaces[1].find_entities_filtered{name="item-on-ground"}) do
          e.destroy(); n = n + 1
        end
        rcon.print("removed " .. n)
```

⚠️ **Important limitation:** in **env-var mode**, *custom* commands use `ODB_COMMANDS`
(`!trigger=/rcon cmd;!t2=/cmd2`), which only supports the **simple subset**: public,
single-line commands. **`admin:`, `args:`, multiline, `usage_hint:`, and `discord_link:`
all require the YAML file.**

**But the standard set is built in:** env-var mode ships `!players` plus the complete
account-linking family (`!link`, `!unlink`, and admin-gated `!links` / `!unlink-player` /
`!unlink-all`) by default, with the exact placeholders and admin gating the bridge's
protocol needs — no configuration required. Set `ODB_DEFAULT_COMMANDS=false` to disable,
or define the same trigger in `ODB_COMMANDS` to override one. So the free-form/custom
story above only matters for commands *beyond* that set (`!kick`, scripts, …).

So if you want users to define real custom commands through your UI, the clean approach is a
**free-form text box** whose contents you drop verbatim into the YAML `commands:` block (i.e.
generate a file). A plain "list of trigger → command" form only gets you the
public/single-line subset.

---

## 5. Recommended shape for AleForge

Because of §4, the cleanest fit is **file mode with a generated `bridge.yaml`**:

1. **Form fields** (the table in §3) populate the structured top of the file.
2. **One free-form "Custom commands (YAML)" text area** is pasted in as the `commands:`
   block (§4).
3. **Secrets** (token, RCON password, optional control-API/SFTP) go into **env vars**, not
   the file — the file just references them by name.

My setup-wizard library gives you a head start, but **mind its scope**: it collects only
transport / RCON address / events file / guild ID / channel ID / SFTP fields, and renders a
deliberately **minimal** `bridge.yaml` — one catch-all `*` route, one hardcoded `!players`
command, fixed `*_env` secret references, and **none** of the embed / announce / topic /
linked-role / linked-nickname / admins / control-API / multi-route / custom-command fields.
Treat the §3 table as a **superset** of what the wizard emits — you'll template the rest
yourself.

**If you pre-fill the account-linking commands in a generated YAML, copy them exactly** —
they're not trivial and a naive default silently breaks:
- `!link` / `!unlink` need `args: true` (they interpolate `{userid}` / `{1}` / `{user}`).
- `!links` / `!unlink-player` / `!unlink-all` **must** be `admin: true`, or you expose admin
  unlink operations to everyone in the channel.
- Use the exact block in `bridge.yaml.example` / `DEPLOYMENT.md` §2 "Player linking".

(In **env-var mode** you don't copy anything — that exact set is built in and on by
default, see §4.)

For the runtime topology: AleForge's model is the **sidecar** (bridge runs in the *same*
container as Factorio, `local` transport, RCON over loopback). In that case Transport stays
`local`, the events path is local, and you don't need any of the SFTP fields.

If you'd rather stick to pure panel variables (no file), env-var mode works — just accept the
custom-commands limitation (§4) and the egg-subset caveat (§3).

---

## 6. Reference `bridge.yaml` (file mode)

An annotated example covering the common settings. Anything commented out is optional. (This
is not exhaustive — the full key list is the §3 table plus `bridge.yaml.example`.)

```yaml
# Secrets are referenced by env-var NAME here; the values live in the environment.
factorio:
  rcon:
    address: 127.0.0.1:27015            # loopback for the sidecar
    password_env: FACTORIO_RCON_PASSWORD
  events_file: /factorio/script-output/open-discord-bridge/events.jsonl
  links_file: /data/odb-links.json      # persist account links on a volume (see §7)
  # required_mod_version: "0.1.3"        # optional: pin the companion-mod version it handshakes against

  # Only when transport: sftp (bridge in a separate container from Factorio)
  # sftp:
  #   host: game-host:2022
  #   user: factorio-bridge
  #   key_path: /secrets/bridge_ed25519     # or password_env: SFTP_PASSWORD
  #   known_hosts_path: /secrets/known_hosts # omit to skip host-key check (warns)

transport: local                        # local | sftp  (inbound is always RCON)
poll_interval: 1s

discord:
  token_env: DISCORD_BOT_TOKEN
  guild_id: "000000000000000000"        # required for slash commands AND linked role/nickname
  announce_status: true                 # post connect/disconnect
  # embed: false                        # ANSI-colored event-category labels
  # channel_topic_status: true          # ON by default; needs Manage Channels (see §7)
  # linked_role_id: "112233445566778899"     # needs Manage Roles + guild_id
  # linked_nickname: "{discord} | {factorio}" # needs Manage Nicknames + guild_id
  # status_player_joined_event: "vanilla.player_joined"  # override for modded join/leave events
  # status_player_left_event:   "vanilla.player_left"

  # At least one route is required. First match wins, and route[0] is also where the
  # channel-topic status is written (see §7).
  routes:
    - source: "*"                       # catch-all
      channel_id: "111111111111111111"

  # Who counts as admin. Defaults to "Discord Administrator permission = admin".
  admins:
    roles: []                           # role IDs
    users: []                           # user IDs
    # use_discord_permission: false     # require the lists instead

  # The free-form part (see §4).
  commands:
    - trigger: "!players"
      rcon: "/players online"

# HTTP Control API — off by default. Bind to loopback if enabled.
control_api:
  enabled: false
  listen: 127.0.0.1:7777
  auth_token_env: BRIDGE_CONTROL_TOKEN
```

**Secrets** supplied as environment variables (never in the file):

```
DISCORD_BOT_TOKEN=...
FACTORIO_RCON_PASSWORD=...
# BRIDGE_CONTROL_TOKEN=...   # only if control_api.enabled
# SFTP_PASSWORD=...          # only if transport: sftp with password auth
```

---

## 7. Gotchas that silently break a setup (please read)

These don't error loudly — they just don't work, which makes them the most likely support
tickets:

1. **File vs env-var mode is exclusive.** A `bridge.yaml` present at the `-config` path makes
   the bridge ignore every `ODB_*` var. Don't half-fill one and half-fill the other.
2. **Account links persistence.** Links default to `links.json` *next to the binary*. If your
   binary lives in the persistent server volume (`/home/container`), that's fine — they
   persist across restarts. They're only lost if the binary lives somewhere ephemeral, or on
   a panel "reinstall" that wipes the volume. If in doubt, set `factorio.links_file` /
   `ODB_LINKS_FILE` to an explicit volume path.
3. **Channel-topic status needs Manage Channels — and it's ON by default.** My wizard's invite
   URL does **not** request Manage Channels, so a bot invited that way will log a permission
   warning and fail to set the topic. Either add Manage Channels to the bot invite, or default
   `channel_topic_status` / `ODB_CHANNEL_TOPIC_STATUS` to `false`.
4. **`guild_id` gates more than slash commands.** Linked role + linked nickname sync only run
   when `guild_id` is set. A UI that offers those toggles without also collecting the guild ID
   produces a silently inert setup.
5. **Linked-role / linked-nickname permissions.** Role needs **Manage Roles** *and* the bot's
   own role must sit **above** the target role. Nickname needs **Manage Nicknames** (and can't
   rename the server owner).
6. **Channel-topic writes to route[0], not the `*` route.** The status topic goes to the
   **first** route in the list (in `ODB_ROUTES` order), not whichever entry is the catch-all.
   If you emit `vanilla.chat=111,*=222`, the topic lands on `111`.
7. **Admin-gate destructive commands.** Anything without `admin: true` is runnable by anyone
   who can post in the bridged channel. Keep `/kick`, `/ban`, `/silent-command`, unlink-all,
   etc. behind `admin: true`.

---

## 8. Notes on your sidecar startup command

You launch the bridge backgrounded (`./open-discord-bridge &`), in env-var mode, *before*
Factorio starts, with 5 vars: `DISCORD_BOT_TOKEN`, `ODB_DISCORD_CHANNEL_ID`,
`ODB_RCON_ADDRESS=127.0.0.1:<RCON_PORT>`, `FACTORIO_RCON_PASSWORD`,
`ODB_EVENTS_FILE=./script-output/open-discord-bridge/events.jsonl`. We checked this against
the bridge's actual code:

**✅ Works as-is (no change needed):**
- **Starting the bridge before Factorio is safe.** RCON connects lazily and reconnects on its
  own; the events-file tailer polls until the file appears. The bridge does **not** crash when
  RCON is down or the events file is missing — game→Discord starts working the moment events
  appear, and Discord→game starts the moment RCON is up.
- **Config validation passes** with those 5 vars; `ODB_DISCORD_CHANNEL_ID` creates the
  required catch-all route.
- **Bidirectional chat works** (game→Discord and Discord→game) with no commands configured.
- **`links.json` lands at `/home/container/links.json`** (the persistent volume) — links
  survive restarts.

**⚠️ Please fix these:**

1. **Require RCON password when the bridge is enabled.** If `RCON_PASSWORD` is empty, the
   bridge **exits immediately on startup** (validation rejects an empty RCON password) — and
   because it's backgrounded with no supervisor, it just stays dead, silently. Your Factorio
   line already gates `--rcon-port` on a non-empty password, so make the panel field
   **required** whenever the bridge is on (both `RCON_PASSWORD` and `RCON_PORT`).

2. **Stop the bridge with the container.** Your `&` has no `trap`, so on stop the bridge is
   hard-killed (no graceful Discord disconnect), and if Factorio exits without the container
   stopping the bridge orphans against a dead server. Mirror our `run-sidecar.sh`: capture the
   PID and trap EXIT.

   ```sh
   if [ "{{ENABLE_DISCORD_BRIDGE}}" = "1" ] || [ "{{ENABLE_DISCORD_BRIDGE}}" = "true" ]; then
     export DISCORD_BOT_TOKEN="{{DISCORD_BOT_TOKEN}}"
     export ODB_DISCORD_CHANNEL_ID="{{DISCORD_CHANNEL_ID}}"
     export ODB_RCON_ADDRESS="127.0.0.1:{{RCON_PORT}}"
     export FACTORIO_RCON_PASSWORD="{{RCON_PASSWORD}}"
     export ODB_EVENTS_FILE="./script-output/open-discord-bridge/events.jsonl"
     ./open-discord-bridge &
     BRIDGE_PID=$!
     trap 'kill -TERM "$BRIDGE_PID" 2>/dev/null' EXIT   # bridge dies with the server, gracefully
   fi
   ```
   (Or just call `./run-sidecar.sh ./bin/x64/factorio <args…>` and let it own the lifecycle.)

**ℹ️ What your 5-var setup does NOT include (all optional):**
- ~~No commands at all~~ **Update:** `!players` + the full linking family are now built in
  by default (env-var mode, §4) — your 5-var setup gets them with no extra variables.
  `ODB_COMMANDS` adds further public single-line commands; other custom commands
  (admin/args/multiline) need file mode.
- **No slash commands** and **no linked role/nickname** — both need `ODB_DISCORD_GUILD_ID`.
- **Channel-topic status is ON by default** and tries to edit the channel topic every ~5 min;
  it needs **Manage Channels**, which the standard bot invite does *not* grant. Either grant
  it or set `ODB_CHANNEL_TOPIC_STATUS=false` to silence the warnings.
- `ODB_ANNOUNCE_STATUS` (connect/disconnect posts) and `ODB_EMBED` (colored labels) are off.
