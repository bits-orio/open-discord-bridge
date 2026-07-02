package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"net/http"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/config"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/controlapi"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/discord"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/rcon"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/router"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/status"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/transport"
)

// Event mirrors a JSONL line written by the companion mod.
type Event struct {
	Event   string         `json:"event"`
	Ts      int64          `json:"ts"`
	Surface string         `json:"surface"`
	Data    map[string]any `json:"data"`
}

func main() {
	cfgPath := flag.String("config", "bridge.yaml", "path to config file")
	flag.Parse()

	// Load a .env sitting next to the config file (the setup wizard writes one there) so the
	// bridge's secrets are available without the caller having to `source` it first. Real
	// environment variables always win, so a hosting panel's injected vars take precedence.
	loadDotEnv(filepath.Dir(*cfgPath))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Tee logs to a file too, so the bridge is observable where stdout isn't surfaced (e.g.
	// a panel that backgrounds it). Defaults to bridge.log next to the events file.
	setupLogFile(cfg)

	rt := router.New(toRouterRoutes(cfg.Discord.Routes))
	rc := rcon.New(cfg.Factorio.RCON.Address, cfg.Factorio.RCON.Password)
	defer rc.Close()

	// Admin-configured Discord commands: first word of a message maps to an RCON command.
	cmdMap := make(map[string]config.Command, len(cfg.Discord.Commands))
	for _, c := range cfg.Discord.Commands {
		cmdMap[c.Trigger] = c
	}

	// Discord → game.
	onInbound := func(msg discord.InboundMessage) {
		if cmd, ok := cmdMap[firstWord(msg.Message)]; ok {
			isAdmin := resolveAdmin(cfg.Discord.Admins, msg)
			// Discord-initiated linking: !link with no code → DM the player an in-game command.
			if cmd.DiscordLink && len(commandArgs(msg.Message)) == 0 {
				go initiateDiscordLink(rc, dc, msg)
				return
			}
			if reply := runCommand(rc, cmd, isAdmin, commandArgs(msg.Message), msg.User, msg.UserID); reply != "" {
				dc.Send(msg.ChannelID, reply)
			}
			return
		}
		for _, cmd := range incomingCommands(msg.User, msg.UserID, msg.Message, msg.ChannelID) {
			if _, err := rc.Execute(cmd); err != nil {
				log.Printf("rcon: inbound deliver failed: %v", err)
			}
		}
	}

	dc, err = discord.New(cfg.Discord.Token, rt.InboundChannels(), onInbound)
	if err != nil {
		log.Fatalf("discord: %v", err)
	}

	// Expose the configured commands as guild slash commands too (if a guild is set).
	if specs, byName := buildSlash(cfg.Discord.Commands); len(specs) > 0 && cfg.Discord.GuildID != "" {
		dc.EnableSlashCommands(cfg.Discord.GuildID, specs, func(inv discord.SlashInvocation) string {
			cmd, ok := byName[inv.Name]
			if !ok {
				return "Unknown command."
			}
			isAdmin := resolveAdmin(cfg.Discord.Admins, discord.InboundMessage{
				UserID: inv.UserID, Roles: inv.Roles, IsAdmin: inv.IsAdmin,
			})
			return runCommand(rc, cmd, isAdmin, strings.Fields(inv.Args), inv.User, inv.UserID)
		})
	}

	if err := dc.Open(); err != nil {
		log.Fatalf("discord: open gateway: %v", err)
	}
	defer dc.Close()
	log.Printf("bridge: connected to Discord; tailing %s", cfg.Factorio.EventsFile)

	// Persistent links store: survives Factorio resets. Default path is links.json next to
	// the config file; override with factorio.links_file or ODB_LINKS_FILE.
	linksPath := cfg.Factorio.LinksFile
	if linksPath == "" {
		linksPath = filepath.Join(filepath.Dir(*cfgPath), "links.json")
	}
	ls := newLinksStore(linksPath)
	ls.load()

	// Channel topic status: enabled by default; set channel_topic_status: false to disable.
	topicEnabled := cfg.Discord.ChannelTopicStatus == nil || *cfg.Discord.ChannelTopicStatus
	primaryChannelID := cfg.Discord.Routes[0].ChannelID
	var sm *status.Manager
	if topicEnabled {
		sm = status.New(primaryChannelID, dc)
	}

	// Version handshake: confirm the companion mod is reachable and log its version.
	if st, ok := queryODBStatus(rc); ok {
		log.Printf("bridge: companion mod version %s", st.ModVersion)
	} else {
		log.Printf("bridge: companion mod not reachable over RCON yet (will retry on demand)")
	}

	// Route an event to its channel and post it. With decoration on, integrator events
	// (mts.*, …) render in an ANSI code block with their "[ns → event]" category label
	// colored (deterministically per key) so categories are visually distinct; the rest of
	// the line stays default. Vanilla/bridge events and chat are always plain text.
	// pingIDs are the Discord users the message may ping (resolved @mentions); everything
	// else stays under the deny-all AllowedMentions default.
	emit := func(ev Event, pingIDs ...string) {
		channel, ok := rt.Channel(ev.Event)
		if !ok {
			return
		}
		if len(pingIDs) > 0 {
			if err := dc.PostMentioning(channel, renderEvent(ev, cfg.Discord.Embed), pingIDs...); err != nil {
				log.Printf("discord: mention post failed: %v", err)
			}
			return
		}
		dc.Send(channel, renderEvent(ev, cfg.Discord.Embed))
	}

	// Mention resolver: populated once ctx is live, but declared here so onLine can close over it.
	mentions := &mentionResolver{}

	// Game → Discord.
	var lastEvent atomic.Int64
	var tail *transport.Tailer
	switch cfg.Transport {
	case "sftp":
		tail = transport.NewSFTP(transport.SFTPConfig{
			Host:           cfg.Factorio.SFTP.Host,
			User:           cfg.Factorio.SFTP.User,
			KeyPath:        cfg.Factorio.SFTP.KeyPath,
			Password:       cfg.Factorio.SFTP.Password,
			KnownHostsPath: cfg.Factorio.SFTP.KnownHostsPath,
		}, cfg.Factorio.EventsFile, cfg.Interval())
	default:
		tail = transport.NewLocal(cfg.Factorio.EventsFile, cfg.Interval())
	}
	joinEvent := cfg.Discord.StatusPlayerJoinedEvent
	if joinEvent == "" {
		joinEvent = "vanilla.player_joined"
	}
	leftEvent := cfg.Discord.StatusPlayerLeftEvent
	if leftEvent == "" {
		leftEvent = "vanilla.player_left"
	}
	onLine := func(line []byte) {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			log.Printf("transport: bad JSONL line: %v", err)
			return
		}
		lastEvent.Store(time.Now().Unix())
		if sm != nil {
			handleStatusEvent(sm, ev, joinEvent, leftEvent)
		}
		handleLinkEvent(ls, ev)
		// Resolve @playerName mentions to Discord pings in chat messages.
		var pingIDs []string
		if isChatEvent(ev.Event) {
			if msg, ok := ev.Data["message"].(string); ok && msg != "" {
				ev.Data["message"], pingIDs = mentions.resolve(msg)
			}
		}
		emit(ev, pingIDs...)
	}

	// baseCtx lets the Control API request a restart (clean exit → supervisor restarts).
	baseCtx, requestRestart := context.WithCancel(context.Background())
	defer requestRestart()
	ctx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go tail.Run(ctx, onLine)

	// Permission monitor: check at startup and retry every 20s until all permissions are
	// granted. Posts a warning when something is missing and a confirmation when fixed.
	go func() {
		pc := discord.PermissionCheck{
			GuildID:         cfg.Discord.GuildID,
			ChannelIDs:      rt.InboundChannels(),
			NeedRoles:       cfg.Discord.LinkedRoleID != "",
			NeedNicks:       cfg.Discord.LinkedNickname != "",
			RoleAboveID:     cfg.Discord.LinkedRoleID,
			NeedTopicStatus: sm != nil,
			TopicChannelID:  primaryChannelID,
		}
		warnCh, hasWarnCh := rt.Channel("bridge.warning")

		rep := dc.CheckPermissions(pc)
		if rep.Err != "" {
			log.Printf("bridge: could not check permissions: %s", rep.Err)
			return
		}
		if rep.OK() {
			log.Printf("bridge: Discord permission preflight passed (checked channels: %s)",
				strings.Join(rt.InboundChannels(), ", "))
			return
		}

		// Missing permissions — post the warning and start retry loop.
		if len(rep.Missing) > 0 {
			log.Printf("bridge: bot is missing permissions: %s", strings.Join(rep.Missing, ", "))
		}
		if rep.Hierarchy {
			log.Printf("bridge: bot's role is not above the linked role")
		}
		if hasWarnCh {
			dc.Send(warnCh, permissionHelp(rep))
		} else {
			log.Printf("bridge: no route matches \"bridge.warning\" — warning logged only, not posted to Discord")
		}

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rep = dc.CheckPermissions(pc)
				if rep.Err != "" {
					continue // can't check right now, try again
				}
				if rep.OK() {
					log.Printf("bridge: Discord permissions now OK")
					if hasWarnCh {
						dc.Send(warnCh, ":white_check_mark: **Open Discord Bridge — all permissions granted.** The bridge is fully operational.")
					}
					return
				}
			}
		}
	}()

	// Keep @mention resolution cache fresh (player name → Discord user ID).
	go syncMentions(ctx, rc, mentions)

	// Keep a Discord role / nickname in sync with linked players.
	if (cfg.Discord.LinkedRoleID != "" || cfg.Discord.LinkedNickname != "") && cfg.Discord.GuildID != "" {
		go syncLinkedMembers(ctx, rc, dc, cfg.Discord.GuildID, cfg.Discord.LinkedRoleID, cfg.Discord.LinkedNickname)
	}

	// Watch the bridge↔Factorio link (RCON + mod handshake), update the channel topic,
	// and optionally announce connect/disconnect transitions to Discord.
	go monitorConnection(ctx, rc, func(connected bool, st odbStatus) {
		now := time.Now()
		if connected {
			restoreLinks(rc, ls)
		}
		if sm != nil {
			if connected {
				sm.OnConnected(now, st.Players, st.Ticks)
			} else {
				sm.OnDisconnected(now)
			}
		}
		if cfg.Discord.AnnounceStatus {
			if connected {
				emit(Event{Event: "bridge.established", Data: map[string]any{"version": st.ModVersion}})
			} else {
				emit(Event{Event: "bridge.disconnected"})
			}
		}
	})

	// Open Control API (off by default).
	if cfg.ControlAPI.Enabled {
		deps := controlapi.Deps{
			Status: func() controlapi.Status {
				return buildStatus(cfg, dc, rc, lastEvent.Load())
			},
			Guilds: func() ([]controlapi.Guild, error) {
				gs, err := dc.Guilds()
				if err != nil {
					return nil, err
				}
				out := make([]controlapi.Guild, len(gs))
				for i, g := range gs {
					out[i] = controlapi.Guild{ID: g.ID, Name: g.Name}
				}
				return out, nil
			},
			Channels: func(guildID string) ([]controlapi.Channel, error) {
				chs, err := dc.Channels(guildID)
				if err != nil {
					return nil, err
				}
				out := make([]controlapi.Channel, len(chs))
				for i, ch := range chs {
					out[i] = controlapi.Channel{ID: ch.ID, Name: ch.Name, Type: ch.Type}
				}
				return out, nil
			},
			Test: func() controlapi.TestResult {
				return roundTrip(dc, rc, rt.InboundChannels())
			},
			GetConfig: func() controlapi.Config {
				return getConfig(cfg, rt)
			},
			SetConfig: func(in controlapi.Config) error {
				return setConfig(cfg, rt, dc, in)
			},
			Restart: func() {
				log.Printf("controlapi: restart requested — exiting for supervisor to restart")
				requestRestart()
			},
		}
		srv := controlapi.New(cfg.ControlAPI.Listen, cfg.ControlAPI.AuthToken, deps)
		go func() {
			if err := srv.Start(ctx); err != nil && err != http.ErrServerClosed {
				log.Printf("controlapi: %v", err)
			}
		}()
	}

	<-ctx.Done()
	log.Printf("bridge: shutting down")
}

