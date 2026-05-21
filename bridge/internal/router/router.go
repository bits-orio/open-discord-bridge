package router

import (
	"strings"
	"sync"
)

type Route struct {
	Source    string
	ChannelID string
}

// Router maps event keys to channels. Safe for concurrent reads (tailer, gateway) and
// live updates (Control API).
type Router struct {
	mu     sync.RWMutex
	routes []Route
}

func New(routes []Route) *Router {
	return &Router{routes: routes}
}

// Update atomically replaces the routing table.
func (r *Router) Update(routes []Route) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = routes
}

// Routes returns a copy of the current routing table.
func (r *Router) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	return out
}

// Channel returns the first channel whose route matches the event key.
func (r *Router) Channel(eventKey string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rt := range r.routes {
		if matches(rt.Source, eventKey) {
			return rt.ChannelID, true
		}
	}
	return "", false
}

// InboundChannels is the set of channels the bridge writes to; messages posted there
// by humans are relayed into the game.
func (r *Router) InboundChannels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, rt := range r.routes {
		if rt.ChannelID != "" && !seen[rt.ChannelID] {
			seen[rt.ChannelID] = true
			out = append(out, rt.ChannelID)
		}
	}
	return out
}

// matches supports exact keys ("vanilla.chat"), namespace globs ("mts.*"), and "*".
func matches(pattern, key string) bool {
	if pattern == "*" || pattern == key {
		return true
	}
	if ns, ok := strings.CutSuffix(pattern, ".*"); ok {
		return key == ns || strings.HasPrefix(key, ns+".")
	}
	return false
}
