package router

import "testing"

func TestChannelMatching(t *testing.T) {
	r := New([]Route{
		{Source: "vanilla.chat", ChannelID: "chat"},
		{Source: "mts.*", ChannelID: "teams"},
		{Source: "*", ChannelID: "fallback"},
	})

	cases := map[string]string{
		"vanilla.chat":            "chat",     // exact
		"mts.team_milestone":      "teams",    // namespace glob
		"mts":                     "teams",    // glob matches bare namespace
		"vanilla.rocket_launched": "fallback", // catch-all
		"oarc.spawn":              "fallback",
	}
	for event, want := range cases {
		got, ok := r.Channel(event)
		if !ok || got != want {
			t.Errorf("Channel(%q) = %q,%v; want %q", event, got, ok, want)
		}
	}
}

func TestInboundChannelsDeduped(t *testing.T) {
	r := New([]Route{
		{Source: "vanilla.chat", ChannelID: "a"},
		{Source: "*", ChannelID: "a"},
		{Source: "mts.*", ChannelID: "b"},
	})
	got := r.InboundChannels()
	if len(got) != 2 {
		t.Fatalf("InboundChannels() = %v; want 2 unique", got)
	}
}