// monitorConnection polls the RCON+mod handshake and reports connect/disconnect
// transitions via announce. The initial state is announced only if connected (so a bridge
// started before Factorio doesn't post a spurious "disconnected").
func monitorConnection(ctx context.Context, rc *rcon.Client, announce func(connected bool, st odbStatus)) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	var last *bool
	check := func() {
		st, ok := queryODBStatus(rc)
		switch {
		case last == nil:
			if ok {
				announce(true, st)
			}
			last = &ok
		case *last != ok:
			announce(ok, st)
			*last = ok
		}
	}

	check() // immediate, so a healthy start announces "established" right away
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// linkInfo is one player↔Discord mapping reported by /odb-status.
type linkInfo struct {
	DiscordID   string `json:"discord_id"`
	Player      string `json:"player"`
	DiscordName string `json:"discord_name"`
}

func parseLinks(statusJSON string) []linkInfo {
	var s struct {
		Links json.RawMessage `json:"links"`
	}
	if json.Unmarshal([]byte(statusJSON), &s) != nil {
		return nil
	}
	var links []linkInfo
	_ = json.Unmarshal(s.Links, &links) // [] or {} (empty) -> nil; [{...}] -> entries
	return links
}

// generateDiscordLinkCode returns a random 6-character base-36 code for Discord-initiated linking.
func generateDiscordLinkCode() string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// initiateDiscordLink handles the Discord-side !link (no code): generates a one-time code,
// registers it in Factorio via RCON, then DMs the user the in-game command to complete linking.
// Falls back to an in-channel mention if the user has DMs disabled.
func initiateDiscordLink(rc *rcon.Client, dc *discord.Client, msg discord.InboundMessage) {
	code := generateDiscordLinkCode()
	rconCmd := fmt.Sprintf("/odb-register-discord-code %s %s %s",
		code, sanitizeArg(msg.UserID), sanitizeArg(msg.User))
	if _, err := rc.Execute(rconCmd); err != nil {
		log.Printf("discord-link: RCON failed: %v", err)
		dc.Send(msg.ChannelID, ":x: Factorio server is not reachable right now — try again shortly.")
		return
	}
	dmText := fmt.Sprintf(":link: Run this command in Factorio to link your account (expires ~60s):\n```\n/odb-discord-link %s\n```", code)
	if err := dc.SendDM(msg.UserID, dmText); err != nil {
		log.Printf("discord-link: DM to %s failed (%v) — posting in channel", msg.UserID, err)
		// Deliberate, targeted ping: this is a bridge-authored fallback notifying the same
		// user who ran !link that their DM bounced, not untrusted relayed content.
		if err := dc.PostMentioning(msg.ChannelID, fmt.Sprintf("<@%s> %s", msg.UserID, dmText), msg.UserID); err != nil {
			log.Printf("discord-link: fallback channel post to %s failed: %v", msg.ChannelID, err)
		}
	}
}

