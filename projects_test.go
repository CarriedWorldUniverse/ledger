package ledger

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// TestHandleCreateProject_HTTP exercises POST /api/projects end to end through
// the Service handler: the project's org is taken from the authenticated
// tenancy (not the body), and the caller must be a member of that org.
func TestHandleCreateProject_HTTP(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	if _, err := svc.CreateOrganisation(ctx, "acme", "Acme"); err != nil {
		t.Fatalf("CreateOrganisation: %v", err)
	}
	if _, err := svc.CreateUser(ctx, "agent-1", "ai"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := svc.AddOrgMember(ctx, "acme", "agent-1", "member"); err != nil {
		t.Fatalf("AddOrgMember: %v", err)
	}

	// POST with auth claims for org "acme"; body carries NO org.
	body := bytes.NewBufferString(`{"key":"ACME","name":"Acme project"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", body)
	req = req.WithContext(ContextWithAuth(req.Context(), &AuthClaims{Sub: "agent-1", Org: "acme"}))
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/projects = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	// The project exists and is scoped to the authed org.
	got, err := svc.GetProject(ContextWithAuth(ctx, &AuthClaims{Sub: "agent-1", Org: "acme"}), "ACME")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Organisation != "acme" {
		t.Errorf("project org = %q, want %q (from auth, not body)", got.Organisation, "acme")
	}

	// A caller who is not a member of the org is refused.
	body2 := bytes.NewBufferString(`{"key":"NOPE","name":"Nope"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/projects", body2)
	req2 = req2.WithContext(ContextWithAuth(req2.Context(), &AuthClaims{Sub: "stranger", Org: "acme"}))
	rec2 := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("non-member create = %d, want 400: %s", rec2.Code, rec2.Body.String())
	}
}

func TestCreateProject_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(context.Background(), Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Close()

	ctx := context.Background()
	if err := svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus engineering"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := svc.GetProject(ctx, "NEX")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Key != "NEX" || got.Name != "Nexus engineering" {
		t.Errorf("got %+v", got)
	}

	// Sequence row was auto-created.
	var nextSeq int
	if err := svc.db.QueryRowContext(ctx, `SELECT next_seq FROM project_sequences WHERE project = ?`, "NEX").Scan(&nextSeq); err != nil {
		t.Fatalf("sequence row missing: %v", err)
	}
	if nextSeq != 1 {
		t.Errorf("next_seq = %d, want 1", nextSeq)
	}
}

// TestCreateProject_RejectsUnknownOrg verifies the app-layer FK
// guard (#14): organisation must exist in the organisations table.
// Schema can't enforce this because SQLite's ALTER TABLE ADD COLUMN
// doesn't accept FK constraints — see schema.sql comment.
func TestCreateProject_RejectsUnknownOrg(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	err := svc.CreateProject(ctx, Project{
		Key:          "FOO",
		Name:         "Foo",
		Organisation: "no-such-org",
	})
	if !errors.Is(err, ErrOrgNotFound) {
		t.Errorf("got %v, want ErrOrgNotFound", err)
	}

	// And nothing was inserted (rollback check).
	var n int
	_ = svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE key = ?`, "FOO").Scan(&n)
	if n != 0 {
		t.Errorf("project row leaked through despite org-validation failure: count=%d", n)
	}
	_ = svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_sequences WHERE project = ?`, "FOO").Scan(&n)
	if n != 0 {
		t.Errorf("project_sequences row leaked: count=%d", n)
	}
}

// TestCreateProject_DefaultOrgWorksWhenUnspecified verifies the
// silent-default behaviour: empty Organisation → "nexus". The
// "nexus" org is backfilled by the schema so this MUST succeed,
// even though it relies on the org-existence guard.
func TestCreateProject_DefaultOrgWorksWhenUnspecified(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	if err := svc.CreateProject(ctx, Project{Key: "FOO", Name: "Foo"}); err != nil {
		t.Errorf("default-org create: %v", err)
	}
}

