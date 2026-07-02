// Package controlapi serves the bridge's open HTTP Control API (the /v1/* surface
// documented by pkg/controlapi/spec/openapi.yaml). Any portal or CLI integrates against
// this same contract — there are no privileged endpoints.
package controlapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// Status is the payload returned by GET /v1/status. Mirrors the OpenAPI schema.
type Status struct {
	Transport     string         `json:"transport"`
	Discord       DiscordStatus  `json:"discord"`
	Factorio      FactorioStatus `json:"factorio"`
	LastEventUnix int64          `json:"last_event_unix"`
}

type DiscordStatus struct {
	Connected bool `json:"connected"`
}

type FactorioStatus struct {
	RconAddress        string          `json:"rcon_address"`
	RconOK             bool            `json:"rcon_ok"`
	ModVersion         string          `json:"mod_version,omitempty"`
	Interface          string          `json:"interface,omitempty"`
	RequiredModVersion string          `json:"required_mod_version,omitempty"`
	Sources            json.RawMessage `json:"sources,omitempty"`
	Error              string          `json:"error,omitempty"`
}

type Guild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"`
}

type TestResult struct {
	OutboundOK bool   `json:"outbound_ok"`
	InboundOK  bool   `json:"inbound_ok"`
	Error      string `json:"error,omitempty"`
}

// Config is the GET/POST /v1/config body. Secrets are never included.
type Config struct {
	Transport string         `json:"transport,omitempty"`
	Factorio  ConfigFactorio `json:"factorio"`
	Discord   ConfigDiscord  `json:"discord"`
}

type ConfigFactorio struct {
	RconAddress        string `json:"rcon_address,omitempty"`
	EventsFile         string `json:"events_file,omitempty"`
	RequiredModVersion string `json:"required_mod_version,omitempty"`
}

type ConfigDiscord struct {
	GuildID string  `json:"guild_id,omitempty"`
	Routes  []Route `json:"routes"`
}

type Route struct {
	Source    string `json:"source"`
	ChannelID string `json:"channel_id"`
}

// Deps are the bridge capabilities the API exposes. A nil func means the corresponding
// endpoint reports 501 (Not Implemented).
type Deps struct {
	Status    func() Status
	Guilds    func() ([]Guild, error)
	Channels  func(guildID string) ([]Channel, error)
	Test      func() TestResult
	GetConfig func() Config
	SetConfig func(Config) error
	Restart   func()
}

// maxConfigBodyBytes caps the POST /v1/config request body — config payloads are a small
// list of routes, so this is generous headroom against an oversized/malicious body.
const maxConfigBodyBytes = 256 * 1024

type Server struct {
	listen string
	token  string
	deps   Deps
	srv    *http.Server
}

func New(listen, token string, deps Deps) *Server {
	return &Server{listen: listen, token: token, deps: deps}
}

// Start serves until ctx is cancelled (returns http.ErrServerClosed on clean shutdown).
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("/v1/discord/guilds", s.auth(s.handleGuilds))
	mux.HandleFunc("/v1/discord/channels", s.auth(s.handleChannels))
	mux.HandleFunc("/v1/test", s.auth(s.handleTest))
	mux.HandleFunc("/v1/config", s.auth(s.handleConfig))
	mux.HandleFunc("/v1/restart", s.auth(s.handleRestart))

	s.srv = &http.Server{
		Addr:              s.listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutCtx)
	}()

	log.Printf("controlapi: listening on %s", s.listen)
	return s.srv.ListenAndServe()
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, errBody("unauthorized"))
			return
		}
		next(w, r)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Status())
}

func (s *Server) handleGuilds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	if s.deps.Guilds == nil {
		s.notImplemented(w, r)
		return
	}
	guilds, err := s.deps.Guilds()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, guilds)
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	if s.deps.Channels == nil {
		s.notImplemented(w, r)
		return
	}
	guildID := r.URL.Query().Get("guild_id")
	if guildID == "" {
		writeJSON(w, http.StatusBadRequest, errBody("guild_id query parameter is required"))
		return
	}
	channels, err := s.deps.Channels(guildID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	if s.deps.Test == nil {
		s.notImplemented(w, r)
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Test())
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if s.deps.GetConfig == nil {
		s.notImplemented(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.deps.GetConfig())
	case http.MethodPost:
		if s.deps.SetConfig == nil {
			s.notImplemented(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxConfigBodyBytes)
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("invalid json: "+err.Error()))
			return
		}
		if err := s.deps.SetConfig(cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, s.deps.GetConfig()) // echo the applied config
	default:
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
	}
}

// handleRestart acknowledges with 202, flushes the response, then triggers a graceful
// process exit — a process supervisor (systemd, Docker restart policy) brings the bridge
// back up with fresh config.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	if s.deps.Restart == nil {
		s.notImplemented(w, r)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.deps.Restart()
}

func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errBody("not implemented"))
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
