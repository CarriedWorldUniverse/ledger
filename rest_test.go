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

func TestREST_WatchUnwatchRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "watch me",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	// POST /api/issues/{key}/watchers
	body, _ := json.Marshal(map[string]any{"aspect": "plumb", "actor": "plumb"})
	resp, err := http.Post(srv.URL+"/api/issues/"+issue.Key+"/watchers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("watch status = %d", resp.StatusCode)
	}

	// GET /api/issues/{key}/watchers
	resp2, err := http.Get(srv.URL + "/api/issues/" + issue.Key + "/watchers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp2.StatusCode)
	}
	var list []string
	_ = json.NewDecoder(resp2.Body).Decode(&list)
	if len(list) != 1 || list[0] != "plumb" {
		t.Errorf("watchers = %v, want [plumb]", list)
	}

	// DELETE /api/issues/{key}/watchers
	body3, _ := json.Marshal(map[string]any{"aspect": "plumb", "actor": "plumb"})
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/issues/"+issue.Key+"/watchers", bytes.NewReader(body3))
	req.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("unwatch status = %d", resp3.StatusCode)
	}

	// Verify empty.
	list2, _ := svc.Watchers(ctx, issue.Key)
	if len(list2) != 0 {
		t.Errorf("watchers after unwatch = %v", list2)
	}
}

func TestREST_UpdatesEndpoint(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, err = svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		AssigneeAspect: "anvil",
	})
	if err != nil {
		t.Fatal(err)
	}

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// GET /api/issues/updates?aspect=anvil
	resp, err := http.Get(srv.URL + "/api/issues/updates?aspect=anvil")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var events []Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Error("expected non-empty events for assigned aspect")
	}

	// Missing aspect returns 400.
	resp2, err := http.Get(srv.URL + "/api/issues/updates")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing aspect; got %d", resp2.StatusCode)
	}

	// Irrelevant aspect returns empty array.
	resp3, err := http.Get(srv.URL + "/api/issues/updates?aspect=forge")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	var empty []Event
	_ = json.NewDecoder(resp3.Body).Decode(&empty)
	if len(empty) != 0 {
		t.Errorf("expected empty for irrelevant aspect; got %v", empty)
	}
}

// NEX-324: GET /api/projects backs the issue.list_projects MCP tool.
// Aspects need to discover the keyspace they can create issues against.
func TestREST_ListProjects(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "WAKE", Name: "WakeStone"})

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var projects []Project
	_ = json.NewDecoder(resp.Body).Decode(&projects)
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
}
