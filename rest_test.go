package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestREST_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// POST /api/issues
	body, _ := json.Marshal(map[string]any{
		"project":            "NEX",
		"type":               "Story",
		"summary":            "via rest",
		"definition_of_done": "- [ ] go",
		"reporter":           "shadow",
	})
	resp, err := http.Post(srv.URL+"/api/issues", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var created struct{ Key string }
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.Key != "NEX-1" {
		t.Errorf("key = %q", created.Key)
	}

	// GET /api/issues/NEX-1
	resp2, err := http.Get(srv.URL + "/api/issues/" + created.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", resp2.StatusCode)
	}
}
