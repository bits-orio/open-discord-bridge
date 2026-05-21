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

// Server is the HTTP Control API. StatusFn supplies a fresh snapshot per request.
type Server struct {
	listen   string
	token    string
	statusFn func() Status
	srv      *http.Server
}

func New(listen, token string, statusFn func() Status) *Server {
	return &Server{listen: listen, token: token, statusFn: statusFn}
}

// Start serves until ctx is cancelled. Returns the listener error (http.ErrServerClosed
// on clean shutdown).
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.auth(s.handleStatus))
	// Documented in the spec; implemented in a later phase.
	for _, p := range []string{"/v1/config", "/v1/restart", "/v1/test", "/v1/discord/guilds", "/v1/discord/channels"} {
		mux.HandleFunc(p, s.auth(s.notImplemented))
	}

	s.srv = &http.Server{
		Addr:              s.listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
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

// auth enforces a constant-time bearer-token check.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, s.statusFn())
}

func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
