package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"axamp/internal/config"
	"axamp/internal/supervisor"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.DefaultConfig(t.TempDir())
	cfg.RemoteAccess = false
	return NewServer(cfg, supervisor.NewManager(cfg))
}

// do invokes the full middleware chain (hostGuard + routes) in-process, so a
// forged RemoteAddr survives — unlike a real httptest listener, whose TCP
// connection would overwrite it with 127.0.0.1.
func do(t *testing.T, s *Server, method, path, remoteAddr, host, pin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	if host != "" {
		req.Host = host
	}
	if pin != "" {
		req.Header.Set("X-AXAMP-PIN", pin)
	}
	w := httptest.NewRecorder()
	s.hostGuard(s.routes()).ServeHTTP(w, req)
	return w
}

// The /api/state response must never leak the RemotePIN (it used to ship the
// whole config, secrets included).
func TestStateResponseHidesPIN(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/state", "127.0.0.1:55555", "127.0.0.1:4040", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/state: expected 200, got %d", w.Code)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	body, _ := json.Marshal(payload)
	cfgObj, _ := json.Marshal(payload["config"])
	if strings.Contains(string(body), s.cfg.RemotePIN) {
		t.Fatalf("state response leaks the remote PIN")
	}
	if strings.Contains(string(cfgObj), "remote_pin") {
		t.Fatalf("state config object still carries the remote_pin field")
	}
}

// A request arriving from the local machine under a non-loopback Host header
// is the DNS-rebinding signature and must be rejected, regardless of path.
func TestHostGuardBlocksRebinding(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/state", "127.0.0.1:55555", "attacker.example.com", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("rebinding request: expected 403, got %d", w.Code)
	}

	w = do(t, s, http.MethodGet, "/api/state", "127.0.0.1:55555", "127.0.0.1:4040", "")
	if w.Code != http.StatusOK {
		t.Fatalf("loopback host request: expected 200, got %d", w.Code)
	}
}

// Remote PIN guessing must be throttled: after maxPinFailures wrong attempts
// the source IP receives 429 even with the correct PIN.
func TestPinBruteForceLockout(t *testing.T) {
	s := newTestServer(t)
	s.cfg.RemoteAccess = true

	for i := 0; i < maxPinFailures; i++ {
		w := do(t, s, http.MethodGet, "/api/live-stats", "10.9.8.7:40000", "", "wrong-pin-"+strings.Repeat("x", i))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Code)
		}
	}
	w := do(t, s, http.MethodGet, "/api/live-stats", "10.9.8.7:40000", "", s.cfg.RemotePIN)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("locked-out request with correct PIN: expected 429, got %d", w.Code)
	}

	// A different source IP is not affected by another IP's failures.
	w = do(t, s, http.MethodGet, "/api/live-stats", "10.9.8.8:40000", "", s.cfg.RemotePIN)
	if w.Code != http.StatusOK {
		t.Fatalf("different IP with correct PIN: expected 200, got %d", w.Code)
	}
}

// Remote access stays fully blocked while disabled, PIN or not.
func TestRemoteDeniedWhenDisabled(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/live-stats", "10.9.8.7:40000", "", s.cfg.RemotePIN)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote request with RemoteAccess disabled: expected 403, got %d", w.Code)
	}
}

// /api/pin is the only place the PIN is served, and only to the local machine.
func TestPinEndpointLocalOnly(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/pin", "127.0.0.1:55555", "127.0.0.1:4040", "")
	if w.Code != http.StatusOK {
		t.Fatalf("local /api/pin: expected 200, got %d", w.Code)
	}
	var payload map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&payload)
	if payload["remote_pin"] == "" || payload["remote_pin"] == nil {
		t.Fatalf("local /api/pin must return the PIN for the owner")
	}

	s.cfg.RemoteAccess = true // even with remote access on, PIN stays local-only
	w = do(t, s, http.MethodGet, "/api/pin", "10.9.8.7:40000", "", s.cfg.RemotePIN)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote /api/pin: expected 403, got %d", w.Code)
	}
}

// The control plane must stay on loopback unless remote access is enabled.
func TestBindHostLoopbackByDefault(t *testing.T) {
	s := newTestServer(t)
	if got := s.bindHost(); got != "127.0.0.1" {
		t.Fatalf("default bind host: expected 127.0.0.1, got %s", got)
	}
	s.cfg.RemoteAccess = true
	if got := s.bindHost(); got != "0.0.0.0" {
		t.Fatalf("remote-access bind host: expected 0.0.0.0, got %s", got)
	}
}