// mentionResolver keeps a live cache of player-name → Discord-ID mappings and resolves
// @name tokens in chat messages to proper Discord <@id> pings.
type mentionResolver struct {
	mu    sync.RWMutex
	links map[string]string // lowercase factorio name → discord user ID
}

// atMentionRe matches an @token that may be preceded by whitespace or start-of-string,
// capturing the leading separator and the name separately so we can preserve spacing.
var atMentionRe = regexp.MustCompile(`(^|[\s,])@([A-Za-z0-9_\-]+)`)

func (mr *mentionResolver) update(links []linkInfo) {
	m := make(map[string]string, len(links))
	for _, l := range links {
		if l.DiscordID != "" {
			m[l.Player] = l.DiscordID
		}
	}
	mr.mu.Lock()
	mr.links = m
	mr.mu.Unlock()
}

// resolve rewrites @playerName tokens to <@id> and returns the distinct IDs it
// resolved. Outbound sends default to pinging nobody (AllowedMentions deny-all), so the
// caller must pass these IDs to PostMentioning for the rewritten mentions to actually
// notify — a bare <@id> in a deny-all message renders as a mention but pings no one.
func (mr *mentionResolver) resolve(s string) (string, []string) {
	mr.mu.RLock()
	links := mr.links
	mr.mu.RUnlock()
	if len(links) == 0 {
		return s, nil
	}
	var ids []string
	seen := make(map[string]bool)
	resolved := atMentionRe.ReplaceAllStringFunc(s, func(m string) string {
		at := strings.IndexByte(m, '@')
		name := m[at+1:]
		if id, ok := links[name]; ok {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			return m[:at] + "<@" + id + ">"
		}
		return m
	})
	return resolved, ids
}

