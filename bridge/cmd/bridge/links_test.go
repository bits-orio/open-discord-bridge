package main

import "testing"

func TestParseLinks(t *testing.T) {
	if got := parseLinks(`{"links":[]}`); len(got) != 0 {
		t.Errorf("empty array: got %v", got)
	}
	if got := parseLinks(`{"links":{}}`); len(got) != 0 {
		t.Errorf("empty object (Factorio empty table): got %v", got)
	}
	got := parseLinks(`{"links":[{"discord_id":"42","player":"Bob","discord_name":"bits-orio"}]}`)
	if len(got) != 1 || got[0].DiscordID != "42" || got[0].Player != "Bob" || got[0].DiscordName != "bits-orio" {
		t.Fatalf("parse: %+v", got)
	}
}

func TestNickFor(t *testing.T) {
	l := linkInfo{Player: "Bob", DiscordName: "bits-orio"}
	if got := nickFor("{factorio}", l); got != "Bob" {
		t.Errorf("got %q", got)
	}
	if got := nickFor("{discord} | {factorio}", l); got != "bits-orio | Bob" {
		t.Errorf("got %q", got)
	}
	long := nickFor("{discord}", linkInfo{DiscordName: "0123456789012345678901234567890123456789"})
	if len(long) != 32 {
		t.Errorf("nickname should be capped at 32, got %d", len(long))
	}
}
