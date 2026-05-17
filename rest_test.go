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

func TestREST_TransitionRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"status": "In Progress", "actor": "anvil"})
	resp, err := http.Post(srv.URL+"/api/issues/"+issue.Key+"/transition", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transition status = %d", resp.StatusCode)
	}
}

func TestREST_PatchRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "original",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	newSummary := "patched summary"
	body, _ := json.Marshal(map[string]any{"summary": newSummary, "actor": "anvil"})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/issues/"+issue.Key, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", resp.StatusCode)
	}

	got, _ := svc.GetIssue(ctx, issue.Key)
	if got.Summary != newSummary {
		t.Errorf("summary = %q, want %q", got.Summary, newSummary)
	}
}

func TestREST_AssignRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "assign me",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"aspect": "anvil", "actor": "shadow"})
	resp, err := http.Post(srv.URL+"/api/issues/"+issue.Key+"/assign", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign status = %d", resp.StatusCode)
	}

	got, _ := svc.GetIssue(ctx, issue.Key)
	if got.AssigneeAspect != "anvil" {
		t.Errorf("assignee = %q, want %q", got.AssigneeAspect, "anvil")
	}
}
