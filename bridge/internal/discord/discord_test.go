package discord

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// captureTransport is a minimal http.RoundTripper fake: it records the last request body
// and returns a canned 200 response, so Post/PostMentioning/SendDM can be exercised without
// hitting the real Discord API. discordgo.Session.Client is a plain *http.Client, so swapping
// its Transport is enough — no need for a fuller mock.
type captureTransport struct {
	lastBody []byte
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		t.lastBody, _ = io.ReadAll(req.Body)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
		Header:     make(http.Header),
	}, nil
}

// newTestClient returns a discord.Client wired to a captureTransport in place of the real
// Discord REST API, plus the transport so the test can inspect what was sent.
func newTestClient(t *testing.T) (*Client, *captureTransport) {
	t.Helper()
	sess, err := discordgo.New("Bot faketoken")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	ct := &captureTransport{}
	sess.Client.Transport = ct
	return &Client{session: sess}, ct
}

// allowedMentionsOf decodes the allowed_mentions object off the last captured request body.
func allowedMentionsOf(t *testing.T, body []byte) discordgo.MessageAllowedMentions {
	t.Helper()
	var payload struct {
		AllowedMentions discordgo.MessageAllowedMentions `json:"allowed_mentions"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode request body %s: %v", body, err)
	}
	return payload.AllowedMentions
}

func TestPost_SuppressesAllMentions(t *testing.T) {
	c, ct := newTestClient(t)
	if err := c.Post("chan1", "@everyone <@123> <@&456> hi"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	am := allowedMentionsOf(t, ct.lastBody)
	if len(am.Parse) != 0 {
		t.Errorf("Post: Parse = %v; want empty (no mentions parsed)", am.Parse)
	}
	if len(am.Users) != 0 || len(am.Roles) != 0 {
		t.Errorf("Post: Users=%v Roles=%v; want none allowlisted", am.Users, am.Roles)
	}
}

func TestPostMentioning_AllowlistsOnlyGivenUsers(t *testing.T) {
	c, ct := newTestClient(t)
	if err := c.PostMentioning("chan1", "<@42> your DM bounced", "42"); err != nil {
		t.Fatalf("PostMentioning: %v", err)
	}
	am := allowedMentionsOf(t, ct.lastBody)
	if len(am.Parse) != 0 {
		t.Errorf("PostMentioning: Parse = %v; want empty (still no @everyone/@here/role parsing)", am.Parse)
	}
	if len(am.Roles) != 0 {
		t.Errorf("PostMentioning: Roles = %v; want none", am.Roles)
	}
	if len(am.Users) != 1 || am.Users[0] != "42" {
		t.Errorf("PostMentioning: Users = %v; want [42]", am.Users)
	}
}

func TestSendDM_SuppressesAllMentions(t *testing.T) {
	c, ct := newTestClient(t)
	// SendDM first opens a DM channel (GET/POST to users/@me/channels), then sends the
	// message — capture only reflects the last request, so check after Send.
	if err := c.SendDM("42", "@everyone hi"); err != nil {
		t.Fatalf("SendDM: %v", err)
	}
	am := allowedMentionsOf(t, ct.lastBody)
	if len(am.Parse) != 0 || len(am.Users) != 0 || len(am.Roles) != 0 {
		t.Errorf("SendDM: allowed_mentions = %+v; want fully suppressed", am)
	}
}
