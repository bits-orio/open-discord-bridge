package main

import "testing"

// TestNextAnnounce covers the connection-state transition logic behind fix #3: the very
// first observation must always announce (so a state consumer like the channel-topic
// status can seed itself even if Factorio is down at startup), while later observations
// only announce on an actual up/down transition.
func TestNextAnnounce(t *testing.T) {
	up, down := true, false
	cases := []struct {
		name         string
		last         *bool
		ok           bool
		wantAnnounce bool
		wantInitial  bool
	}{
		{"first observation, down", nil, false, true, true},
		{"first observation, up", nil, true, true, true},
		{"no change, still up", &up, true, false, false},
		{"no change, still down", &down, false, false, false},
		{"transition down to up", &down, true, true, false},
		{"transition up to down", &up, false, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotAnnounce, gotInitial := nextAnnounce(c.last, c.ok)
			if gotAnnounce != c.wantAnnounce || gotInitial != c.wantInitial {
				t.Errorf("nextAnnounce(%v, %v) = (%v, %v), want (%v, %v)",
					c.last, c.ok, gotAnnounce, gotInitial, c.wantAnnounce, c.wantInitial)
			}
		})
	}
}

// TestHandleZeroLinksRestoresOnceWhenPersistedStateExists covers fix #1: a fresh Factorio
// session that skipped the disconnect→reconnect transition reports zero links even though
// the bridge has persisted links on disk. handleZeroLinks should detect that, trigger a
// restore exactly once, and tell the caller to skip reconciling against the stale
// zero-links snapshot (which would otherwise strip everyone's role/nickname).
func TestHandleZeroLinksRestoresOnceWhenPersistedStateExists(t *testing.T) {
	dir := t.TempDir()
	ls := newLinksStore(dir + "/links.json")
	ls.upsert(linkInfo{Player: "Bob", DiscordID: "42", DiscordName: "bob#disc"})

	rc := newFakeRCON()
	restoredForGap := false

	// First poll after the gap: zero links reported, but we have one persisted — should
	// restore and tell the caller to skip this cycle.
	if skip := handleZeroLinks(rc, ls, nil, &restoredForGap); !skip {
		t.Fatal("want skip=true on first zero-links-with-persisted-state observation")
	}
	if !restoredForGap {
		t.Fatal("want restoredForGap=true after the first restore")
	}
	if got := rc.calls; len(got) != 1 || got[0] != "/odb-set-link Bob 42 bob#disc" {
		t.Fatalf("want exactly one restore RCON call, got %v", got)
	}

	// A second consecutive zero-links poll (restore hasn't landed yet, or genuinely
	// everyone unlinked) must NOT restore again — otherwise a real mass-unlink would never
	// settle and just keep re-restoring forever.
	if skip := handleZeroLinks(rc, ls, nil, &restoredForGap); !skip {
		t.Fatal("want skip=true on second zero-links observation (still guarded)")
	}
	if len(rc.calls) != 1 {
		t.Fatalf("want no additional restore RCON calls, got %v", rc.calls)
	}

	// Once links are reported non-zero again, the guard resets so a *future* gap can
	// trigger a restore again.
	if skip := handleZeroLinks(rc, ls, []linkInfo{{Player: "Bob", DiscordID: "42"}}, &restoredForGap); skip {
		t.Fatal("want skip=false once links are non-zero")
	}
	if restoredForGap {
		t.Fatal("want restoredForGap reset to false once links are non-zero")
	}
}

// TestHandleZeroLinksNoopWhenNothingPersisted covers the genuine "everyone really is
// unlinked" case: with no persisted links, zero mod-reported links is trusted as ground
// truth and syncLinkedMembers should proceed to reconcile (strip roles) normally.
func TestHandleZeroLinksNoopWhenNothingPersisted(t *testing.T) {
	ls := newLinksStore(t.TempDir() + "/links.json")
	rc := newFakeRCON()
	restoredForGap := false

	if skip := handleZeroLinks(rc, ls, nil, &restoredForGap); skip {
		t.Fatal("want skip=false when nothing is persisted (zero links is trusted)")
	}
	if len(rc.calls) != 0 {
		t.Fatalf("want no RCON calls, got %v", rc.calls)
	}
}
