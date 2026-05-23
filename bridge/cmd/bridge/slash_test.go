package main

import (
	"testing"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/config"
)

func TestSlashName(t *testing.T) {
	cases := map[string]string{
		"!players":  "players",
		"!ban":      "ban",
		"!evo-rate": "evo-rate",
		"!":         "",
	}
	for in, want := range cases {
		if got := slashName(in); got != want {
			t.Errorf("slashName(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestBuildSlashDedupAndFlags(t *testing.T) {
	specs, byName := buildSlash([]config.Command{
		{Trigger: "!players", Rcon: "/players online"},
		{Trigger: "players", Rcon: "/dup"}, // same slash name -> skipped
		{Trigger: "!ban", Rcon: "/ban {1}", Admin: true, Args: true},
	})
	if len(specs) != 2 {
		t.Fatalf("want 2 specs, got %d", len(specs))
	}
	if c := byName["ban"]; !c.Admin || !c.Args {
		t.Fatalf("ban command flags lost: %+v", c)
	}
	if byName["players"].Rcon != "/players online" {
		t.Fatalf("dedup should keep the first, got %q", byName["players"].Rcon)
	}
}
