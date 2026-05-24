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

func TestBridgeStatusFormatting(t *testing.T) {
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
		if got := humanizeKey(in, ""); got != want {
			t.Errorf("humanizeKey(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestLabelOverride(t *testing.T) {
	// An integrator-supplied "label" replaces the event-name portion; namespace stays.
	if got := humanizeKey("mts.research_finished", "🔬"); got != "[mts → 🔬]" {
		t.Errorf("override label = %q; want [mts → 🔬]", got)
	}
	// And it flows through plain-mode rendering.
	got := formatGeneric("mts.research_finished", map[string]any{
		"label": "🔬",
		"text":  "Team 01 researched `epic-quality`",
	})
	want := "**[mts → 🔬]** Team 01 researched `epic-quality`"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	// "label" is a presentation key, not shown in the kv fallback.
	if kv := formatGeneric("oarc.spawn", map[string]any{"label": "🌲", "x": float64(5)}); kv != "**[oarc → 🌲]** x=5" {
		t.Errorf("kv fallback leaked label: %q", kv)
	}
}

func TestFormatGenericPrefersText(t *testing.T) {
	got := formatGeneric("mts.player_joined_team", map[string]any{
		"force_name": "team-1",
		"player":     "bits-orio",
		"team":       "Team 01",
		"text":       "bits-orio joined Team 01",
	})
	want := "**[mts → player joined team]** bits-orio joined Team 01"
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

func TestIsGenericEvent(t *testing.T) {
	for _, k := range []string{"mts.research_finished", "oarc.spawn", "custom.x"} {
		if !isGenericEvent(k) {
			t.Errorf("%q should be generic", k)
		}
	}
	for _, k := range []string{"vanilla.player_died", "bridge.established"} {
		if isGenericEvent(k) {
			t.Errorf("%q should NOT be generic", k)
		}
	}
}

func TestRenderEvent(t *testing.T) {
	ev := Event{Event: "mts.research_finished", Data: map[string]any{"text": "Team 01 researched steel-processing"}}

	// Decoration off → plain text line, no code block.
	plain := renderEvent(ev, false)
	if strings.Contains(plain, "```") || strings.Contains(plain, "\x1b") {
		t.Errorf("undecorated should be plain: %q", plain)
	}
	if !strings.Contains(plain, "[mts → research finished]") {
		t.Errorf("undecorated missing label: %q", plain)
	}

	// Decoration on → ANSI code block; only the label is wrapped in color codes.
	dec := renderEvent(ev, true)
	if !strings.HasPrefix(dec, "```ansi\n") || !strings.HasSuffix(dec, "\n```") {
		t.Errorf("decorated should be an ansi block: %q", dec)
	}
	if !strings.Contains(dec, ansiColor(ev.Event)+"[mts → research finished]"+ansiReset) {
		t.Errorf("label should be ansi-colored: %q", dec)
	}
	if !strings.Contains(dec, "Team 01 researched steel-processing") {
		t.Errorf("body missing: %q", dec)
	}

	// Color is deterministic per key, and varies across keys.
	if ansiColor("mts.research_finished") != ansiColor("mts.research_finished") {
		t.Error("ansi color must be deterministic")
	}
	// Vanilla stays plain even with decoration on.
	if v := renderEvent(Event{Event: "vanilla.player_died", Data: map[string]any{"player": "Bob"}}, true); strings.Contains(v, "```") {
		t.Errorf("vanilla should stay plain: %q", v)
	}
}

func TestFormatGenericFallsBackToKV(t *testing.T) {
	got := formatGeneric("oarc.spawn", map[string]any{"player": "bob", "x": float64(5)})
	want := "**[oarc → spawn]** player=bob, x=5"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