// syncMentions polls /odb-status every 20s to keep the mention cache fresh.
func syncMentions(ctx context.Context, rc *rcon.Client, mr *mentionResolver) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	refresh := func() {
		if links, ok := queryLinks(rc); ok {
			mr.update(links)
		}
	}
	refresh()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

// queryLinks fetches the current player↔Discord links over RCON. ok=false means the mod
// is unreachable (so callers should not churn Discord state during an outage).
func queryLinks(rc *rcon.Client) ([]linkInfo, bool) {
	resp, err := rc.Execute("/odb-status")
	if err != nil {
		return nil, false
	}
	return parseLinks(resp), true
}

// nickFor renders a linked member's nickname from the configured format ({factorio} =
// in-game name, {discord} = their Discord display name), capped at Discord's 32 chars.
func nickFor(format string, l linkInfo) string {
	n := strings.ReplaceAll(format, "{factorio}", l.Player)
	n = strings.ReplaceAll(n, "{discord}", l.DiscordName)
	if len(n) > 32 {
		n = n[:32]
	}
	return n
}

// syncLinkedMembers reconciles a Discord role and/or nickname with the set of linked
// players, polling /odb-status. Tracked in memory: it applies on a link transition and
// reverts on unlink (while running). Errors (missing Manage Roles/Nicknames, role
// hierarchy, guild owner) are logged and retried.
func syncLinkedMembers(ctx context.Context, rc *rcon.Client, dc *discord.Client, guildID, roleID, nickFormat string) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	// Discord forbids bots from changing the guild owner's nickname (even with Manage
	// Nicknames), so we skip the nickname step for them — their linked role still applies.
	ownerID, _ := dc.GuildOwnerID(guildID)

	seen := map[string]string{} // discord_id → factorio player name
	reconcile := func() {
		links, ok := queryLinks(rc)
		if !ok {
			return
		}
		cur := make(map[string]linkInfo, len(links))
		for _, l := range links {
			if l.DiscordID != "" {
				cur[l.DiscordID] = l
			}
		}
		for id, l := range cur { // newly linked → apply
			if seen[id] != "" {
				continue
			}
			if roleID != "" {
				if err := dc.AddRole(guildID, id, roleID); err != nil {
					log.Printf("linked-member: add role to %s failed: %v", id, err)
					continue // retry next poll
				}
				log.Printf("linked-member: role added — %s linked to %s", l.Player, id)
			}
			if nickFormat != "" {
				if id == ownerID {
					log.Printf("linked-member: not setting nickname for %s — they are the server owner (Discord forbids bots renaming the owner); their linked role still applies", id)
				} else if err := dc.SetNickname(guildID, id, nickFor(nickFormat, l)); err != nil {
					log.Printf("linked-member: set nickname for %s failed: %v", id, err)
				}
			}
			seen[id] = l.Player
		}
		for id, player := range seen { // newly unlinked → revert
			if _, still := cur[id]; still {
				continue
			}
			if roleID != "" {
				if err := dc.RemoveRole(guildID, id, roleID); err != nil {
					log.Printf("linked-member: remove role from %s failed: %v", id, err)
					continue // keep, retry next poll
				}
				log.Printf("linked-member: role removed — %s unlinked (%s)", player, id)
			}
			if nickFormat != "" && id != ownerID {
				_ = dc.SetNickname(guildID, id, "") // clear (owner's was never set)
			}
			delete(seen, id)
		}
	}

	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

