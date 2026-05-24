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

	guildID    string
	slashSpecs []SlashSpec
	onSlash    SlashFunc
}

// SlashSpec describes a slash command to register. Admin gates it via Discord's own
// default_member_permissions; TakesArgs adds a required "args" string option.
type SlashSpec struct {
	Name        string
	Description string
	Admin       bool
	TakesArgs   bool
}

// SlashInvocation is a slash command invocation handed to the SlashFunc.
type SlashInvocation struct {
	Name    string
	Args    string
	User    string
	UserID  string
	Roles   []string
	IsAdmin bool
}

// SlashFunc handles a slash command and returns the reply text ("" → a generic "Done.").
type SlashFunc func(SlashInvocation) string

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

// EnableSlashCommands registers the interaction handler now; the commands themselves are
// registered with Discord after Open (when the application ID is known). Call before Open.
func (c *Client) EnableSlashCommands(guildID string, specs []SlashSpec, onSlash SlashFunc) {
	c.guildID = guildID
	c.slashSpecs = specs
	c.onSlash = onSlash
	c.session.AddHandler(c.handleInteraction)
}

func (c *Client) Open() error {
	if err := c.session.Open(); err != nil {
		return err
	}
	c.connected = true
	if len(c.slashSpecs) > 0 && c.guildID != "" {
		if err := c.registerSlash(); err != nil {
			fmt.Printf("discord: slash command registration failed: %v\n", err)
		}
	}
	return nil
}

func (c *Client) registerSlash() error {
	appID := c.session.State.User.ID
	cmds := make([]*discordgo.ApplicationCommand, 0, len(c.slashSpecs))
	for _, sp := range c.slashSpecs {
		ac := &discordgo.ApplicationCommand{Name: sp.Name, Description: sp.Description}
		if sp.Admin {
			perm := int64(discordgo.PermissionAdministrator)
			ac.DefaultMemberPermissions = &perm
		}
		if sp.TakesArgs {
			ac.Options = []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "args",
				Description: "arguments",
				Required:    true,
			}}
		}
		cmds = append(cmds, ac)
	}
	_, err := c.session.ApplicationCommandBulkOverwrite(appID, c.guildID, cmds)
	return err
}

func (c *Client) handleInteraction(_ *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand || c.onSlash == nil {
		return
	}
	data := i.ApplicationCommandData()

	var args string
	for _, opt := range data.Options {
		if opt.Name == "args" {
			args = opt.StringValue()
		}
	}

	inv := SlashInvocation{Name: data.Name, Args: args}
	if i.Member != nil {
		inv.Roles = i.Member.Roles
		inv.IsAdmin = i.Member.Permissions&discordgo.PermissionAdministrator != 0
		if i.Member.User != nil {
			inv.UserID = i.Member.User.ID
			inv.User = i.Member.User.Username
			if i.Member.User.GlobalName != "" {
				inv.User = i.Member.User.GlobalName
			}
			if i.Member.Nick != "" {
				inv.User = i.Member.Nick
			}
		}
	}

	reply := c.onSlash(inv)
	if reply == "" {
		reply = "Done."
	}
	_ = c.session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: reply},
	})
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

// PermissionCheck describes what the bridge needs the bot to be able to do.
type PermissionCheck struct {
	GuildID     string
	NeedEmbed   bool
	NeedRoles   bool   // Manage Roles (linked_role_id)
	NeedNicks   bool   // Manage Nicknames (linked_nickname)
	RoleAboveID string // bot's top role must outrank this role (the linked role)
}

// PermissionReport is the result of a permission preflight.
type PermissionReport struct {
	Missing   []string // names of permissions the bot lacks (e.g. "Manage Roles")
	Hierarchy bool     // bot's highest role is not above the linked role
	Err       string   // set if the check itself could not run
}

// OK reports whether nothing needs fixing.
func (r PermissionReport) OK() bool { return len(r.Missing) == 0 && !r.Hierarchy && r.Err == "" }

// CheckPermissions runs a guild-level preflight via REST (doesn't account for per-channel
// overwrites). Owner/Administrator short-circuit to an OK report.
func (c *Client) CheckPermissions(pc PermissionCheck) PermissionReport {
	if pc.GuildID == "" {
		return PermissionReport{}
	}
	botID := c.session.State.User.ID
	g, err := c.session.Guild(pc.GuildID)
	if err != nil {
		return PermissionReport{Err: err.Error()}
	}
	if g.OwnerID == botID {
		return PermissionReport{}
	}
	roles, err := c.session.GuildRoles(pc.GuildID)
	if err != nil {
		return PermissionReport{Err: err.Error()}
	}
	byID := make(map[string]*discordgo.Role, len(roles))
	for _, r := range roles {
		byID[r.ID] = r
	}
	member, err := c.session.GuildMember(pc.GuildID, botID)
	if err != nil {
		return PermissionReport{Err: err.Error()}
	}

	var perms int64
	botTop := 0
	if everyone := byID[pc.GuildID]; everyone != nil { // @everyone role ID == guild ID
		perms |= everyone.Permissions
	}
	for _, rid := range member.Roles {
		if r := byID[rid]; r != nil {
			perms |= r.Permissions
			if r.Position > botTop {
				botTop = r.Position
			}
		}
	}
	if perms&discordgo.PermissionAdministrator != 0 {
		return PermissionReport{}
	}

	var rep PermissionReport
	need := func(bit int64, name string) {
		if perms&bit == 0 {
			rep.Missing = append(rep.Missing, name)
		}
	}
	need(discordgo.PermissionViewChannel, "View Channels")
	need(discordgo.PermissionSendMessages, "Send Messages")
	need(discordgo.PermissionReadMessageHistory, "Read Message History")
	if pc.NeedEmbed {
		need(discordgo.PermissionEmbedLinks, "Embed Links")
	}
	if pc.NeedRoles {
		need(discordgo.PermissionManageRoles, "Manage Roles")
		if r := byID[pc.RoleAboveID]; r != nil && botTop <= r.Position {
			rep.Hierarchy = true
		}
	}
	if pc.NeedNicks {
		need(discordgo.PermissionManageNicknames, "Manage Nicknames")
	}
	return rep
}

// AddRole / RemoveRole manage a guild role on a member (used to mark linked players).
// The bot needs Manage Roles and a role above the target role.
func (c *Client) AddRole(guildID, userID, roleID string) error {
	return c.session.GuildMemberRoleAdd(guildID, userID, roleID)
}

func (c *Client) RemoveRole(guildID, userID, roleID string) error {
	return c.session.GuildMemberRoleRemove(guildID, userID, roleID)
}

// SetNickname sets a member's server nickname ("" clears it). Needs Manage Nicknames and
// a role above the member; can't change the guild owner.
func (c *Client) SetNickname(guildID, userID, nick string) error {
	return c.session.GuildMemberNickname(guildID, userID, nick)
}

// SendEmbed posts an event as a single-line colored embed (the color is the left bar).
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
		User:      messageDisplayName(m),
		UserID:    m.Author.ID,
		Roles:     roles,
		Message:   m.Content,
		ChannelID: m.ChannelID,
		IsAdmin:   c.authorIsAdmin(m),
	})
}

// messageDisplayName prefers the server nickname, then the global display name, then the
// raw username — so "bits-orio" (a display name) shows correctly even though the new-style
// username can't contain a hyphen.
func messageDisplayName(m *discordgo.MessageCreate) string {
	if m.Member != nil && m.Member.Nick != "" {
		return m.Member.Nick
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
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
