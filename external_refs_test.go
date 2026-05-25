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

// externalRefsRig spins up a service + REST handler and seeds a project,
// returning the URL and a cleanup. Mirrors the rig pattern in
// links_rest_test.go.
func externalRefsRig(t *testing.T) (*Service, string, func()) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	srv := httptest.NewServer(svc.Handler())
	return svc, srv.URL, func() { srv.Close(); svc.Close() }
}

func TestExternalRefs_RoundTripViaService(t *testing.T) {
	svc, _, cleanup := externalRefsRig(t)
	defer cleanup()
	ctx := context.Background()

	refs := []ExternalRef{
		{Tracker: "jira", Key: "JIRA-1", URL: "https://example.atlassian.net/browse/JIRA-1"},
		{Tracker: "github", Key: "owner/repo#42", URL: "https://github.com/owner/repo/issues/42", Description: "upstream report"},
	}
	created, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "with refs",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
		ExternalRefs: refs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ExternalRefs) != 2 {
		t.Fatalf("created.ExternalRefs len = %d, want 2", len(created.ExternalRefs))
	}

	got, err := svc.GetIssue(ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ExternalRefs) != 2 {
		t.Fatalf("get.ExternalRefs len = %d, want 2", len(got.ExternalRefs))
	}
	if got.ExternalRefs[0].Tracker != "jira" || got.ExternalRefs[0].URL == "" {
		t.Errorf("first ref wrong: %+v", got.ExternalRefs[0])
	}
	if got.ExternalRefs[1].Description != "upstream report" {
		t.Errorf("second ref description lost: %+v", got.ExternalRefs[1])
	}
}

func TestExternalRefs_EmptyIsNil(t *testing.T) {
	svc, _, cleanup := externalRefsRig(t)
	defer cleanup()
	ctx := context.Background()
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "no refs",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if issue.ExternalRefs != nil {
		t.Errorf("unset refs should be nil, got %+v", issue.ExternalRefs)
	}
}

func TestExternalRefs_UpdateReplacesEntireSet(t *testing.T) {
	svc, _, cleanup := externalRefsRig(t)
	defer cleanup()
	ctx := context.Background()

	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "patch test",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
		ExternalRefs: []ExternalRef{{Tracker: "jira", Key: "JIRA-1", URL: "u1"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Replace with a different set.
	newRefs := []ExternalRef{{Tracker: "jira", Key: "JIRA-99", URL: "u99"}}
	if err := svc.UpdateIssue(ctx, issue.Key, UpdatePatch{ExternalRefs: &newRefs}, "shadow"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetIssue(ctx, issue.Key)
	if len(got.ExternalRefs) != 1 || got.ExternalRefs[0].Key != "JIRA-99" {
		t.Errorf("after patch, refs = %+v", got.ExternalRefs)
	}

	// Clear via empty slice pointer.
	empty := []ExternalRef{}
	if err := svc.UpdateIssue(ctx, issue.Key, UpdatePatch{ExternalRefs: &empty}, "shadow"); err != nil {
		t.Fatal(err)
	}
	got2, _ := svc.GetIssue(ctx, issue.Key)
	if got2.ExternalRefs != nil {
		t.Errorf("after clear, refs should be nil, got %+v", got2.ExternalRefs)
	}
}

func TestExternalRefs_RoundTripViaREST(t *testing.T) {
	_, url, cleanup := externalRefsRig(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"project":            "NEX",
		"type":               "Story",
		"summary":            "rest with refs",
		"definition_of_done": "- [x] done",
		"reporter":           "shadow",
		"external_refs": []map[string]string{
			{"tracker": "jira", "key": "JIRA-7", "url": "https://example/JIRA-7"},
		},
	})
	resp, err := http.Post(url+"/api/issues", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		Key          string        `json:"Key"`
		ExternalRefs []ExternalRef `json:"ExternalRefs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if len(created.ExternalRefs) != 1 || created.ExternalRefs[0].Key != "JIRA-7" {
		t.Errorf("response refs wrong: %+v", created.ExternalRefs)
	}

	// PATCH to add a second ref.
	patchBody, _ := json.Marshal(map[string]any{
		"external_refs": []map[string]string{
			{"tracker": "jira", "key": "JIRA-7", "url": "https://example/JIRA-7"},
			{"tracker": "github", "key": "owner/repo#9", "url": "https://github.com/owner/repo/issues/9"},
		},
		"actor": "shadow",
	})
	req, _ := http.NewRequest(http.MethodPatch, url+"/api/issues/"+created.Key, bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", resp2.StatusCode)
	}

	// Verify via GET ?format=raw.
	resp3, err := http.Get(url + "/api/issues/" + created.Key + "?format=raw")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	var got struct {
		ExternalRefs []ExternalRef `json:"ExternalRefs"`
	}
	_ = json.NewDecoder(resp3.Body).Decode(&got)
	if len(got.ExternalRefs) != 2 {
		t.Errorf("after patch, GET shows %d refs, want 2", len(got.ExternalRefs))
	}
}