// odbStatus is the parsed result of an /odb-status RCON call.
type odbStatus struct {
	ModVersion string
	Players    []string
	Ticks      *uint64 // nil when the mod didn't report it (pre-0.1.4)
}

// queryODBStatus calls /odb-status and returns the parsed result.
// ok=false means the mod is unreachable.
func queryODBStatus(rc *rcon.Client) (odbStatus, bool) {
	resp, err := rc.Execute("/odb-status")
	if err != nil {
		return odbStatus{}, false
	}
	var raw struct {
		ModVersion string          `json:"mod_version"`
		Players    json.RawMessage `json:"players"`
		Ticks      *uint64         `json:"ticks"`
	}
	if json.Unmarshal([]byte(resp), &raw) != nil || raw.ModVersion == "" {
		return odbStatus{}, false
	}
	var players []string
	_ = json.Unmarshal(raw.Players, &players) // {} -> nil (no players), [...] -> slice
	return odbStatus{ModVersion: raw.ModVersion, Players: players, Ticks: raw.Ticks}, true
}

// handleLinkEvent updates the persistent links store on odb.link_confirmed/removed events.
func handleLinkEvent(ls *linksStore, ev Event) {
	switch ev.Event {
	case "odb.link_confirmed":
		player, discordID, discordName := str(ev.Data["player"]), str(ev.Data["discord_id"]), str(ev.Data["discord_name"])
		if player != "" && discordID != "" {
			ls.upsert(linkInfo{Player: player, DiscordID: discordID, DiscordName: discordName})
		}
	case "odb.link_removed":
		if player := str(ev.Data["player"]); player != "" {
			ls.removeByPlayer(player)
		}
	}
}

// restoreLinks pushes all stored links into the mod via /odb-set-link after a connection.
func restoreLinks(rc *rcon.Client, ls *linksStore) {
	for _, l := range ls.all() {
		cmd := fmt.Sprintf("/odb-set-link %s %s %s", l.Player, l.DiscordID, l.DiscordName)
		if _, err := rc.Execute(cmd); err != nil {
			log.Printf("links: restore %s: %v", l.Player, err)
		}
	}
}

// handleStatusEvent feeds relevant JSONL events to the status manager.
// It matches both the configured event names (default: vanilla.*) and the mts.*
// equivalents so that MTS servers work without extra config — MTS disables the
// vanilla baselines and emits its own player_joined/left with the same payload shape.
func handleStatusEvent(sm *status.Manager, ev Event, joinEvent, leftEvent string) {
	switch ev.Event {
	case joinEvent, "mts.player_joined":
		if name := str(ev.Data["player"]); name != "" {
			sm.OnPlayerJoined(name)
		}
	case leftEvent, "mts.player_left":
		if name := str(ev.Data["player"]); name != "" {
			sm.OnPlayerLeft(name)
		}
	}
}

// roundTrip backs POST /v1/test: post a message to each bridged channel (outbound)
// and inject one into the game over RCON (inbound).
func roundTrip(dc *discord.Client, rc *rcon.Client, channels []string) controlapi.TestResult {
	var res controlapi.TestResult

	var sendErr error
	for _, ch := range channels {
		if err := dc.Post(ch, ":satellite: Open Discord Bridge — round-trip test"); err != nil {
			sendErr = err
		}
	}
	res.OutboundOK = sendErr == nil

	payload, _ := json.Marshal(map[string]string{
		"user":    "Bridge Test",
		"message": "round-trip test",
	})
	if _, err := rc.Execute("/odb-incoming " + string(payload)); err != nil {
		res.Error = err.Error()
	} else {
		res.InboundOK = true
	}
	if sendErr != nil && res.Error == "" {
		res.Error = sendErr.Error()
	}
	return res
}

// getConfig backs GET /v1/config. Live routes come from the router; the static fields
// come from the loaded config. Secrets are never included.
func getConfig(cfg *config.Config, rt *router.Router) controlapi.Config {
	rs := rt.Routes()
	routes := make([]controlapi.Route, len(rs))
	for i, r := range rs {
		routes[i] = controlapi.Route{Source: r.Source, ChannelID: r.ChannelID}
	}
	return controlapi.Config{
		Transport: cfg.Transport,
		Factorio: controlapi.ConfigFactorio{
			RconAddress:        cfg.Factorio.RCON.Address,
			EventsFile:         cfg.Factorio.EventsFile,
			RequiredModVersion: cfg.Factorio.RequiredModVersion,
		},
		Discord: controlapi.ConfigDiscord{
			GuildID: cfg.Discord.GuildID,
			Routes:  routes,
		},
	}
}

