package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/rcon"
)

func TestIncomingCommands_ShortMessageIsOneCommand(t *testing.T) {
	cmds := incomingCommands("Bob", "123", "hello", "chan1")
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	if !strings.HasPrefix(cmds[0], "/odb-incoming ") {
		t.Errorf("command = %q, want /odb-incoming prefix", cmds[0])
	}
}

func TestIncomingCommands_LongMessageIsSplitWithinBudget(t *testing.T) {
	long := strings.Repeat("hello world ", 200) // well over MaxCommandLen once wrapped
	cmds := incomingCommands("Bob", "1234567890", long, "chan1")

	if len(cmds) < 2 {
		t.Fatalf("got %d commands, want > 1 for a long message", len(cmds))
	}

	var rebuilt strings.Builder
	for _, cmd := range cmds {
		if len(cmd) > rcon.MaxCommandLen {
			t.Errorf("command length %d exceeds MaxCommandLen %d: %q", len(cmd), rcon.MaxCommandLen, cmd)
		}
		if !strings.HasPrefix(cmd, "/odb-incoming ") {
			t.Fatalf("command = %q, want /odb-incoming prefix", cmd)
		}
		payload := strings.TrimPrefix(cmd, "/odb-incoming ")
		var args struct {
			User    string `json:"user"`
			UserID  string `json:"user_id"`
			Message string `json:"message"`
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal([]byte(payload), &args); err != nil {
			t.Fatalf("command payload is not valid JSON: %v (%q)", err, payload)
		}
		if args.User != "Bob" || args.UserID != "1234567890" || args.Channel != "chan1" {
			t.Errorf("chunk metadata changed: got %+v", args)
		}
		rebuilt.WriteString(args.Message)
	}

	if rebuilt.String() != long {
		t.Errorf("rebuilt message does not match original.\ngot:  %q\nwant: %q", rebuilt.String(), long)
	}
}

func TestIncomingCommands_HandlesJSONEscapedContent(t *testing.T) {
	// Quotes, backslashes and newlines expand when JSON-escaped, so a naive rune-count
	// split could still overflow MaxCommandLen after marshaling.
	long := strings.Repeat(`say "hi" \ back-slash and newline`+"\n", 60)
	cmds := incomingCommands("Bob", "1", long, "chan1")

	if len(cmds) == 0 {
		t.Fatal("got 0 commands")
	}
	for _, cmd := range cmds {
		if len(cmd) > rcon.MaxCommandLen {
			t.Errorf("command length %d exceeds MaxCommandLen %d", len(cmd), rcon.MaxCommandLen)
		}
	}
}
