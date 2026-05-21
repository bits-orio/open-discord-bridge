package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"sort"
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

	// Discord → game.
	onInbound := func(user, message, channelID string) {
		if strings.TrimSpace(message) == "!players" {
			if resp, err := rc.Execute("/players online"); err == nil {
				dc.Send(channelID, "```\n"+resp+"\n```")
			}
			return
		}
		payload, _ := json.Marshal(map[string]string{
			"user":    user,
			"message": message,
			"channel": channelID,
		})
		if _, err := rc.Execute("/odb-incoming " + string(payload)); err != nil {
			log.Printf("rcon: inbound deliver failed: %v", err)
		}
	}

	dc, err = discord.New(cfg.Discord.Token, rt.InboundChannels(), onInbound)
	if err != nil {
		log.Fatalf("discord: %v", err)
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

	// Game → Discord.
	var lastEvent atomic.Int64
	tail := transport.NewLocal(cfg.Factorio.EventsFile, cfg.Interval())
	onLine := func(line []byte) {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			log.Printf("transport: bad JSONL line: %v", err)
			return
		}
		lastEvent.Store(time.Now().Unix())
		channel, ok := rt.Channel(ev.Event)
		if !ok {
			return // no route for this event; drop silently
		}
		dc.Send(channel, formatEvent(ev))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go tail.Run(ctx, onLine)

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
