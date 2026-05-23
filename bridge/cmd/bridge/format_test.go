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
		t.Error("death should be red")
	}
	if eventColor("mts.team_created") != eventColor("anything.else") {
		t.Error("non-vanilla events should share the neutral color")
	}
}

func TestFormatGenericFallsBackToKV(t *testing.T) {
	got := formatGeneric("oarc.spawn", map[string]any{"player": "bob", "x": float64(5)})
	want := "[oarc → spawn] player=bob, x=5"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
