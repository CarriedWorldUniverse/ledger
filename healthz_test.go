package ledger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHealthz_ReturnsOK(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(context.Background(), Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz/ledger", nil)
	svc.HealthzHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body struct {
		Status        string `json:"status"`
		SchemaVersion int    `json:"schema_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%q", err, rec.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
	if body.SchemaVersion < 1 {
		t.Errorf("schema_version = %d, want >= 1", body.SchemaVersion)
	}
}
