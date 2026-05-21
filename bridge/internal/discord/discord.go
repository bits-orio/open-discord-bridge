package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// InboundFunc is called when a human posts in one of the bridged channels.
type InboundFunc func(user, message, channelID string)

type Client struct {
	session *discordgo.Session
	inbound map[string]bool
	onMsg   InboundFunc
}

func New(token string, inboundChannels []string, onMsg InboundFunc) (*Client, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	c := &Client{session: s, inbound: map[string]bool{}, onMsg: onMsg}
	for _, ch := range inboundChannels {
		c.inbound[ch] = true
	}
	s.AddHandler(c.handleMessage)
	return c, nil
}

func (c *Client) Open() error  { return c.session.Open() }
func (c *Client) Close() error { return c.session.Close() }

// Send posts a plain message to a channel.
func (c *Client) Send(channelID, content string) {
	if _, err := c.session.ChannelMessageSend(channelID, content); err != nil {
		fmt.Printf("discord: send to %s failed: %v\n", channelID, err)
	}
}

func (c *Client) handleMessage(_ *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return // ignore bots, including ourselves — prevents echo loops
	}
	if !c.inbound[m.ChannelID] {
		return
	}
	if m.Content == "" {
		return
	}
	c.onMsg(m.Author.Username, m.Content, m.ChannelID)
}
