package ledger

import (
	"context"
	"testing"
)

func TestSchemaV7_OrganisationsAndUsersExist(t *testing.T) {
	svc := newTestService(t)
	defer svc.Close()

	// Schema version 7 should be applied.
	var v int
	if err := svc.db.QueryRow(
		`SELECT version FROM schema_versions ORDER BY version DESC LIMIT 1`,
	).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v < 7 {
		t.Fatalf("expected schema version >= 7, got %d", v)
	}

	// Default nexus org exists.
	var orgName string
	if err := svc.db.QueryRow(
		`SELECT name FROM organisations WHERE slug = 'nexus'`,
	).Scan(&orgName); err != nil {
		t.Fatalf("default org missing: %v", err)
	}
	if orgName != "Nexus" {
		t.Errorf("default org name = %q, want %q", orgName, "Nexus")
	}

	// Users backfilled.
	var count int
	if err := svc.db.QueryRow(
		`SELECT COUNT(*) FROM users`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 10 {
		t.Errorf("expected ≥10 user rows backfilled; got %d", count)
	}

	// Operator is human.
	var kind string
	if err := svc.db.QueryRow(
		`SELECT kind FROM users WHERE id = 'jacinta'`,
	).Scan(&kind); err != nil {
		t.Fatalf("jacinta user missing: %v", err)
	}
	if kind != "human" {
		t.Errorf("jacinta kind = %q, want human", kind)
	}

	// AI aspects are ai.
	if err := svc.db.QueryRow(
		`SELECT kind FROM users WHERE id = 'plumb'`,
	).Scan(&kind); err != nil {
		t.Fatalf("plumb user missing: %v", err)
	}
	if kind != "ai" {
		t.Errorf("plumb kind = %q, want ai", kind)
	}

	// Org members backfilled.
	if err := svc.db.QueryRow(
		`SELECT role FROM org_members WHERE org = 'nexus' AND user_id = 'jacinta'`,
	).Scan(&kind); err != nil {
		t.Fatalf("jacinta org membership missing: %v", err)
	}
	if kind != "owner" {
		t.Errorf("jacinta role = %q, want owner", kind)
	}
}

func TestCreateOrganisation_Roundtrip(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	org, err := svc.CreateOrganisation(ctx, "acme", "Acme Corp")
	if err != nil {
		t.Fatal(err)
	}
	if org.Slug != "acme" || org.Name != "Acme Corp" {
		t.Fatalf("got %+v", org)
	}

	got, err := svc.GetOrganisation(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Acme Corp" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestCreateUser_HumanAndAI(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	h, err := svc.CreateUser(ctx, "alice", "human")
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "alice" || h.Kind != "human" {
		t.Fatalf("got %+v", h)
	}

	a, err := svc.CreateUser(ctx, "bot42", "ai")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "bot42" || a.Kind != "ai" {
		t.Fatalf("got %+v", a)
	}
}

func TestCreateUser_RejectsInvalidKind(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	_, err := svc.CreateUser(ctx, "x", "robot")
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestOrgMember_Roundtrip(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	_, _ = svc.CreateOrganisation(ctx, "acme", "Acme Corp")
	_, _ = svc.CreateUser(ctx, "alice", "human")

	if err := svc.AddOrgMember(ctx, "acme", "alice", "admin"); err != nil {
		t.Fatal(err)
	}

	members, err := svc.ListOrgMembers(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member; got %d", len(members))
	}
	if members[0].UserID != "alice" || members[0].Role != "admin" {
		t.Errorf("got %+v", members[0])
	}
}

func TestProject_DefaultsToNexusOrg(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	if err := svc.CreateProject(ctx, Project{Key: "ACME", Name: "Acme"}); err != nil {
		t.Fatal(err)
	}

	p, err := svc.GetProject(ctx, "ACME")
	if err != nil {
		t.Fatal(err)
	}
	if p.Organisation != "nexus" {
		t.Errorf("default org = %q, want nexus", p.Organisation)
	}
}
