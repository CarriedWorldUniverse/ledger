package ledger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func verbsTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	svc, err := New(context.Background(), Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestHandleMy_ScopedToCallerViaQuery(t *testing.T) {
	ctx := context.Background()
	svc := verbsTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "mine",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "theirs",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "keel"})

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/issues/my?aspect=anvil")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var refs []IssueRef
	_ = json.NewDecoder(resp.Body).Decode(&refs)
	if len(refs) != 1 || refs[0].AssigneeAspect != "anvil" {
		t.Fatalf("my = %+v", refs)
	}
}

func TestHandleReady_ScopedToCallerViaQuery(t *testing.T) {
	ctx := context.Background()
	svc := verbsTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "ready",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/issues/ready?aspect=anvil")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var refs []IssueRef
	_ = json.NewDecoder(resp.Body).Decode(&refs)
	if len(refs) != 1 {
		t.Fatalf("ready = %+v", refs)
	}
}

func TestHandleMy_PrefersClaimsSubjectOverQuery(t *testing.T) {
	ctx := context.Background()
	svc := verbsTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "mine",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

	// Inject claims for anvil; pass a contradictory ?aspect=keel. Claims win.
	req := httptest.NewRequest("GET", "/api/issues/my?aspect=keel", nil)
	req = req.WithContext(ContextWithAuth(req.Context(), &AuthClaims{Sub: "anvil"}))
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var refs []IssueRef
	_ = json.NewDecoder(rec.Body).Decode(&refs)
	if len(refs) != 1 || refs[0].AssigneeAspect != "anvil" {
		t.Fatalf("claims-scoped my = %+v", refs)
	}
}