// setConfig backs POST /v1/config. Routing is applied live; runtime-immutable fields
// (transport, events_file, rcon address) are rejected if a change is requested — those
// require a restart with an updated config file.
func setConfig(cfg *config.Config, rt *router.Router, dc *discord.Client, in controlapi.Config) error {
	if in.Transport != "" && in.Transport != cfg.Transport {
		return fmt.Errorf("transport cannot be changed at runtime; restart with an updated config")
	}
	if in.Factorio.EventsFile != "" && in.Factorio.EventsFile != cfg.Factorio.EventsFile {
		return fmt.Errorf("factorio.events_file cannot be changed at runtime")
	}
	if in.Factorio.RconAddress != "" && in.Factorio.RconAddress != cfg.Factorio.RCON.Address {
		return fmt.Errorf("factorio.rcon_address cannot be changed at runtime")
	}
	if len(in.Discord.Routes) == 0 {
		return fmt.Errorf("discord.routes must not be empty")
	}
	routes := make([]router.Route, len(in.Discord.Routes))
	for i, r := range in.Discord.Routes {
		if r.Source == "" || r.ChannelID == "" {
			return fmt.Errorf("discord.routes[%d] needs both source and channel_id", i)
		}
		routes[i] = router.Route{Source: r.Source, ChannelID: r.ChannelID}
	}
	rt.Update(routes)
	dc.UpdateInbound(rt.InboundChannels())
	return nil
}

// buildStatus assembles a live GET /v1/status snapshot.
func buildStatus(cfg *config.Config, dc *discord.Client, rc *rcon.Client, lastEventUnix int64) controlapi.Status {
	fs := controlapi.FactorioStatus{
		RconAddress:        cfg.Factorio.RCON.Address,
		RequiredModVersion: cfg.Factorio.RequiredModVersion,
	}
	if resp, err := rc.Execute("/odb-status"); err != nil {
		fs.Error = err.Error()
	} else {
		fs.RconOK = true
		var ms struct {
			ModVersion string          `json:"mod_version"`
			Interface  string          `json:"interface"`
			Sources    json.RawMessage `json:"sources"`
		}
		if json.Unmarshal([]byte(resp), &ms) == nil {
			fs.ModVersion = ms.ModVersion
			fs.Interface = ms.Interface
			fs.Sources = ms.Sources
		}
	}
	return controlapi.Status{
		Transport:     cfg.Transport,
		Discord:       controlapi.DiscordStatus{Connected: dc.Connected()},
		Factorio:      fs,
		LastEventUnix: lastEventUnix,
	}
}

// dc is package-level so onInbound (defined before dc) can reference it.
var dc *discord.Client

func toRouterRoutes(in []config.Route) []router.Route {
	out := make([]router.Route, len(in))
	for i, r := range in {
		out[i] = router.Route{Source: r.Source, ChannelID: r.ChannelID}
	}
	return out
}

func formatEvent(ev Event) string {
	d := ev.Data
	switch ev.Event {
	case "vanilla.chat":
		return fmt.Sprintf("**%s**: %s", str(d["player"]), str(d["message"]))
	case "vanilla.player_joined":
		return fmt.Sprintf(":inbox_tray: **%s** joined the game (online: %s)", str(d["player"]), str(d["online_count"]))
	case "vanilla.player_left":
		return fmt.Sprintf(":outbox_tray: **%s** left the game (online: %s)", str(d["player"]), str(d["online_count"]))
	case "vanilla.player_died":
		if c := str(d["cause"]); c != "" {
			return fmt.Sprintf(":skull: **%s** was killed by %s", str(d["player"]), c)
		}
		return fmt.Sprintf(":skull: **%s** died", str(d["player"]))
	case "vanilla.rocket_launched":
		return fmt.Sprintf(":rocket: Rocket launched (total: %s)", str(d["flight_count"]))
	case "vanilla.research_finished":
		return fmt.Sprintf(":microscope: Research complete: **%s**", str(d["tech_name"]))
	case "vanilla.game_started":
		return ":satellite: Server is online."
	case "bridge.established":
		if v := str(d["version"]); v != "" {
			return fmt.Sprintf(":green_circle: **Open Discord Bridge established** — connected to Factorio (mod v%s)", v)
		}
		return ":green_circle: **Open Discord Bridge established** — connected to Factorio"
	case "bridge.disconnected":
		return ":red_circle: **Open Discord Bridge disconnected** — lost contact with Factorio"
	case "odb.link_confirmed":
		return "```\nLinked " + str(d["player"]) + " to Discord user " + str(d["discord_name"]) + ".\n```"
	case "odb.link_removed":
		return "```\n" + str(d["player"]) + " unlinked their Discord account.\n```"
	default:
		return formatGeneric(ev.Event, d)
	}
}

