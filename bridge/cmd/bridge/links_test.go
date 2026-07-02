package main

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/discord"
)

func TestPermissionHelp(t *testing.T) {
	msg := permissionHelp(discord.PermissionReport{
		Missing:   []string{"Manage Roles", "Manage Nicknames"},
		Hierarchy: true,
	})
	for _, want := range []string{
		"Manage Roles, Manage Nicknames", // listed
		"Server Settings → Roles",        // step 1
		"drag the bot's role above",      // hierarchy step
		"1.", "2.", "3.",                 // numbered steps
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("permission help missing %q\n---\n%s", want, msg)
		}
	}
}

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

func TestNickForTruncatesOnRuneBoundary(t *testing.T) {
	// "é" is 2 bytes (0xC3 0xA9); repeating it 20x is 40 bytes, so a plain byte-slice cap
	// at 32 would land mid-rune. The result must stay valid UTF-8, capped at or under 32.
	name := strings.Repeat("é", 20)
	got := nickFor("{discord}", linkInfo{DiscordName: name})
	if !utf8.ValidString(got) {
		t.Fatalf("truncated nickname is not valid UTF-8: %q", got)
	}
	if len(got) > 32 {
		t.Fatalf("truncated nickname exceeds 32 bytes: %d", len(got))
	}
}

func TestLinksStoreSaveLoadRoundTrip(t *testing.T) {
	path := t.TempDir() + "/links.json"
	ls := newLinksStore(path)
	ls.upsert(linkInfo{Player: "Bob", DiscordID: "42", DiscordName: "bits-orio"})

	// save() writes via a temp file + fsync + rename; the temp file must not linger.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file should not exist after rename: err=%v", err)
	}

	reloaded := newLinksStore(path)
	reloaded.load()
	got := reloaded.all()
	if len(got) != 1 || got[0].Player != "Bob" || got[0].DiscordID != "42" {
		t.Fatalf("round trip: got %+v", got)
	}
}

func TestMentionResolverCaseInsensitive(t *testing.T) {
	mr := &mentionResolver{}
	mr.update([]linkInfo{{Player: "Bits-Orio", DiscordID: "42"}})

	for _, in := range []string{"hi @Bits-Orio", "hi @bits-orio", "hi @BITS-ORIO"} {
		got, ids := mr.resolve(in)
		if got != "hi <@42>" {
			t.Errorf("resolve(%q) = %q; want %q", in, got, "hi <@42>")
		}
		if len(ids) != 1 || ids[0] != "42" {
			t.Errorf("resolve(%q) ids = %v; want [42]", in, ids)
		}
	}
}

func TestTruncateUTF8(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},            // shorter than limit -> unchanged
		{"hello world", 5, "hello"},       // ASCII cuts cleanly
		{strings.Repeat("é", 3), 5, "éé"}, // "ééé" is 6 bytes; cutting at 5 would split the 3rd é, so back off to 4
	}
	for _, c := range cases {
		got := truncateUTF8(c.in, c.n)
		if got != c.want {
			t.Errorf("truncateUTF8(%q, %d) = %q; want %q", c.in, c.n, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncateUTF8(%q, %d) produced invalid UTF-8: %q", c.in, c.n, got)
		}
	}
}
