package main

import "testing"

// resolve must return the IDs it rewrote: outbound sends are deny-all by default, so a
// rewritten <@id> only pings when the caller allowlists these IDs via PostMentioning.
func TestResolveReturnsResolvedIDs(t *testing.T) {
	mr := &mentionResolver{}
	mr.update([]linkInfo{
		{Player: "alice", DiscordID: "111"},
		{Player: "bob", DiscordID: "222"},
	})

	out, ids := mr.resolve("hi @alice and @bob and @alice again, @stranger")
	if want := "hi <@111> and <@222> and <@111> again, @stranger"; out != want {
		t.Fatalf("resolve = %q, want %q", out, want)
	}
	if len(ids) != 2 || ids[0] != "111" || ids[1] != "222" {
		t.Fatalf("resolved IDs = %v, want deduped [111 222]", ids)
	}
}

func TestResolveNoMentionsReturnsNilIDs(t *testing.T) {
	mr := &mentionResolver{}
	mr.update([]linkInfo{{Player: "alice", DiscordID: "111"}})

	out, ids := mr.resolve("plain message, @unlinked too")
	if out != "plain message, @unlinked too" {
		t.Fatalf("resolve rewrote something it shouldn't: %q", out)
	}
	if ids != nil {
		t.Fatalf("expected nil IDs, got %v", ids)
	}
}