// formatGeneric renders a non-baseline event (mts.*, oarc.*, custom.*, ...) for plain-text
// mode: a humanized "[namespace → event]" label (bolded — bold is emoji-safe, so an
// integrator can put an emoji in the label, unlike inline-code which renders :shortcodes:
// literally) plus the integrator's body. The ANSI-decorated path builds its own label.
func formatGeneric(eventKey string, data map[string]any) string {
	label := "**" + humanizeKey(eventKey, firstString(data, "label")) + "**"
	if body := genericBody(data); body != "" {
		return label + " " + body
	}
	return label
}

// genericBody is an event's human text: the integrator-supplied "text"/"message", else a
// readable key=value summary.
func genericBody(data map[string]any) string {
	if s := firstString(data, "text", "message"); s != "" {
		return s
	}
	return kvSummary(data)
}

// eventLabel turns "mts.team_created" into "mts → team created" (no brackets). An integrator
// may override the event-name portion (right of the arrow) by supplying a "label" in the
// event data — e.g. label "🔬" yields "mts → 🔬" — while the bridge keeps the namespace so
// attribution stays consistent. Use a literal unicode/custom emoji; bare :shortcodes: don't
// render from a bot.
func eventLabel(eventKey, override string) string {
	ns, name, found := strings.Cut(eventKey, ".")
	if !found {
		if override != "" {
			return override
		}
		return strings.ReplaceAll(ns, "_", " ")
	}
	if override == "" {
		override = strings.ReplaceAll(name, "_", " ")
	}
	return fmt.Sprintf("%s → %s", ns, override)
}

// humanizeKey is the bracketed label for plain text, e.g. "[mts → team created]". override
// (the integrator-supplied "label", may be "") replaces the event-name portion.
func humanizeKey(eventKey, override string) string { return "[" + eventLabel(eventKey, override) + "]" }

func firstString(data map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := data[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// kvSummary is the fallback when an event carries no "text": sorted key=value pairs,
// skipping the "text"/"message"/"label" presentation keys themselves.
func kvSummary(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		if k == "text" || k == "message" || k == "label" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, str(data[k])))
	}
	return strings.Join(parts, ", ")
}

// runCommand executes a configured command (shared by the text and slash paths) and
// returns the reply text to post ("" = nothing to post). Enforces admin gating and, for
// args:true commands, interpolation + a usage hint.
func runCommand(rc *rcon.Client, cmd config.Command, isAdmin bool, argv []string, user, userID string) string {
	if cmd.Admin && !isAdmin {
		return fmt.Sprintf(":no_entry: `%s` is admin-only.", cmd.Trigger)
	}
	rconCmd := cmd.Rcon
	if cmd.Args {
		if templateNeedsArgs(cmd.Rcon) && len(argv) == 0 {
			if cmd.UsageHint != "" {
				return cmd.UsageHint
			}
			return fmt.Sprintf("Usage: `%s <args>`", cmd.Trigger)
		}
		rconCmd = interpolate(cmd.Rcon, argv, user, userID)
	}
	resp, err := rc.Execute(rconCmd)
	if err != nil {
		log.Printf("rcon: command %q failed: %v", cmd.Trigger, err)
		return ""
	}
	if strings.TrimSpace(resp) != "" {
		return "```\n" + resp + "\n```"
	}
	return ""
}

// buildSlash maps configured commands to slash-command specs (deduped by sanitized name)
// and a name→command lookup for the handler.
func buildSlash(commands []config.Command) ([]discord.SlashSpec, map[string]config.Command) {
	var specs []discord.SlashSpec
	byName := map[string]config.Command{}
	for _, c := range commands {
		name := slashName(c.Trigger)
		if name == "" {
			continue
		}
		if _, dup := byName[name]; dup {
			continue
		}
		byName[name] = c
		specs = append(specs, discord.SlashSpec{
			Name: name, Description: "Run " + c.Trigger, Admin: c.Admin,
			TakesArgs: c.Args && templateNeedsArgs(c.Rcon),
		})
	}
	return specs, byName
}

