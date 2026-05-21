package discord

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// InboundFunc is called when a human posts in one of the bridged channels.
type InboundFunc func(user, message, channelID string)

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
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

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
	c.onMsg(m.Author.Username, m.Content, m.ChannelID)
}
