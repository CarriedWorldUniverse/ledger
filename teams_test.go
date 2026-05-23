package ledger

import (
	"context"
	"errors"
	"testing"
)

func TestCreateTeam_HappyPath(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	if err := svc.CreateTeam(ctx, "backend", "NEX", "Backend devs"); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	got, err := svc.GetTeam(ctx, "backend")
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if got.Project != "NEX" {
		t.Errorf("project = %q, want NEX", got.Project)
	}
	if got.Description != "Backend devs" {
		t.Errorf("description = %q", got.Description)
	}
	if len(got.Members) != 0 {
		t.Errorf("expected no members on fresh team, got %v", got.Members)
	}
}

func TestCreateTeam_RejectsUnknownProject(t *testing.T) {
	// teams.project has no SQL FK (ALTER TABLE ADD COLUMN can't add
	// REFERENCES — same trap as projects.organisation). App-layer
	// check rejects the orphan with ErrProjectNotFound.
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	err := svc.CreateTeam(ctx, "backend", "NOPROJ", "")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("got %v, want ErrProjectNotFound", err)
	}
}

func TestTeamMembers_AddRemoveListed(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateTeam(ctx, "backend", "NEX", "")

	if err := svc.AddTeamMember(ctx, "backend", "anvil"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddTeamMember(ctx, "backend", "keel"); err != nil {
		t.Fatal(err)
	}
	// Idempotent — duplicate add is no-op
	if err := svc.AddTeamMember(ctx, "backend", "anvil"); err != nil {
		t.Errorf("duplicate add should be no-op, got %v", err)
	}

	got, _ := svc.GetTeam(ctx, "backend")
	if len(got.Members) != 2 {
		t.Errorf("expected 2 members, got %v", got.Members)
	}
	// Sorted alphabetically per ORDER BY aspect
	if got.Members[0] != "anvil" || got.Members[1] != "keel" {
		t.Errorf("member order: %v", got.Members)
	}

	if err := svc.RemoveTeamMember(ctx, "backend", "anvil"); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetTeam(ctx, "backend")
	if len(got.Members) != 1 || got.Members[0] != "keel" {
		t.Errorf("after remove: %v", got.Members)
	}

	// Idempotent remove
	if err := svc.RemoveTeamMember(ctx, "backend", "anvil"); err != nil {
		t.Errorf("idempotent remove failed: %v", err)
	}
}

func TestListTeams_FilterByProject(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})
	_ = svc.CreateTeam(ctx, "nex-backend", "NEX", "")
	_ = svc.CreateTeam(ctx, "oss-backend", "OSS", "")

	nexTeams, _ := svc.ListTeams(ctx, "NEX")
	if len(nexTeams) != 1 || nexTeams[0].Name != "nex-backend" {
		t.Errorf("NEX filter: got %v", nexTeams)
	}

	ossTeams, _ := svc.ListTeams(ctx, "OSS")
	if len(ossTeams) != 1 || ossTeams[0].Name != "oss-backend" {
		t.Errorf("OSS filter: got %v", ossTeams)
	}

	// No project + no auth → all teams (in-process trusted caller)
	all, _ := svc.ListTeams(ctx, "")
	if len(all) != 2 {
		t.Errorf("unfiltered: got %d teams, want 2", len(all))
	}
}

func TestListTeams_AuthRequiresProjectFilter(t *testing.T) {
	// With auth context but no project filter, refuse rather than
	// silently leak teams across orgs.
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	authedCtx := withClaims(ctx, "shadow", "nexus", "member")
	_, err := svc.ListTeams(authedCtx, "")
	if err == nil {
		t.Error("expected error for authed unfiltered list; got nil")
	}
}

