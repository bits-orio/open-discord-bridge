package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"net/http"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/config"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/controlapi"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/discord"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/rcon"
	"github.com/bits-orio/open-discord-bridge/bridge/internal/router"
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

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

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
			if reply := runCommand(rc, cmd, isAdmin, commandArgs(msg.Message), msg.User, msg.UserID); reply != "" {
				dc.Send(msg.ChannelID, reply)
			}
			return
		}
		payload, _ := json.Marshal(map[string]string{
			"user":    msg.User,
			"user_id": msg.UserID,
			"message": msg.Message,
			"channel": msg.ChannelID,
		})
		if _, err := rc.Execute("/odb-incoming " + string(payload)); err != nil {
			log.Printf("rcon: inbound deliver failed: %v", err)
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

	// Version handshake: confirm the companion mod is reachable and log its version.
	if v := queryModVersion(rc); v != "" {
		log.Printf("bridge: companion mod version %s", v)
	} else {
		log.Printf("bridge: companion mod not reachable over RCON yet (will retry on demand)")
	}

	// Route an event to its channel and post it. In embed mode everything is a colored
	// embed except chat (kept as plain text, since chat-as-embeds reads poorly).
	emit := func(ev Event) {
		channel, ok := rt.Channel(ev.Event)
		if !ok {
			return
		}
		if cfg.Discord.Embed && !isChatEvent(ev.Event) {
			dc.SendEmbed(channel, formatEvent(ev), eventColor(ev.Event))
		} else {
			dc.Send(channel, formatEvent(ev))
		}
	}

	// Startup permission preflight: warn (logs + Discord, with fix steps) about anything
	// the bot is missing for the configured features.
	go func() {
		rep := dc.CheckPermissions(discord.PermissionCheck{
			GuildID:     cfg.Discord.GuildID,
			NeedEmbed:   cfg.Discord.Embed,
			NeedRoles:   cfg.Discord.LinkedRoleID != "",
			NeedNicks:   cfg.Discord.LinkedNickname != "",
			RoleAboveID: cfg.Discord.LinkedRoleID,
		})
		if rep.Err != "" {
			log.Printf("bridge: could not check permissions: %s", rep.Err)
			return
		}
		if rep.OK() {
			return
		}
		if len(rep.Missing) > 0 {
			log.Printf("bridge: bot is missing permissions: %s", strings.Join(rep.Missing, ", "))
		}
		if rep.Hierarchy {
			log.Printf("bridge: bot's role is not above the linked role")
		}
		// Plain text (not an embed) so it posts even if Embed Links is the missing perm.
		if ch, ok := rt.Channel("bridge.warning"); ok {
			dc.Send(ch, permissionHelp(rep))
		}
	}()

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
	onLine := func(line []byte) {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			log.Printf("transport: bad JSONL line: %v", err)
			return
		}
		lastEvent.Store(time.Now().Unix())
		emit(ev)
	}

	// baseCtx lets the Control API request a restart (clean exit → supervisor restarts).
	baseCtx, requestRestart := context.WithCancel(context.Background())
	defer requestRestart()
	ctx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go tail.Run(ctx, onLine)

	// Keep a Discord role / nickname in sync with linked players.
	if (cfg.Discord.LinkedRoleID != "" || cfg.Discord.LinkedNickname != "") && cfg.Discord.GuildID != "" {
		go syncLinkedMembers(ctx, rc, dc, cfg.Discord.GuildID, cfg.Discord.LinkedRoleID, cfg.Discord.LinkedNickname)
	}

	// Watch the bridge↔Factorio link (RCON + mod handshake) and announce transitions.
	if cfg.Discord.AnnounceStatus {
		go monitorConnection(ctx, rc, func(connected bool, version string) {
			if connected {
				emit(Event{Event: "bridge.established", Data: map[string]any{"version": version}})
			} else {
				emit(Event{Event: "bridge.disconnected"})
			}
		})
	}

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
func monitorConnection(ctx context.Context, rc *rcon.Client, announce func(connected bool, version string)) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	var last *bool
	check := func() {
		version := queryModVersion(rc)
		cur := version != ""
		switch {
		case last == nil:
			if cur {
				announce(true, version)
			}
			last = &cur
		case *last != cur:
			announce(cur, version)
			*last = cur
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

	seen := map[string]bool{}
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
			if seen[id] {
				continue
			}
			if roleID != "" {
				if err := dc.AddRole(guildID, id, roleID); err != nil {
					log.Printf("linked-member: add role to %s failed: %v", id, err)
					continue // retry next poll
				}
			}
			if nickFormat != "" {
				if err := dc.SetNickname(guildID, id, nickFor(nickFormat, l)); err != nil {
					log.Printf("linked-member: set nickname for %s failed: %v", id, err)
				}
			}
			seen[id] = true
		}
		for id := range seen { // newly unlinked → revert
			if _, still := cur[id]; still {
				continue
			}
			if roleID != "" {
				if err := dc.RemoveRole(guildID, id, roleID); err != nil {
					log.Printf("linked-member: remove role from %s failed: %v", id, err)
					continue // keep, retry next poll
				}
			}
			if nickFormat != "" {
				_ = dc.SetNickname(guildID, id, "") // clear
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

// queryModVersion asks the companion mod for its version over RCON ("" if unreachable).
func queryModVersion(rc *rcon.Client) string {
	resp, err := rc.Execute("/odb-status")
	if err != nil {
		return ""
	}
	var ms struct {
		ModVersion string `json:"mod_version"`
	}
	_ = json.Unmarshal([]byte(resp), &ms)
	return ms.ModVersion
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
	default:
		return formatGeneric(ev.Event, d)
	}
}

// formatGeneric renders any non-baseline event (mts.*, oarc.*, custom.*, ...) without
// hardcoding mod knowledge: a humanized "[namespace → event]" label, followed by the
// integrator-supplied "text" sentence if present, else a readable key=value summary.
func formatGeneric(eventKey string, data map[string]any) string {
	label := humanizeKey(eventKey)
	if s := firstString(data, "text", "message"); s != "" {
		return label + " " + s
	}
	if kv := kvSummary(data); kv != "" {
		return label + " " + kv
	}
	return label
}

// humanizeKey turns "mts.team_created" into "[mts → team created]" and a key with no
// namespace like "custom" into "[custom]".
func humanizeKey(eventKey string) string {
	ns, name, found := strings.Cut(eventKey, ".")
	if !found {
		return "[" + strings.ReplaceAll(ns, "_", " ") + "]"
	}
	return fmt.Sprintf("[%s → %s]", ns, strings.ReplaceAll(name, "_", " "))
}

func firstString(data map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := data[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// kvSummary is the fallback when an event carries no "text": sorted key=value pairs,
// skipping the "text"/"message" keys themselves.
func kvSummary(data map[string]any) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		if k == "text" || k == "message" {
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
// event keyed "chat" or "<namespace>.chat" is chat. Such events render as plain text even
// in embed mode, so integrators can mark chat-style relay (e.g. "mts.chat") vs notable
// events (e.g. "mts.team_created", which embed) purely by how they name the event.
func isChatEvent(eventKey string) bool {
	return eventKey == "chat" || strings.HasSuffix(eventKey, ".chat")
}

// eventColor maps an event key to an embed color (used when discord.embed is on).
func eventColor(eventKey string) int {
	switch eventKey {
	case "vanilla.chat":
		return 0x5865F2 // blurple
	case "vanilla.player_joined":
		return 0x57F287 // green
	case "vanilla.player_left":
		return 0x99AAB5 // grey
	case "vanilla.player_died":
		return 0xED4245 // red
	case "vanilla.rocket_launched":
		return 0x3498DB // blue
	case "vanilla.research_finished":
		return 0x9B59B6 // purple
	case "vanilla.game_started":
		return 0x1ABC9C // teal
	case "bridge.established":
		return 0x57F287 // green
	case "bridge.disconnected":
		return 0xED4245 // red
	default:
		// Any other event (mts.*, oarc.*, custom.*) gets a stable, distinct color
		// derived from its key — no per-mod hardcoding, but each event type looks
		// intentionally colored and consistent.
		return hashColor(eventKey)
	}
}

// hashColor maps a string to a stable, pleasant color: the key's hash picks a hue, with
// fixed saturation/lightness so every event type is distinct yet vibrant.
func hashColor(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	hue := float64(h.Sum32() % 360)
	r, g, b := hslToRGB(hue, 0.65, 0.55)
	return r<<16 | g<<8 | b
}

func hslToRGB(h, s, l float64) (int, int, int) {
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return int((r+m)*255 + 0.5), int((g+m)*255 + 0.5), int((b+m)*255 + 0.5)
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
