package wizard

import (
	"strings"
	"testing"
)

func TestRenderBridgeYAML(t *testing.T) {
	got, err := RenderBridgeYAML(ConfigParams{
		RconAddress: "127.0.0.1:27015",
		EventsFile:  "/x/events.jsonl",
		GuildID:     "123456789012345678",
		ChannelID:   "234567890123456789",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"address: 127.0.0.1:27015",
		"events_file: /x/events.jsonl",
		`guild_id: "123456789012345678"`,
		`channel_id: "234567890123456789"`,
		"token_env: DISCORD_BOT_TOKEN",
		`trigger: "!players"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, got)
		}
	}
}

func TestInviteURL(t *testing.T) {
	b := &Bot{ID: "12345", Name: "ODB"}
	got := b.InviteURL()
	for _, want := range []string{
		"https://discord.com/oauth2/authorize?",
		"client_id=12345",
		"scope=bot",
		"applications.commands",
		"permissions=" + InvitePermissions,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("invite URL %q missing %q", got, want)
		}
	}
}
