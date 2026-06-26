// Package status maintains a Factorio server state snapshot and pushes it to a
// Discord channel topic. Updates are debounced with a leading-edge-plus-trailing-edge
// pattern to stay inside Discord's 2-per-10-minute rate limit per channel.
package status

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Setter updates a Discord channel's topic string.
type Setter interface {
	SetChannelTopic(channelID, topic string) error
}

const (
	maxTopic     = 1024
	cooldown     = 5 * time.Minute
	initialDelay = 10 * time.Second
)

// Manager tracks server state and keeps a Discord channel topic in sync.
type Manager struct {
	mu  sync.Mutex
	ch  string // Discord channel ID
	set Setter

	online         bool
	hasEverSeen    bool // true once we've had at least one successful connection
	connectedAt    time.Time
	disconnectedAt time.Time
	players        []string // sorted alphabetically

	// gameTicks is the game.tick snapshot from /odb-status at connection time.
	// gameTicksAt is the wall time of that snapshot. When gameTicksAt is zero,
	// we fall back to time.Since(connectedAt) for the uptime display.
	gameTicks   uint64
	gameTicksAt time.Time

	lastUpdate   time.Time
	pendingTimer *time.Timer
}

// New returns a Manager that writes the topic for channelID via setter.
func New(channelID string, setter Setter) *Manager {
	return &Manager{ch: channelID, set: setter}
}

// OnConnected records a connection event with the current player list and game tick.
// ticks is the game.tick snapshot from /odb-status; nil means the mod didn't report it
// (old version), in which case uptime falls back to wall-clock time since connection.
func (m *Manager) OnConnected(t time.Time, players []string, ticks *uint64) {
	ps := make([]string, len(players))
	copy(ps, players)
	sort.Strings(ps)

	m.mu.Lock()
	m.online = true
	m.hasEverSeen = true
	m.connectedAt = t
	m.players = ps
	if ticks != nil {
		m.gameTicks = *ticks
		m.gameTicksAt = t
	} else {
		m.gameTicksAt = time.Time{}
	}
	m.mu.Unlock()
	m.schedule()
}

// OnDisconnected records a disconnection event.
func (m *Manager) OnDisconnected(t time.Time) {
	m.mu.Lock()
	m.online = false
	m.disconnectedAt = t
	m.players = nil
	m.mu.Unlock()
	m.schedule()
}

// OnPlayerJoined adds a player to the live list.
func (m *Manager) OnPlayerJoined(name string) {
	m.mu.Lock()
	if !hasStr(m.players, name) {
		m.players = append(m.players, name)
		sort.Strings(m.players)
	}
	m.mu.Unlock()
	m.schedule()
}

// OnPlayerLeft removes a player from the live list.
func (m *Manager) OnPlayerLeft(name string) {
	m.mu.Lock()
	m.players = dropStr(m.players, name)
	m.mu.Unlock()
	m.schedule()
}

// schedule queues a topic update.
//
// Before the first ever push: use a short coalesce window (initialDelay) so events
// that arrive immediately after connection (e.g., player joins before the bridge has
// pushed once) are batched rather than being stranded behind a 5-minute cooldown.
//
// After the first push: leading-edge-plus-trailing-edge debounce at the 5-minute
// cooldown to stay inside Discord's 2-per-10-minute rate limit per channel.
func (m *Manager) schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.lastUpdate.IsZero() {
		// No push yet — coalesce into a single push after initialDelay.
		if m.pendingTimer == nil {
			m.pendingTimer = time.AfterFunc(initialDelay, func() {
				m.mu.Lock()
				m.pendingTimer = nil
				m.lastUpdate = time.Now()
				m.mu.Unlock()
				m.push()
			})
		}
		return
	}

	now := time.Now()
	if now.Sub(m.lastUpdate) >= cooldown {
		m.lastUpdate = now
		if m.pendingTimer != nil {
			m.pendingTimer.Stop()
			m.pendingTimer = nil
		}
		go m.push()
		return
	}
	if m.pendingTimer == nil {
		remaining := cooldown - now.Sub(m.lastUpdate)
		m.pendingTimer = time.AfterFunc(remaining, func() {
			m.mu.Lock()
			m.pendingTimer = nil
			m.lastUpdate = time.Now()
			m.mu.Unlock()
			m.push()
		})
	}
}

