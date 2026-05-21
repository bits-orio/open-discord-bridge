package controlapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testServer() *Server {
	return New("", "secret", Deps{
		Status: func() Status {
			return Status{Transport: "local", Discord: DiscordStatus{Connected: true}}
		},
		Guilds: func() ([]Guild, error) {
			return []Guild{{ID: "1", Name: "Guild One"}}, nil
		},
		Channels: func(guildID string) ([]Channel, error) {
			return []Channel{{ID: "10", Name: "general", Type: 0}}, nil
		},
		Test: func() TestResult {
			return TestResult{OutboundOK: true, InboundOK: true}
		},
		GetConfig: func() Config {
			return Config{Transport: "local", Discord: ConfigDiscord{Routes: []Route{{Source: "*", ChannelID: "1"}}}}
		},
		SetConfig: func(c Config) error {
			if len(c.Discord.Routes) == 0 {
				return errBodyErr("discord.routes must not be empty")
			}
			return nil
		},
		Restart: func() {},
	})
}

// errBodyErr is a tiny helper so the test's SetConfig can return a validation error.
func errBodyErr(msg string) error { return &validationError{msg} }

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func testMux() http.Handler {
	s := testServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("/v1/discord/guilds", s.auth(s.handleGuilds))
	mux.HandleFunc("/v1/discord/channels", s.auth(s.handleChannels))
	mux.HandleFunc("/v1/test", s.auth(s.handleTest))
	mux.HandleFunc("/v1/config", s.auth(s.handleConfig))
	mux.HandleFunc("/v1/restart", s.auth(s.handleRestart))
	return mux
}

func postJSON(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	testMux().ServeHTTP(rr, req)
	return rr
}

func do(t *testing.T, method, path string, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if auth {
		req.Header.Set("Authorization", "Bearer secret")
	}
	rr := httptest.NewRecorder()
	testMux().ServeHTTP(rr, req)
	return rr
}

func TestStatusRequiresAuth(t *testing.T) {
	if rr := do(t, http.MethodGet, "/v1/status", false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rr.Code)
	}
}

func TestStatusWithToken(t *testing.T) {
	rr := do(t, http.MethodGet, "/v1/status", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var st Status
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Transport != "local" || !st.Discord.Connected {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestGuilds(t *testing.T) {
	rr := do(t, http.MethodGet, "/v1/discord/guilds", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var gs []Guild
	if err := json.Unmarshal(rr.Body.Bytes(), &gs); err != nil || len(gs) != 1 {
		t.Fatalf("guilds decode: %v body=%s", err, rr.Body.String())
	}
}

func TestChannelsRequiresGuildID(t *testing.T) {
	if rr := do(t, http.MethodGet, "/v1/discord/channels", true); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing guild_id: got %d, want 400", rr.Code)
	}
	if rr := do(t, http.MethodGet, "/v1/discord/channels?guild_id=1", true); rr.Code != http.StatusOK {
		t.Fatalf("with guild_id: got %d, want 200", rr.Code)
	}
}

func TestRoundTrip(t *testing.T) {
	rr := do(t, http.MethodPost, "/v1/test", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestConfigGet(t *testing.T) {
	rr := do(t, http.MethodGet, "/v1/config", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var c Config
	if err := json.Unmarshal(rr.Body.Bytes(), &c); err != nil || c.Transport != "local" {
		t.Fatalf("config decode: %v body=%s", err, rr.Body.String())
	}
}

func TestConfigPost(t *testing.T) {
	if rr := postJSON(t, "/v1/config", `{"discord":{"routes":[{"source":"*","channel_id":"9"}]}}`); rr.Code != http.StatusOK {
		t.Fatalf("valid post: got %d, want 200", rr.Code)
	}
	if rr := postJSON(t, "/v1/config", `{"discord":{"routes":[]}}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("empty routes: got %d, want 400", rr.Code)
	}
}

func TestRestartAccepted(t *testing.T) {
	if rr := do(t, http.MethodPost, "/v1/restart", true); rr.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rr.Code)
	}
}

// A nil dep falls back to 501.
func TestNilDepReturns501(t *testing.T) {
	s := New("", "secret", Deps{}) // all deps nil
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/discord/guilds", s.auth(s.handleGuilds))
	req := httptest.NewRequest(http.MethodGet, "/v1/discord/guilds", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("got %d, want 501", rr.Code)
	}
}