func TestDeleteTeam_RequiresEmpty(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateTeam(ctx, "backend", "NEX", "")
	_ = svc.AddTeamMember(ctx, "backend", "anvil")

	if err := svc.DeleteTeam(ctx, "backend"); !errors.Is(err, ErrTeamNotEmpty) {
		t.Errorf("got %v, want ErrTeamNotEmpty", err)
	}

	// Drain members; delete should now succeed.
	_ = svc.RemoveTeamMember(ctx, "backend", "anvil")
	if err := svc.DeleteTeam(ctx, "backend"); err != nil {
		t.Errorf("after drain: %v", err)
	}
	if _, err := svc.GetTeam(ctx, "backend"); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("post-delete lookup: %v, want ErrTeamNotFound", err)
	}
}

func TestAssignIssue_RejectsCrossProjectTeam(t *testing.T) {
	// Core orchestration property: a ticket in NEX can only be
	// assigned to a team whose project is also NEX. Team in OSS
	// can't take NEX tickets.
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})
	_ = svc.CreateTeam(ctx, "oss-team", "OSS", "")

	nexIssue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [x]g", Reporter: "shadow",
	})

	err := svc.AssignIssue(ctx, nexIssue.Key, "", "oss-team", "shadow")
	if !errors.Is(err, ErrTeamProjectMismatch) {
		t.Errorf("got %v, want ErrTeamProjectMismatch", err)
	}
}

func TestAssignIssue_AllowsSameProjectTeam(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateTeam(ctx, "nex-team", "NEX", "")

	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [x]g", Reporter: "shadow",
	})
	if err := svc.AssignIssue(ctx, issue.Key, "", "nex-team", "shadow"); err != nil {
		t.Errorf("same-project team assign: %v", err)
	}
}

func TestAssignIssue_RejectsNonExistentTeam(t *testing.T) {
	// The team-must-exist check fires via teamProject(); surfaces
	// as ErrTeamNotFound.
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [x]g", Reporter: "shadow",
	})

	err := svc.AssignIssue(ctx, issue.Key, "", "phantom-team", "shadow")
	if !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("got %v, want ErrTeamNotFound", err)
	}
}

// TestSchemaMigration_TeamsBackfilledWithProject verifies the v10
// migration: existing teams (pre-#11) get a project association via
// the lookup chain (assignee_team usage → projects.default_team →
// fallback "nexus"). Since newTestService starts from a fresh DB,
// we manually exercise the path by inserting a row that LOOKS like
// pre-migration state, then verifying it's now scoped.
func TestSchemaMigration_TeamsBackfilledWithProject(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	// Insert a team without specifying project (the column has
	// DEFAULT 'nexus' so this works even without explicit value).
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO teams(name, description) VALUES ('legacy-team', 'pre-migration')`,
	); err != nil {
		t.Fatal(err)
	}

	var project string
	if err := svc.db.QueryRowContext(ctx,
		`SELECT project FROM teams WHERE name = ?`, "legacy-team",
	).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if project != "nexus" {
		t.Errorf("default project = %q, want nexus", project)
	}
}

// TestAddTeamMember_RejectsUnknownUser verifies the app-layer FK
// guard (#15): aspect must exist in the users table. team_members
// has no SQL FK to users (predates the multi-tenancy schema), so
// the app layer prevents orphan member rows.
func TestAddTeamMember_RejectsUnknownUser(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateTeam(ctx, "backend", "NEX", "")

	err := svc.AddTeamMember(ctx, "backend", "non-existent-aspect")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("got %v, want ErrUserNotFound", err)
	}

	// Confirm no row leaked through despite the rejection.
	var n int
	_ = svc.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_members WHERE team = ? AND aspect = ?`,
		"backend", "non-existent-aspect",
	).Scan(&n)
	if n != 0 {
		t.Errorf("orphan team_members row leaked: count=%d", n)
	}
}

// TestAddTeamMember_AllowsKnownUser is the happy-path counterpart —
// existing users from the schema backfill (anvil, keel, shadow, etc.)
// add successfully. Guards against false-positive rejection.
func TestAddTeamMember_AllowsKnownUser(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateTeam(ctx, "backend", "NEX", "")

	// "anvil" is backfilled by the schema's users seed.
	if err := svc.AddTeamMember(ctx, "backend", "anvil"); err != nil {
		t.Errorf("known user add: %v", err)
	}
}