func (m *Manager) push() {
	m.mu.Lock()
	topic := m.format()
	m.mu.Unlock()
	if err := m.set.SetChannelTopic(m.ch, topic); err != nil {
		log.Printf("status: set channel topic: %v", err)
	}
	m.scheduleRefresh()
}

// scheduleRefresh arms a cooldown-interval timer to re-push when the server is online
// and no event-driven push is already pending. This keeps the uptime counter advancing
// even when no player events arrive.
func (m *Manager) scheduleRefresh() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.online || m.pendingTimer != nil {
		return
	}
	m.pendingTimer = time.AfterFunc(cooldown, func() {
		m.mu.Lock()
		m.pendingTimer = nil
		m.lastUpdate = time.Now()
		m.mu.Unlock()
		m.push()
	})
}

// format builds the topic string. Called with m.mu held.
// Truncates player names when needed to stay within maxTopic chars.
func (m *Manager) format() string {
	if !m.hasEverSeen {
		return "⚫ Connecting..."
	}

	var pfx, uptimePart string
	if m.online {
		pfx = "🟢"
		uptimePart = "⏱ Up " + fmtDur(m.gameUptime())
	} else {
		pfx = "🔴 Offline"
		if !m.disconnectedAt.IsZero() {
			uptimePart = "Last seen " + fmtDur(time.Since(m.disconnectedAt)) + " ago"
		}
	}

	for pc := len(m.players); pc >= 0; pc-- {
		if t := m.build(pfx, uptimePart, pc); len(t) <= maxTopic {
			return t
		}
	}
	return pfx
}

func (m *Manager) build(pfx, uptimePart string, playerCap int) string {
	var parts []string
	if uptimePart != "" {
		parts = append(parts, pfx+" "+uptimePart)
	} else {
		parts = append(parts, pfx)
	}
	if m.online {
		parts = append(parts, playerSection(m.players, playerCap))
	}
	if !m.lastUpdate.IsZero() {
		parts = append(parts, "updated "+m.lastUpdate.Format("15:04"))
	}
	return strings.Join(parts, " 🔹 ")
}

func playerSection(players []string, cap int) string {
	n := len(players)
	if n == 0 {
		return "no players online"
	}
	if cap == 0 {
		return fmt.Sprintf("(%d players)", n)
	}
	shown, extra := players, 0
	if cap < n {
		shown, extra = players[:cap], n-cap
	}
	s := strings.Join(shown, ", ")
	if extra > 0 {
		s += fmt.Sprintf(" +%d more", extra)
	}
	return fmt.Sprintf("%s (%d)", s, n)
}

// gameUptime returns how long the current map has been running. When a game.tick snapshot
// is available it computes base map age (ticks/60s) plus real elapsed time since the
// snapshot was taken. Without tick data it falls back to wall-clock time since connection.
// Called with m.mu held.
func (m *Manager) gameUptime() time.Duration {
	if !m.gameTicksAt.IsZero() {
		base := time.Duration(m.gameTicks/60) * time.Second
		return base + time.Since(m.gameTicksAt)
	}
	return time.Since(m.connectedAt)
}

// fmtDur formats a duration as "Xd Yh", "Xh Ym", or "Xm" (rounded to the minute).
func fmtDur(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	hrs := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0 && hrs > 0:
		return fmt.Sprintf("%dd %dh", days, hrs)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hrs > 0 && mins > 0:
		return fmt.Sprintf("%dh %dm", hrs, mins)
	case hrs > 0:
		return fmt.Sprintf("%dh", hrs)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func hasStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func dropStr(ss []string, s string) []string {
	out := make([]string, 0, len(ss))
	for _, v := range ss {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}
