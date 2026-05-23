package discord

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// InboundMessage is a human message in a bridged channel, with the author identity and
// role/permission context needed to authorize admin-only commands.
type InboundMessage struct {
	User      string   // display name
	UserID    string   // Discord user ID
	Roles     []string // role IDs the author holds
	Message   string
	ChannelID string
	IsAdmin   bool // author has Discord's Administrator permission
}

// InboundFunc is called when a human posts in one of the bridged channels.
type InboundFunc func(InboundMessage)

type Client struct {
	session   *discordgo.Session
	onMsg     InboundFunc
	connected bool

	mu      sync.RWMutex
	inbound map[string]bool
}

func New(token string, inboundChannels []string, onMsg InboundFunc) (*Client, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	// Guilds intent populates guild/role/channel state (needed to compute admin perms).
	s.Identify.Intents = discordgo.IntentGuilds | discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	c := &Client{session: s, onMsg: onMsg, inbound: map[string]bool{}}
	c.UpdateInbound(inboundChannels)
	s.AddHandler(c.handleMessage)
	return c, nil
}

// UpdateInbound atomically replaces the set of channels relayed back into the game.
func (c *Client) UpdateInbound(channels []string) {
	m := make(map[string]bool, len(channels))
	for _, ch := range channels {
		m[ch] = true
	}
	c.mu.Lock()
	c.inbound = m
	c.mu.Unlock()
}

func (c *Client) Open() error {
	if err := c.session.Open(); err != nil {
		return err
	}
	c.connected = true
	return nil
}

func (c *Client) Close() error {
	c.connected = false
	return c.session.Close()
}

// Connected reports whether the gateway connection is currently open.
func (c *Client) Connected() bool { return c.connected }

// Post sends a message and returns any error.
func (c *Client) Post(channelID, content string) error {
	_, err := c.session.ChannelMessageSend(channelID, content)
	return err
}

// Send is fire-and-forget (logs on failure) for the event relay path.
func (c *Client) Send(channelID, content string) {
	if err := c.Post(channelID, content); err != nil {
		fmt.Printf("discord: send to %s failed: %v\n", channelID, err)
	}
}

// SendEmbed posts an event as a colored embed (fire-and-forget).
func (c *Client) SendEmbed(channelID, description string, color int) {
	_, err := c.session.ChannelMessageSendEmbed(channelID, &discordgo.MessageEmbed{
		Description: description,
		Color:       color,
	})
	if err != nil {
		fmt.Printf("discord: embed send to %s failed: %v\n", channelID, err)
	}
}

// GuildInfo / ChannelInfo are lightweight projections for the Control API proxy.
type GuildInfo struct {
	ID   string
	Name string
}

type ChannelInfo struct {
	ID   string
	Name string
	Type int
}

// Guilds lists the guilds the bot is a member of.
func (c *Client) Guilds() ([]GuildInfo, error) {
	gs, err := c.session.UserGuilds(100, "", "", false)
	if err != nil {
		return nil, err
	}
	out := make([]GuildInfo, 0, len(gs))
	for _, g := range gs {
		out = append(out, GuildInfo{ID: g.ID, Name: g.Name})
	}
	return out, nil
}

// Channels lists the channels in a guild.
func (c *Client) Channels(guildID string) ([]ChannelInfo, error) {
	chs, err := c.session.GuildChannels(guildID)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelInfo, 0, len(chs))
	for _, ch := range chs {
		out = append(out, ChannelInfo{ID: ch.ID, Name: ch.Name, Type: int(ch.Type)})
	}
	return out, nil
}

func (c *Client) handleMessage(_ *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return // ignore bots, including ourselves — prevents echo loops
	}
	c.mu.RLock()
	inbound := c.inbound[m.ChannelID]
	c.mu.RUnlock()
	if !inbound {
		return
	}
	if m.Content == "" {
		return
	}
	var roles []string
	if m.Member != nil {
		roles = m.Member.Roles
	}
	c.onMsg(InboundMessage{
		User:      m.Author.Username,
		UserID:    m.Author.ID,
		Roles:     roles,
		Message:   m.Content,
		ChannelID: m.ChannelID,
		IsAdmin:   c.authorIsAdmin(m),
	})
}

// authorIsAdmin reports whether the message author has Discord's Administrator
// permission, computed from cached guild/role state + the roles in the message event
// (no privileged GuildMembers intent needed). Guild owner is always admin.
func (c *Client) authorIsAdmin(m *discordgo.MessageCreate) bool {
	if m.GuildID == "" {
		return false
	}
	g, err := c.session.State.Guild(m.GuildID)
	if err != nil {
		return false
	}
	if g.OwnerID == m.Author.ID {
		return true
	}
	roleIDs := append([]string{m.GuildID}, rolesOf(m)...) // include @everyone (== guildID)
	for _, id := range roleIDs {
		if role, err := c.session.State.Role(m.GuildID, id); err == nil {
			if role.Permissions&discordgo.PermissionAdministrator != 0 {
				return true
			}
		}
	}
	return false
}

func rolesOf(m *discordgo.MessageCreate) []string {
	if m.Member == nil {
		return nil
	}
	return m.Member.Roles
}
