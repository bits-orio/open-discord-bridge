package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/config"
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

	// Game → Discord.
	tail := transport.NewLocal(cfg.Factorio.EventsFile, cfg.Interval())
	onLine := func(line []byte) {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			log.Printf("transport: bad JSONL line: %v", err)
			return
		}
		channel, ok := rt.Channel(ev.Event)
		if !ok {
			return // no route for this event; drop silently
		}
		dc.Send(channel, formatEvent(ev))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go tail.Run(ctx, onLine)

	<-ctx.Done()
	log.Printf("bridge: shutting down")
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
		b, _ := json.Marshal(d)
		return fmt.Sprintf("`%s` %s", ev.Event, string(b))
	}
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
