// Package wizard holds the reusable setup-flow logic for the Open Discord Bridge:
// validating a bot token, generating the invite URL, and listing guilds/channels. The
// CLI in cmd/wizard wraps it; a portal (e.g. AleForge) can embed the same library.
package wizard

import (
	"fmt"
	"net/url"

	"github.com/bwmarrin/discordgo"
)

// InvitePermissions: View Channels (1024) + Send Messages (2048) + Embed Links (16384) +
// Read Message History (65536) = 84992.
const InvitePermissions = "84992"

// Bot is a validated Discord bot connection (REST only — no gateway).
type Bot struct {
	session *discordgo.Session
	ID      string // application/bot ID (used as the invite client_id)
	Name    string
}

// Connect validates the token by fetching the bot user. Returns an error if the token is
// invalid or Discord is unreachable.
func Connect(token string) (*Bot, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	me, err := s.User("@me")
	if err != nil {
		return nil, fmt.Errorf("token rejected or Discord unreachable: %w", err)
	}
	return &Bot{session: s, ID: me.ID, Name: me.Username}, nil
}

// InviteURL is the OAuth2 URL to add this bot to a server with the permissions it needs.
func (b *Bot) InviteURL() string {
	v := url.Values{}
	v.Set("client_id", b.ID)
	v.Set("scope", "bot")
	v.Set("permissions", InvitePermissions)
	return "https://discord.com/oauth2/authorize?" + v.Encode()
}

type Guild struct {
	ID   string
	Name string
}

// Guilds lists the servers the bot is currently a member of.
func (b *Bot) Guilds() ([]Guild, error) {
	gs, err := b.session.UserGuilds(100, "", "", false)
	if err != nil {
		return nil, err
	}
	out := make([]Guild, 0, len(gs))
	for _, g := range gs {
		out = append(out, Guild{ID: g.ID, Name: g.Name})
	}
	return out, nil
}

type Channel struct {
	ID   string
	Name string
}

// TextChannels lists the text channels in a guild (where chat can be bridged).
func (b *Bot) TextChannels(guildID string) ([]Channel, error) {
	chs, err := b.session.GuildChannels(guildID)
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(chs))
	for _, ch := range chs {
		if ch.Type == discordgo.ChannelTypeGuildText {
			out = append(out, Channel{ID: ch.ID, Name: ch.Name})
		}
	}
	return out, nil
}