// TestCreateProject_RejectsNonMemberCaller verifies #13: when an
// auth context is present, the caller must be a member of the
// target org. Outsiders refused with ErrCallerNotInOrg.
func TestCreateProject_RejectsNonMemberCaller(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	// Seed an acme org with no members (the seeding aspects from
	// schema.sql are only in "nexus").
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO organisations(slug, name) VALUES ('acme', 'Acme')`); err != nil {
		t.Fatal(err)
	}

	// nexus-org caller tries to create a project in acme.
	authedCtx := withClaims(ctx, "shadow", "nexus", "admin")
	err := svc.CreateProject(authedCtx, Project{
		Key:          "ACM",
		Name:         "Acme",
		Organisation: "acme",
	})
	if !errors.Is(err, ErrCallerNotInOrg) {
		t.Errorf("got %v, want ErrCallerNotInOrg", err)
	}
}

// TestCreateProject_AllowsMemberCaller verifies the happy path: a
// member of the target org CAN create a project there.
func TestCreateProject_AllowsMemberCaller(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	authedCtx := withClaims(ctx, "shadow", "nexus", "admin")
	if err := svc.CreateProject(authedCtx, Project{
		Key:          "FOO",
		Name:         "Foo",
		Organisation: "nexus",
	}); err != nil {
		t.Errorf("member create: %v", err)
	}
}

// TestCreateProject_NoAuthContextSkipsMembershipCheck verifies the
// hybrid promise — when no auth context is present, the in-process
// trusted caller can create projects in any (existing) org without
// satisfying the membership check. Crucial for tests + the broker's
// direct in-process service calls.
func TestCreateProject_NoAuthContextSkipsMembershipCheck(t *testing.T) {
	ctx := context.Background() // NO claims attached
	svc := newTestService(t)
	defer svc.Close()

	// Seed acme org with NO members.
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO organisations(slug, name) VALUES ('acme', 'Acme')`); err != nil {
		t.Fatal(err)
	}

	if err := svc.CreateProject(ctx, Project{Key: "ACM", Name: "Acme", Organisation: "acme"}); err != nil {
		t.Errorf("no-auth create in acme: %v (membership check should have been skipped)", err)
	}
}
func TestListProjects_FiltersByOrgWithAuth(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO organisations(slug, name) VALUES ('acme', 'Acme')`); err != nil {
		t.Fatal(err)
	}
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus", Organisation: "nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "ACM", Name: "Acme", Organisation: "acme"})

	// No-auth caller sees both.
	all, _ := svc.ListProjects(ctx, false)
	if len(all) != 2 {
		t.Errorf("no-auth list: got %d projects, want 2", len(all))
	}

	// Nexus caller sees only NEX.
	nexusCtx := withClaims(ctx, "shadow", "nexus", "member")
	nexusList, _ := svc.ListProjects(nexusCtx, false)
	if len(nexusList) != 1 || nexusList[0].Key != "NEX" {
		t.Errorf("nexus filter: got %+v", nexusList)
	}
}

func TestListProjects_ArchivedExcludedByDefault(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OLD", Name: "Old"})
	if err := svc.ArchiveProject(ctx, "OLD"); err != nil {
		t.Fatal(err)
	}

	// Default: archived excluded.
	active, _ := svc.ListProjects(ctx, false)
	if len(active) != 1 || active[0].Key != "NEX" {
		t.Errorf("active-only: got %+v", active)
	}

	// includeArchived=true: both.
	all, _ := svc.ListProjects(ctx, true)
	if len(all) != 2 {
		t.Errorf("include-archived: got %d projects, want 2", len(all))
	}
}

func TestUpdateProject_PatchesNameDescription(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus", Description: "initial"})

	newName := "Nexus engineering"
	newDesc := "updated"
	if err := svc.UpdateProject(ctx, "NEX", UpdateProjectPatch{
		Name:        &newName,
		Description: &newDesc,
	}); err != nil {
		t.Fatal(err)
	}

	got, _ := svc.GetProject(ctx, "NEX")
	if got.Name != newName || got.Description != newDesc {
		t.Errorf("after patch: %+v", got)
	}
}

func TestUpdateProject_EmptyPatchIsNoOp(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	if err := svc.UpdateProject(ctx, "NEX", UpdateProjectPatch{}); err != nil {
		t.Errorf("empty patch should be no-op, got %v", err)
	}
}

func TestUpdateProject_HidesCrossOrg(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO organisations(slug, name) VALUES ('acme', 'Acme')`); err != nil {
		t.Fatal(err)
	}
	_ = svc.CreateProject(ctx, Project{Key: "ACM", Name: "Acme", Organisation: "acme"})

	nexusCtx := withClaims(ctx, "shadow", "nexus", "admin")
	newName := "hijacked"
	err := svc.UpdateProject(nexusCtx, "ACM", UpdateProjectPatch{Name: &newName})
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("got %v, want ErrProjectNotFound (hide-existence)", err)
	}
}

func TestArchiveProject_RoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	if err := svc.ArchiveProject(ctx, "NEX"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.GetProject(ctx, "NEX")
	if !got.Archived {
		t.Error("project should be archived")
	}

	if err := svc.UnarchiveProject(ctx, "NEX"); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetProject(ctx, "NEX")
	if got.Archived {
		t.Error("project should be unarchived")
	}
}

func TestArchiveProject_HidesCrossOrg(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO organisations(slug, name) VALUES ('acme', 'Acme')`); err != nil {
		t.Fatal(err)
	}
	_ = svc.CreateProject(ctx, Project{Key: "ACM", Name: "Acme", Organisation: "acme"})

	nexusCtx := withClaims(ctx, "shadow", "nexus", "admin")
	err := svc.ArchiveProject(nexusCtx, "ACM")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("got %v, want ErrProjectNotFound", err)
	}
}
