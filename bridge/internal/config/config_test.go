package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnvSingleChannel(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")

	// A missing file path triggers env-var config mode.
	c, err := Load("/no/such/bridge.yaml")
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	if c.Transport != "local" || c.Factorio.RCON.Address != "game:27015" {
		t.Fatalf("unexpected: %+v", c.Factorio)
	}
	if c.Discord.Token != "tok" || c.Factorio.RCON.Password != "pw" {
		t.Fatalf("secrets not resolved")
	}
	if len(c.Discord.Routes) != 1 || c.Discord.Routes[0].Source != "*" || c.Discord.Routes[0].ChannelID != "12345" {
		t.Fatalf("routes: %+v", c.Discord.Routes)
	}
}

func TestLoadFromEnvExplicitRoutes(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_ROUTES", "vanilla.chat=111, mts.*=222 , *=111")

	c, err := Load("/no/such/bridge.yaml")
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	if len(c.Discord.Routes) != 3 {
		t.Fatalf("want 3 routes, got %+v", c.Discord.Routes)
	}
	if c.Discord.Routes[1].Source != "mts.*" || c.Discord.Routes[1].ChannelID != "222" {
		t.Fatalf("route trimming/parse wrong: %+v", c.Discord.Routes[1])
	}
}

func TestLoadFromEnvCommands(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")
	t.Setenv("ODB_COMMANDS", "!players=/players online; !evo=/evolution")
	t.Setenv("ODB_DEFAULT_COMMANDS", "false") // isolate ODB_COMMANDS parsing from the built-in set

	c, err := Load("/no/such/bridge.yaml")
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	if len(c.Discord.Commands) != 2 {
		t.Fatalf("want 2 commands, got %+v", c.Discord.Commands)
	}
	if c.Discord.Commands[0].Trigger != "!players" || c.Discord.Commands[0].Rcon != "/players online" {
		t.Fatalf("cmd[0] parse: %+v", c.Discord.Commands[0])
	}
	if c.Discord.Commands[1].Trigger != "!evo" || c.Discord.Commands[1].Rcon != "/evolution" {
		t.Fatalf("cmd[1] trim/parse: %+v", c.Discord.Commands[1])
	}
}

func TestLoadFromEnvAdmins(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")
	t.Setenv("ODB_ADMIN_ROLES", "r1, r2")
	t.Setenv("ODB_ADMIN_USERS", "u1")

	c, err := Load("/no/such/bridge.yaml")
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	if len(c.Discord.Admins.Roles) != 2 || c.Discord.Admins.Roles[1] != "r2" {
		t.Fatalf("admin roles: %+v", c.Discord.Admins.Roles)
	}
	if len(c.Discord.Admins.Users) != 1 || c.Discord.Admins.Users[0] != "u1" {
		t.Fatalf("admin users: %+v", c.Discord.Admins.Users)
	}
	if !c.Discord.Admins.PermissionFallback() {
		t.Fatal("permission fallback should default to true")
	}
}

func TestLoadExpandsLinksAndSFTPPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ODB_TEST_HOME", dir)
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")

	cfgPath := filepath.Join(dir, "bridge.yaml")
	yaml := `
transport: sftp
factorio:
  rcon:
    address: "game:27015"
    password_env: FACTORIO_RCON_PASSWORD
  events_file: /tmp/events.jsonl
  links_file: "${ODB_TEST_HOME}/links.json"
  sftp:
    host: "example.com:22"
    user: bob
    key_path: "${ODB_TEST_HOME}/id_rsa"
    known_hosts_path: "${ODB_TEST_HOME}/known_hosts"
discord:
  token_env: DISCORD_BOT_TOKEN
  routes:
    - source: "*"
      channel_id: "12345"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	c, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := filepath.Join(dir, "links.json"); c.Factorio.LinksFile != want {
		t.Errorf("links_file not expanded: got %q, want %q", c.Factorio.LinksFile, want)
	}
	if want := filepath.Join(dir, "id_rsa"); c.Factorio.SFTP.KeyPath != want {
		t.Errorf("sftp.key_path not expanded: got %q, want %q", c.Factorio.SFTP.KeyPath, want)
	}
	if want := filepath.Join(dir, "known_hosts"); c.Factorio.SFTP.KnownHostsPath != want {
		t.Errorf("sftp.known_hosts_path not expanded: got %q, want %q", c.Factorio.SFTP.KnownHostsPath, want)
	}
}

func TestLoadFromEnvMissingTokenFails(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")
	t.Setenv("DISCORD_BOT_TOKEN", "") // missing

	if _, err := Load("/no/such/bridge.yaml"); err == nil {
		t.Fatal("expected error for missing Discord token")
	}
}

func TestEnvModeDefaultCommands(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")

	c, err := Load("/no/such/bridge.yaml")
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	byTrigger := make(map[string]Command, len(c.Discord.Commands))
	for _, cmd := range c.Discord.Commands {
		byTrigger[cmd.Trigger] = cmd
	}
	if len(byTrigger) != 6 {
		t.Fatalf("want 6 default commands, got %+v", c.Discord.Commands)
	}
	if cmd := byTrigger["!link"]; !cmd.Args || cmd.Admin || cmd.Rcon != "/odb-confirm-link {1} {userid} {user}" {
		t.Fatalf("!link default wrong: %+v", cmd)
	}
	if cmd := byTrigger["!unlink"]; !cmd.Args || cmd.Admin || cmd.Rcon != "/odb-unlink-discord {userid}" {
		t.Fatalf("!unlink default wrong: %+v", cmd)
	}
	for _, adminTrigger := range []string{"!links", "!unlink-player", "!unlink-all"} {
		if !byTrigger[adminTrigger].Admin {
			t.Fatalf("%s must be admin-gated: %+v", adminTrigger, byTrigger[adminTrigger])
		}
	}
	if cmd := byTrigger["!players"]; cmd.Admin || cmd.Rcon != "/players online" {
		t.Fatalf("!players default wrong: %+v", cmd)
	}
}

func TestEnvModeDefaultCommandsDisabled(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")
	t.Setenv("ODB_DEFAULT_COMMANDS", "false")

	c, err := Load("/no/such/bridge.yaml")
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	if len(c.Discord.Commands) != 0 {
		t.Fatalf("defaults should be off, got %+v", c.Discord.Commands)
	}
}

func TestEnvModeExplicitCommandOverridesDefault(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "12345")
	t.Setenv("ODB_COMMANDS", "!players=/players full")

	c, err := Load("/no/such/bridge.yaml")
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	if len(c.Discord.Commands) != 6 {
		t.Fatalf("want override + 5 remaining defaults, got %+v", c.Discord.Commands)
	}
	var players Command
	seen := 0
	for _, cmd := range c.Discord.Commands {
		if cmd.Trigger == "!players" {
			players, seen = cmd, seen+1
		}
	}
	if seen != 1 || players.Rcon != "/players full" {
		t.Fatalf("explicit !players must replace the default exactly once: %+v", c.Discord.Commands)
	}
}
