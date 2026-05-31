package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CarriedWorldUniverse/ledger"
)

// echoIdentity is a tiny terminal handler that reports the claims the
// middleware injected, so tests can assert the mapping without going
// through the DB.
func echoIdentity(w http.ResponseWriter, r *http.Request) {
	c := ledger.AuthFromContext(r.Context())
	if c == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("nil"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(c.Sub + "|" + c.Org))
}

func TestGatewayMiddleware_InjectsIdentity(t *testing.T) {
	mw := buildAuthMiddleware(serverConfig{AuthMode: "gateway"})
	srv := httptest.NewServer(mw(http.HandlerFunc(echoIdentity)))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/issues/my", nil)
	req.Header.Set("X-CWB-Subject", "agent.anvil")
	req.Header.Set("X-CWB-Org", "carried-world")
	req.Header.Set("X-CWB-Scopes", "issue:read issue:write")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if got := string(buf[:n]); got != "agent.anvil|carried-world" {
		t.Errorf("identity = %q", got)
	}
}

func TestGatewayMiddleware_RejectsMissingIdentity(t *testing.T) {
	mw := buildAuthMiddleware(serverConfig{AuthMode: "gateway"})
	srv := httptest.NewServer(mw(http.HandlerFunc(echoIdentity)))
	defer srv.Close()

	// No X-CWB-* headers on a gated path → 401.
	resp, err := http.Get(srv.URL + "/api/issues/my")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing-identity status = %d, want 401", resp.StatusCode)
	}
}

func TestGatewayMiddleware_HealthzIsPublic(t *testing.T) {
	mw := buildAuthMiddleware(serverConfig{AuthMode: "gateway"})
	srv := httptest.NewServer(mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer srv.Close()

	// /healthz/issues must not require identity (k8s probes are tokenless).
	resp, err := http.Get(srv.URL + "/healthz/issues")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}
}

func TestGatewayMiddleware_InsufficientScope(t *testing.T) {
	mw := buildAuthMiddleware(serverConfig{AuthMode: "gateway"})
	srv := httptest.NewServer(mw(http.HandlerFunc(echoIdentity)))
	defer srv.Close()

	// POST create needs issue:write; caller only has issue:read → 403.
	req, _ := http.NewRequest("POST", srv.URL+"/api/issues", nil)
	req.Header.Set("X-CWB-Subject", "agent.anvil")
	req.Header.Set("X-CWB-Org", "carried-world")
	req.Header.Set("X-CWB-Scopes", "issue:read")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("insufficient-scope status = %d, want 403", resp.StatusCode)
	}
}

func TestScopeForMethodPath(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"GET", "/api/issues/my", "issue:read"},
		{"GET", "/api/issues/ready", "issue:read"},
		{"GET", "/api/issues/NEX-1", "issue:read"},
		{"POST", "/api/issues/search", "issue:read"},
		{"POST", "/api/issues", "issue:write"},
		{"PATCH", "/api/issues/NEX-1", "issue:write"},
		{"POST", "/api/issues/NEX-1/transition", "issue:write"},
		{"POST", "/api/issues/NEX-1/comments", "issue:write"},
		{"POST", "/api/issues/NEX-1/claim", "issue:claim"},
		{"GET", "/api/admin/orgs", "issue:admin"},
		{"POST", "/api/projects", "issue:admin"}, // project create is structural
		{"GET", "/api/projects", "issue:read"},    // listing is a plain read
		{"DELETE", "/api/org", "org:purge"},        // NEX-402 cross-org wipe
	}
	for _, c := range cases {
		if got := scopeForMethodPath(c.method, c.path); got != c.want {
			t.Errorf("%s %s → %q, want %q", c.method, c.path, got, c.want)
		}
	}
}
