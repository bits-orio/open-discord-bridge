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

func TestRenderBridgeYAML_SFTP_Password(t *testing.T) {
	got, err := RenderBridgeYAML(ConfigParams{
		Transport:   "sftp",
		RconAddress: "play.example.net:32410",
		EventsFile:  "script-output/open-discord-bridge/events.jsonl",
		GuildID:     "111",
		ChannelID:   "222",
		SFTPHost:    "panel.example.net:2022",
		SFTPUser:    "alice.abcd1234",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"transport: sftp",
		"poll_interval: 2s",
		"address: play.example.net:32410",
		"events_file: script-output/open-discord-bridge/events.jsonl",
		"host: panel.example.net:2022",
		"user: alice.abcd1234",
		"password_env: SFTP_PASSWORD",
		`known_hosts_path: ""`,
		", SFTP password) live in .env",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "key_path:") {
		t.Errorf("password mode should not emit key_path\n---\n%s", got)
	}
}

func TestRenderBridgeYAML_SFTP_Key(t *testing.T) {
	got, err := RenderBridgeYAML(ConfigParams{
		Transport:      "sftp",
		RconAddress:    "play.example.net:32410",
		EventsFile:     "script-output/open-discord-bridge/events.jsonl",
		GuildID:        "111",
		ChannelID:      "222",
		SFTPHost:       "panel.example.net:2022",
		SFTPUser:       "alice.abcd1234",
		SFTPKeyPath:    "/secrets/id_ed25519",
		SFTPKnownHosts: "/secrets/known_hosts",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"transport: sftp",
		"key_path: /secrets/id_ed25519",
		"known_hosts_path: /secrets/known_hosts",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "password_env: SFTP_PASSWORD") {
		t.Errorf("key mode should not emit password_env\n---\n%s", got)
	}
	if strings.Contains(got, ", SFTP password) live in .env") {
		t.Errorf("key mode header should not mention SFTP password\n---\n%s", got)
	}
}

func TestRenderBridgeYAML_DefaultsToLocal(t *testing.T) {
	got, err := RenderBridgeYAML(ConfigParams{
		RconAddress: "127.0.0.1:27015",
		EventsFile:  "/x/events.jsonl",
		GuildID:     "111",
		ChannelID:   "222",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(got, "transport: local") {
		t.Errorf("empty Transport should render local\n---\n%s", got)
	}
	if strings.Contains(got, "sftp:") {
		t.Errorf("local mode should not emit sftp block\n---\n%s", got)
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
