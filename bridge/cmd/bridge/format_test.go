package main

import (
	"strings"
	"testing"
)

func TestIsChatEvent(t *testing.T) {
	chat := []string{"chat", "vanilla.chat", "mts.chat", "oarc.chat"}
	notChat := []string{"vanilla.player_joined", "mts.team_created", "bridge.established", "chatter", "x.chatroom"}
	for _, k := range chat {
		if !isChatEvent(k) {
			t.Errorf("%q should be chat", k)
		}
	}
	for _, k := range notChat {
		if isChatEvent(k) {
			t.Errorf("%q should NOT be chat", k)
		}
	}
}

func TestFormatEmbed(t *testing.T) {
	// Generic event: label becomes the title, sentence the body.
	title, desc := formatEmbed(Event{
		Event: "mts.milestone_first",
		Data:  map[string]any{"text": "Team 01 was the first to produce automation-science-pack"},
	})
	if title != "mts → milestone first" {
		t.Errorf("title = %q", title)
	}
	if desc != "Team 01 was the first to produce automation-science-pack" {
		t.Errorf("desc = %q", desc)
	}
	// Vanilla event: no title, the one-liner is the body.
	title, desc = formatEmbed(Event{Event: "vanilla.player_joined", Data: map[string]any{"player": "Bob", "online_count": float64(1)}})
	if title != "" || !strings.Contains(desc, "Bob") {
		t.Errorf("vanilla embed: title=%q desc=%q", title, desc)
	}
}

func TestBridgeStatusFormatting(t *testing.T) {
	if eventColor("bridge.established") != 0x57F287 {
		t.Error("established should be green")
	}
	if eventColor("bridge.disconnected") != 0xED4245 {
		t.Error("disconnected should be red")
	}
	got := formatEvent(Event{Event: "bridge.established", Data: map[string]any{"version": "0.1.0"}})
	if !strings.Contains(got, "established") || !strings.Contains(got, "0.1.0") {
		t.Errorf("established message wrong: %q", got)
	}
}

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
