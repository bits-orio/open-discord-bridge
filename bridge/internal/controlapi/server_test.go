package controlapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	s := New("", "secret", func() Status {
		return Status{Transport: "local", Discord: DiscordStatus{Connected: true}}
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.auth(s.handleStatus))
	mux.HandleFunc("/v1/config", s.auth(s.notImplemented))
	return mux
}

func TestStatusRequiresAuth(t *testing.T) {
	rr := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rr.Code)
	}
}

func TestStatusWithToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rr, req)
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

func TestStubReturns501(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("got %d, want 501", rr.Code)
	}
}
