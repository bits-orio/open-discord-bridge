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
		tmpl string
		args []string
		user string
		want string
	}{
		{"/ban {1}", []string{"Bob"}, "alice", "/ban Bob"},
		{"/say {args}", []string{"hello", "there"}, "alice", "/say hello there"},
		{"/whisper {user}: {args}", []string{"hi"}, "alice", "/whisper alice: hi"},
		{"/x {2}", []string{"a"}, "u", "/x "},          // missing positional -> empty
		{"/x {bogus}", []string{"a"}, "u", "/x "},      // unknown token -> empty
	}
	for _, c := range cases {
		if got := interpolate(c.tmpl, c.args, c.user); got != c.want {
			t.Errorf("interpolate(%q,%q,%q) = %q; want %q", c.tmpl, c.args, c.user, got, c.want)
		}
	}
}

func TestInterpolateStripsInjection(t *testing.T) {
	// A newline in user input must not survive (no second RCON line).
	got := interpolate("/say {args}", []string{"hi\n/promote evil"}, "u")
	if got != "/say hi/promote evil" {
		t.Fatalf("newline not stripped: %q", got)
	}
}
