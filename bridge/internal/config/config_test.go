package config

import "testing"

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
