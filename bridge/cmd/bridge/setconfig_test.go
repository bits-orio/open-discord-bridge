package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/config"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/controlapi"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/discord"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/router"
)

// TestSetConfigPersistsRoutesToFile covers fix #4 end-to-end: a POST /v1/config routing
// change must be written back to the config file it was loaded from, not just applied live
// — otherwise a restart silently reverts to the old routing.
func TestSetConfigPersistsRoutesToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.yaml")
	original := `factorio:
  rcon:
    address: game:27015
    password_env: FACTORIO_RCON_PASSWORD
  events_file: /tmp/events.jsonl
discord:
  token_env: DISCORD_BOT_TOKEN
  routes:
    - source: "*"
      channel_id: "111"
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rt := router.New(toRouterRoutes(cfg.Discord.Routes))
	dc, err := discord.New("faketoken", rt.InboundChannels(), func(discord.InboundMessage) {})
	if err != nil {
		t.Fatalf("discord.New: %v", err)
	}

	persisted, err := setConfig(cfg, rt, dc, controlapi.Config{
		Discord: controlapi.ConfigDiscord{
			Routes: []controlapi.Route{{Source: "vanilla.chat", ChannelID: "999"}},
		},
	})
	if err != nil {
		t.Fatalf("setConfig: %v", err)
	}
	if !persisted {
		t.Fatal("want persisted=true for a file-based config")
	}

	// The live router must reflect the change immediately...
	if ch, ok := rt.Channel("vanilla.chat"); !ok || ch != "999" {
		t.Fatalf("router not updated live: %v %v", ch, ok)
	}

	// ...and it must survive a fresh reload from disk.
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Discord.Routes) != 1 || reloaded.Discord.Routes[0].ChannelID != "999" {
		t.Fatalf("reloaded routes not persisted: %+v", reloaded.Discord.Routes)
	}
}

// TestSetConfigEnvVarModeNotPersisted covers the case where the bridge has no config file
// to write to (env-var config mode): the change should still apply live (so the request
// isn't a hard failure) but report persisted=false, so a caller/UI can warn that it's
// memory-only until the env vars are updated too.
func TestSetConfigEnvVarModeNotPersisted(t *testing.T) {
	t.Setenv("ODB_RCON_ADDRESS", "game:27015")
	t.Setenv("FACTORIO_RCON_PASSWORD", "pw")
	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("ODB_EVENTS_FILE", "/tmp/events.jsonl")
	t.Setenv("ODB_DISCORD_CHANNEL_ID", "111")

	cfg, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("env load: %v", err)
	}
	if cfg.FilePath != "" {
		t.Fatalf("expected env-var mode to have no FilePath, got %q", cfg.FilePath)
	}

	rt := router.New(toRouterRoutes(cfg.Discord.Routes))
	dc, err := discord.New("faketoken", rt.InboundChannels(), func(discord.InboundMessage) {})
	if err != nil {
		t.Fatalf("discord.New: %v", err)
	}

	persisted, err := setConfig(cfg, rt, dc, controlapi.Config{
		Discord: controlapi.ConfigDiscord{
			Routes: []controlapi.Route{{Source: "*", ChannelID: "222"}},
		},
	})
	if err != nil {
		t.Fatalf("setConfig: %v", err)
	}
	if persisted {
		t.Fatal("want persisted=false in env-var config mode")
	}
	// Still applied live.
	if ch, ok := rt.Channel("anything"); !ok || ch != "222" {
		t.Fatalf("router not updated live: %v %v", ch, ok)
	}
}

// TestSetConfigRejectsImmutableFields is a lightweight regression check that the existing
// runtime-immutability guards (transport/events_file/rcon address) survived the (persisted
// bool, error) signature change.
func TestSetConfigRejectsImmutableFields(t *testing.T) {
	cfg := &config.Config{Transport: "local", Factorio: config.FactorioConfig{EventsFile: "/tmp/e.jsonl"}}
	rt := router.New(nil)
	dc, err := discord.New("faketoken", nil, func(discord.InboundMessage) {})
	if err != nil {
		t.Fatalf("discord.New: %v", err)
	}

	_, err = setConfig(cfg, rt, dc, controlapi.Config{Transport: "sftp"})
	if err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("want transport-immutable error, got %v", err)
	}
}
