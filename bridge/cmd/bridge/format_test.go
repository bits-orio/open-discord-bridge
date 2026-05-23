package main

import "testing"

func TestHumanizeKey(t *testing.T) {
	cases := map[string]string{
		"mts.team_created":       "[mts → team created]",
		"mts.player_joined_team": "[mts → player joined team]",
		"custom":                 "[custom]",
	}
	for in, want := range cases {
		if got := humanizeKey(in); got != want {
			t.Errorf("humanizeKey(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestFormatGenericPrefersText(t *testing.T) {
	got := formatGeneric("mts.player_joined_team", map[string]any{
		"force_name": "team-1",
		"player":     "bits-orio",
		"team":       "Team 01",
		"text":       "bits-orio joined Team 01",
	})
	want := "[mts → player joined team] bits-orio joined Team 01"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestFirstWord(t *testing.T) {
	cases := map[string]string{
		"!players":          "!players",
		"!players now":      "!players",
		"  !evo  arg ":      "!evo",
		"":                  "",
		"   ":               "",
		"hello world there": "hello",
	}
	for in, want := range cases {
		if got := firstWord(in); got != want {
			t.Errorf("firstWord(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestEventColor(t *testing.T) {
	if eventColor("vanilla.player_died") != 0xED4245 {
		t.Error("death should keep its intentional red")
	}
	// Non-vanilla events get a stable, distinct color per key (no hardcoding).
	if eventColor("mts.team_created") != eventColor("mts.team_created") {
		t.Error("color must be deterministic for a given key")
	}
	seen := map[int]bool{}
	for _, k := range []string{"mts.team_created", "mts.team_released", "oarc.spawn", "custom.x"} {
		c := eventColor(k)
		if c < 0 || c > 0xFFFFFF {
			t.Errorf("color out of range for %q: %#x", k, c)
		}
		seen[c] = true
	}
	if len(seen) < 2 {
		t.Error("distinct event keys should map to distinct colors")
	}
}

func TestFormatGenericFallsBackToKV(t *testing.T) {
	got := formatGeneric("oarc.spawn", map[string]any{"player": "bob", "x": float64(5)})
	want := "[oarc → spawn] player=bob, x=5"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
