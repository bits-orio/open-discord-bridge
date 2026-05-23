package main

import (
	"reflect"
	"testing"
)

func TestCommandArgs(t *testing.T) {
	if got := commandArgs("!kick Bob now"); !reflect.DeepEqual(got, []string{"Bob", "now"}) {
		t.Fatalf("got %q", got)
	}
	if got := commandArgs("!players"); got != nil {
		t.Fatalf("expected nil, got %q", got)
	}
}

func TestInterpolate(t *testing.T) {
	cases := []struct {
		tmpl   string
		args   []string
		user   string
		userID string
		want   string
	}{
		{"/ban {1}", []string{"Bob"}, "alice", "42", "/ban Bob"},
		{"/say {args}", []string{"hello", "there"}, "alice", "42", "/say hello there"},
		{"/whisper {user}: {args}", []string{"hi"}, "alice", "42", "/whisper alice: hi"},
		{"/odb-confirm-link {1} {userid} {user}", []string{"AB12CD"}, "alice", "42", "/odb-confirm-link AB12CD 42 alice"},
		{"/x {2}", []string{"a"}, "u", "1", "/x "},     // missing positional -> empty
		{"/x {bogus}", []string{"a"}, "u", "1", "/x "}, // unknown token -> empty
	}
	for _, c := range cases {
		if got := interpolate(c.tmpl, c.args, c.user, c.userID); got != c.want {
			t.Errorf("interpolate(%q,%q,%q,%q) = %q; want %q", c.tmpl, c.args, c.user, c.userID, got, c.want)
		}
	}
}

func TestInterpolateStripsInjection(t *testing.T) {
	// A newline in user input must not survive (no second RCON line).
	got := interpolate("/say {args}", []string{"hi\n/promote evil"}, "u", "1")
	if got != "/say hi/promote evil" {
		t.Fatalf("newline not stripped: %q", got)
	}
}
