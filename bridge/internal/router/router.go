package router

import "strings"

type Route struct {
	Source    string
	ChannelID string
}

type Router struct {
	routes []Route
}

func New(routes []Route) *Router {
	return &Router{routes: routes}
}

// Channel returns the first channel whose route matches the event key.
func (r *Router) Channel(eventKey string) (string, bool) {
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
