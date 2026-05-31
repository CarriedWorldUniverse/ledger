package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CarriedWorldUniverse/ledger"
)

func TestBuildHandler_HealthzAndMux(t *testing.T) {
	dir := t.TempDir()
	svc, err := ledger.New(context.Background(), ledger.Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	h := buildHandler(svc, serverConfig{AuthMode: "gateway"})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz/issues")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}
}
