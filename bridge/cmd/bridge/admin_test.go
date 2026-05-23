package main

import (
	"testing"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/config"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/discord"
)

func TestResolveAdmin(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		a    config.AdminConfig
		msg  discord.InboundMessage
		want bool
	}{
		{"user id match", config.AdminConfig{Users: []string{"u1"}}, discord.InboundMessage{UserID: "u1"}, true},
		{"role match", config.AdminConfig{Roles: []string{"r1"}}, discord.InboundMessage{Roles: []string{"r0", "r1"}}, true},
		{"permission fallback default-on", config.AdminConfig{}, discord.InboundMessage{IsAdmin: true}, true},
		{"permission disabled", config.AdminConfig{UseDiscordPermission: &no}, discord.InboundMessage{IsAdmin: true}, false},
		{"permission explicit-on", config.AdminConfig{UseDiscordPermission: &yes}, discord.InboundMessage{IsAdmin: true}, true},
		{"no match, perm off", config.AdminConfig{Users: []string{"u1"}, UseDiscordPermission: &no}, discord.InboundMessage{UserID: "u2", IsAdmin: true}, false},
	}
	for _, c := range cases {
		if got := resolveAdmin(c.a, c.msg); got != c.want {
			t.Errorf("%s: resolveAdmin = %v, want %v", c.name, got, c.want)
		}
	}
}
