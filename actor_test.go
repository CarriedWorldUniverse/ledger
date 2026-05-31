package ledger

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// postWithSubject drives a request through Service.Handler with an
// injected herald Subject and a body actor that should be OVERRIDDEN.
func postWithSubject(t *testing.T, svc *Service, method, path, subject, body string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuth(req.Context(), &AuthClaims{Sub: subject}))
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code >= 400 {
		t.Fatalf("%s %s → %d: %s", method, path, rec.Code, rec.Body.String())
	}
}

func TestActorTagging_HeraldSubjectOverridesBodyActor(t *testing.T) {
	ctx := context.Background()
	svc := verbsTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	// Comment with a LYING body actor; the herald Subject must win.
	postWithSubject(t, svc, "POST", "/api/issues/"+iss.Key+"/comments",
		"agent.anvil", `{"actor":"impersonated","body":"hello"}`)

	tl, _ := svc.Timeline(ctx, iss.Key)
	var commentActor string
	for _, e := range tl {
		if e.Kind == "comment" {
			commentActor = e.Actor
		}
	}
	if commentActor != "agent.anvil" {
		t.Fatalf("comment actor = %q, want agent.anvil (herald Subject wins)", commentActor)
	}
}

func TestActorTagging_EmbeddedPathKeepsBodyActor(t *testing.T) {
	ctx := context.Background()
	svc := verbsTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	// No claims context (embedded): body actor is honoured.
	req := httptest.NewRequest("POST", "/api/issues/"+iss.Key+"/comments",
		strings.NewReader(`{"actor":"shadow","body":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code >= 400 {
		t.Fatalf("comment → %d: %s", rec.Code, rec.Body.String())
	}

	tl, _ := svc.Timeline(ctx, iss.Key)
	var commentActor string
	for _, e := range tl {
		if e.Kind == "comment" {
			commentActor = e.Actor
		}
	}
	if commentActor != "shadow" {
		t.Fatalf("embedded comment actor = %q, want shadow", commentActor)
	}
}
