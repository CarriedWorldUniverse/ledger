package ledger

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

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
