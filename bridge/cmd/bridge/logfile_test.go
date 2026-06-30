package main

import (
	"path/filepath"
	"testing"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/config"
)

func TestResolveLogPath(t *testing.T) {
	mk := func(transport, eventsFile, logFile string) *config.Config {
		c := &config.Config{Transport: transport, LogFile: logFile}
		c.Factorio.EventsFile = eventsFile
		return c
	}
	eventsDir := filepath.Join("/data", "script-output", "open-discord-bridge")

	cases := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{"explicit path wins", mk("local", filepath.Join(eventsDir, "events.jsonl"), "/var/log/odb.log"), "/var/log/odb.log"},
		{"default next to events (local)", mk("local", filepath.Join(eventsDir, "events.jsonl"), ""), filepath.Join(eventsDir, "bridge.log")},
		{"dash disables the file", mk("local", filepath.Join(eventsDir, "events.jsonl"), "-"), ""},
		{"sftp default is stderr-only (remote events path)", mk("sftp", "script-output/open-discord-bridge/events.jsonl", ""), ""},
		{"sftp honors an explicit local path", mk("sftp", "remote/events.jsonl", "/local/odb.log"), "/local/odb.log"},
		{"no events file, no default", mk("local", "", ""), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveLogPath(tc.cfg); got != tc.want {
				t.Errorf("resolveLogPath() = %q, want %q", got, tc.want)
			}
		})
	}
}