// slashName turns a trigger like "!players" into a valid slash name "players".
func slashName(trigger string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(trigger) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

// resolveAdmin reports whether the message author is a Discord admin: an explicit user
// ID, a holder of a configured admin role, or (fallback) Discord's Administrator perm.
func resolveAdmin(a config.AdminConfig, msg discord.InboundMessage) bool {
	for _, u := range a.Users {
		if u == msg.UserID {
			return true
		}
	}
	if len(a.Roles) > 0 {
		held := make(map[string]bool, len(msg.Roles))
		for _, r := range msg.Roles {
			held[r] = true
		}
		for _, r := range a.Roles {
			if held[r] {
				return true
			}
		}
	}
	return a.PermissionFallback() && msg.IsAdmin
}

// permissionHelp builds a Discord message describing the permission problems and
// numbered, step-by-step instructions to fix them.
func permissionHelp(rep discord.PermissionReport) string {
	var b strings.Builder
	b.WriteString(":warning: **Open Discord Bridge — permission setup needed.**\n")
	if len(rep.Missing) > 0 {
		b.WriteString("• Missing permissions: " + strings.Join(rep.Missing, ", ") + "\n")
	}
	if rep.Hierarchy {
		b.WriteString("• The bot's role is below the linked role.\n")
	}

	steps := []string{
		"Open **Server Settings → Roles**.",
		"Select the bot's own role (its name matches the bot).",
	}
	if len(rep.Missing) > 0 {
		steps = append(steps, "Under **Permissions**, turn on: "+strings.Join(rep.Missing, ", ")+".")
	}
	if rep.Hierarchy {
		steps = append(steps, "Back on the Roles list, **drag the bot's role above** the linked role.")
	}
	steps = append(steps, "That's it — the bridge re-checks automatically within ~20s (or restart it).")

	b.WriteString("\n**How to fix** (a server admin):\n")
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s)
	}
	b.WriteString("\n(Alternatively, re-invite the bot with these permissions.)")
	return b.String()
}

// isChatEvent reports whether an event is chat — the interface convention is that any
// event keyed "chat" or "<namespace>.chat" is chat. Such events never get a category
// square, so integrators can mark chat-style relay (e.g. "mts.chat") vs notable events
// (e.g. "mts.team_created", which gets a square) purely by how they name the event.
func isChatEvent(eventKey string) bool {
	return eventKey == "chat" || strings.HasSuffix(eventKey, ".chat")
}

// renderEvent produces the Discord text for an event. With decoration off (or for
// vanilla/bridge/chat events) it's the plain-text formatEvent line. With decoration on,
// integrator events (those carrying a "[ns → event]" label, e.g. mts.*) render inside an
// ANSI code block with just the category label colored — deterministically per key — so all
// events of one type share a color and differ from other types. The body stays default
// (note: Discord markdown like **bold** does not render inside an ANSI code block).
func renderEvent(ev Event, decorate bool) string {
	if !decorate || isChatEvent(ev.Event) || !isGenericEvent(ev.Event) {
		return formatEvent(ev)
	}
	line := ansiColor(ev.Event) + humanizeKey(ev.Event, firstString(ev.Data, "label")) + ansiReset
	if body := genericBody(ev.Data); body != "" {
		line += " " + body
	}
	return "```ansi\n" + line + "\n```"
}

// isGenericEvent reports whether an event is an integrator event (carries a namespaced
// "[ns → event]" label), as opposed to the bridge's own vanilla.*/bridge.* events.
func isGenericEvent(eventKey string) bool {
	switch ns, _, _ := strings.Cut(eventKey, "."); ns {
	case "vanilla", "bridge", "odb":
		return false
	default:
		return true
	}
}

const ansiReset = "[0m"

// ansiPalette holds Discord-supported ANSI SGR color codes (foreground 31–36), used bold
// (1;) so labels stand out. Category labels hash into this palette.
var ansiPalette = []int{31, 32, 33, 34, 35, 36} // red, green, yellow, blue, magenta, cyan

// ansiColor returns the bold ANSI escape that colors an event's category label, picked
// deterministically from the event key (so a given event type is always the same color).
func ansiColor(eventKey string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(eventKey))
	return fmt.Sprintf("[1;%dm", ansiPalette[h.Sum32()%uint32(len(ansiPalette))])
}

// firstWord returns the first whitespace-separated token of s ("" if none).
func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// commandArgs returns the whitespace-separated tokens after the command trigger.
func commandArgs(s string) []string {
	f := strings.Fields(s)
	if len(f) <= 1 {
		return nil
	}
	return f[1:]
}

var placeholderRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// needsArgsRe matches placeholders that require user-typed input ({args} or {1}, {2}, …).
// {user}/{userid} are always available, so a template using only those needs no typed args.
var needsArgsRe = regexp.MustCompile(`\{(args|[1-9][0-9]*)\}`)

func templateNeedsArgs(template string) bool { return needsArgsRe.MatchString(template) }

// interpolate substitutes {args} (all args), {1}.. (positional), and {user} into an
// admin-authored template. Only called for commands with args:true. Substituted values
// are sanitized (control chars/newlines stripped, length-capped) so user input can't
// inject a second RCON line.
func interpolate(template string, args []string, user, userID string) string {
	return placeholderRe.ReplaceAllStringFunc(template, func(m string) string {
		token := m[1 : len(m)-1]
		switch token {
		case "args":
			return sanitizeArg(strings.Join(args, " "))
		case "user":
			return sanitizeArg(user)
		case "userid":
			return sanitizeArg(userID)
		default:
			if n, err := strconv.Atoi(token); err == nil && n >= 1 && n <= len(args) {
				return sanitizeArg(args[n-1])
			}
			return "" // unknown token or missing positional
		}
	})
}

func sanitizeArg(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r < 0x20 {
			return -1
		}
		return r
	}, s)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// str renders a JSON value as a tidy string (whole-number floats lose the decimal).
func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
