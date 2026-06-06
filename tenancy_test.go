package ledger

import (
	"context"
	"errors"
	"testing"
)

// withClaims returns a ctx carrying the given AuthClaims, mirroring
// what authMiddleware injects from a verified JWT. Lets tests
// exercise the tenancy-enforcement paths without standing up the
// full HTTP+JWT stack.
func withClaims(parent context.Context, sub, org, role string) context.Context {
	return context.WithValue(parent, authClaimsKey, &AuthClaims{
		Sub: sub, Org: org, Role: role,
	})
}

// seedMultiOrgFixture creates two orgs (nexus + acme) with one
// project each and one issue in each, returning the project keys
// and issue keys. Used as the base for cross-org isolation tests.
func seedMultiOrgFixture(t *testing.T) (svc *Service, nexusKey, acmeKey, nexusIssue, acmeIssue string) {
	t.Helper()
	ctx := context.Background()
	svc = newTestService(t)
	t.Cleanup(func() { svc.Close() })

	// Default org "nexus" is backfilled by schema; acme is new.
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO organisations(slug, name) VALUES ('acme', 'Acme')`); err != nil {
		t.Fatalf("seed acme org: %v", err)
	}
	// Add an aspect "anvil" to nexus (already there from schema seed)
	// and a separate aspect "alpha" to acme only — exclusive members.
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO users(id, kind) VALUES ('alpha', 'ai')`); err != nil {
		t.Fatalf("seed alpha user: %v", err)
	}
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO org_members(org, user_id, role) VALUES ('acme', 'alpha', 'member')`); err != nil {
		t.Fatalf("seed acme member: %v", err)
	}

	if err := svc.CreateProject(ctx, Project{Key: "NEX", Organisation: "nexus", Name: "Nexus"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateProject(ctx, Project{Key: "ACM", Organisation: "acme", Name: "Acme"}); err != nil {
		t.Fatal(err)
	}

	nIssue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "nexus work",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	aIssue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "ACM", Type: "Story", Summary: "acme work",
		DefinitionOfDone: "- [ ] go", Reporter: "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, "NEX", "ACM", nIssue.Key, aIssue.Key
}

func TestGetProject_HidesCrossOrgFromAuthedCaller(t *testing.T) {
	svc, nexusKey, acmeKey, _, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	// In-org access works.
	if _, err := svc.GetProject(nexusCtx, nexusKey); err != nil {
		t.Errorf("in-org GetProject: %v", err)
	}

	// Cross-org access returns ErrProjectNotFound (not a permissions
	// error). Hides existence.
	_, err := svc.GetProject(nexusCtx, acmeKey)
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("cross-org GetProject: got %v, want ErrProjectNotFound", err)
	}
}

func TestGetProject_NoAuthContextSkipsCheck(t *testing.T) {
	// Hybrid promise: in-process trusted callers (no auth context)
	// can read any project. Verifies backward-compat.
	svc, nexusKey, acmeKey, _, _ := seedMultiOrgFixture(t)
	ctx := context.Background()

	if _, err := svc.GetProject(ctx, nexusKey); err != nil {
		t.Errorf("nexus from no-auth ctx: %v", err)
	}
	if _, err := svc.GetProject(ctx, acmeKey); err != nil {
		t.Errorf("acme from no-auth ctx: %v", err)
	}
}

func TestGetIssue_HidesCrossOrgFromAuthedCaller(t *testing.T) {
	svc, _, _, _, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	_, err := svc.GetIssue(nexusCtx, acmeIssue)
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org GetIssue: got %v, want ErrIssueNotFound", err)
	}
}

func TestGetIssue_AliasResolutionRespectsTenancy(t *testing.T) {
	// Alias path also goes through tenancy check — moved issue's
	// old key from acme shouldn't leak to nexus caller.
	svc, _, acmeKey, _, acmeIssue := seedMultiOrgFixture(t)
	ctx := context.Background()
	// Need a destination project in acme to move within-org.
	_ = svc.CreateProject(ctx, Project{Key: "ACM2", Organisation: "acme", Name: "Acme 2"})
	newKey, err := svc.ReassignProject(ctx, acmeIssue, "ACM2", "alpha", "test")
	if err != nil {
		t.Fatal(err)
	}
	_ = acmeKey
	_ = newKey

	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	// Look up by ORIGINAL (now-aliased) acme key from a nexus caller.
	_, err = svc.GetIssue(nexusCtx, acmeIssue)
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org alias lookup: got %v, want ErrIssueNotFound", err)
	}
}

func TestAssignIssue_RejectsCrossOrgAssignee(t *testing.T) {
	// Caller in nexus org assigns nexus-issue to an aspect that's
	// only a member of acme. Should fail with ErrAssigneeNotInOrg.
	svc, _, _, nexusIssue, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	// alpha is only in acme org per fixture seed.
	err := svc.AssignIssue(nexusCtx, nexusIssue, "alpha", "", "shadow")
	if !errors.Is(err, ErrAssigneeNotInOrg) {
		t.Errorf("got %v, want ErrAssigneeNotInOrg", err)
	}
}

func TestAssignIssue_AllowsInOrgAssignee(t *testing.T) {
	svc, _, _, nexusIssue, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	// anvil is in the nexus org (backfilled by schema).
	if err := svc.AssignIssue(nexusCtx, nexusIssue, "anvil", "", "shadow"); err != nil {
		t.Errorf("in-org assign: %v", err)
	}
}

func TestAssignIssue_RejectsCrossOrgCallerOnIssue(t *testing.T) {
	// Nexus-org caller tries to assign an acme issue.
	svc, _, _, _, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	err := svc.AssignIssue(nexusCtx, acmeIssue, "anvil", "", "shadow")
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org AssignIssue: got %v, want ErrIssueNotFound (hide-existence)", err)
	}
}

func TestUpdateIssue_RejectsCrossProjectParent(t *testing.T) {
	// Pure correctness check — no auth needed. Setting parent_key
	// to an issue in a different project must fail; cross-project
	// hierarchy is forbidden (ReassignProject is the proper path).
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})
	nex, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Epic", Summary: "nexus parent",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	oss, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "OSS", Type: "Story", Summary: "oss child candidate",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	parentRef := nex.Key
	err := svc.UpdateIssue(ctx, oss.Key, UpdatePatch{ParentKey: &parentRef}, "shadow")
	if !errors.Is(err, ErrCrossProjectParent) {
		t.Errorf("got %v, want ErrCrossProjectParent", err)
	}
}

func TestUpdateIssue_AllowsSameProjectParent(t *testing.T) {
	// Same-project parent assignment continues to work — the cross-
	// project guard shouldn't false-positive on the happy path.
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	parent, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Epic", Summary: "p",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	child, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "c",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	pkey := parent.Key
	if err := svc.UpdateIssue(ctx, child.Key, UpdatePatch{ParentKey: &pkey}, "shadow"); err != nil {
		t.Errorf("same-project parent: %v", err)
	}
}

func TestUpdateIssue_RejectsParentNotFound(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	child, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "c",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	bogus := "NEX-999"
	err := svc.UpdateIssue(ctx, child.Key, UpdatePatch{ParentKey: &bogus}, "shadow")
	if err == nil {
		t.Fatal("expected error for non-existent parent")
	}
	// Loose check — error wraps a "not found"-style message; the
	// surface is "fail loudly" not a specific sentinel here.
}

func TestAuthFromContext_NilSafe(t *testing.T) {
	// AuthFromContext should return nil without panicking when the
	// context has no claims. All tenancy helpers depend on this
	// behaviour to deliver the hybrid promise.
	if c := AuthFromContext(context.Background()); c != nil {
		t.Errorf("AuthFromContext(empty ctx) = %+v, want nil", c)
	}
}

// ---------- cross-org gate on transition / comment / watch / timeline ----------

func TestTransitionIssue_RejectsCrossOrgCaller(t *testing.T) {
	// Nexus-org caller tries to transition an acme issue — must get
	// ErrIssueNotFound (hide-existence), not a permissions error.
	svc, _, _, _, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	err := svc.TransitionIssue(nexusCtx, acmeIssue, "In Progress", "shadow")
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org TransitionIssue: got %v, want ErrIssueNotFound", err)
	}
}

func TestTransitionIssue_AllowsSameOrgCaller(t *testing.T) {
	// Same-org caller can transition their own issue.
	svc, _, _, nexusIssue, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	// Issue starts in "To Do"; transition to "In Progress" (valid path).
	if err := svc.TransitionIssue(nexusCtx, nexusIssue, "In Progress", "shadow"); err != nil {
		t.Errorf("in-org TransitionIssue: %v", err)
	}
}

func TestCommentIssue_RejectsCrossOrgCaller(t *testing.T) {
	// Nexus-org caller tries to comment on an acme issue — must get
	// ErrIssueNotFound (hide-existence).
	svc, _, _, _, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	err := svc.CommentIssue(nexusCtx, acmeIssue, "shadow", "sneaky comment")
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org CommentIssue: got %v, want ErrIssueNotFound", err)
	}
}

func TestCommentIssue_AllowsSameOrgCaller(t *testing.T) {
	// Same-org caller can comment on their own issue.
	svc, _, _, nexusIssue, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	if err := svc.CommentIssue(nexusCtx, nexusIssue, "shadow", "great progress"); err != nil {
		t.Errorf("in-org CommentIssue: %v", err)
	}
}

func TestWatchIssue_RejectsCrossOrgCaller(t *testing.T) {
	// Nexus-org caller tries to watch an acme issue — must get
	// ErrIssueNotFound (hide-existence).
	svc, _, _, _, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	err := svc.WatchIssue(nexusCtx, acmeIssue, "shadow", "shadow")
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org WatchIssue: got %v, want ErrIssueNotFound", err)
	}
}

func TestWatchIssue_AllowsSameOrgCaller(t *testing.T) {
	// Same-org caller can watch their own issue.
	svc, _, _, nexusIssue, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	if err := svc.WatchIssue(nexusCtx, nexusIssue, "shadow", "shadow"); err != nil {
		t.Errorf("in-org WatchIssue: %v", err)
	}
}

func TestUnwatchIssue_RejectsCrossOrgCaller(t *testing.T) {
	// Nexus-org caller tries to unwatch an acme issue — must get
	// ErrIssueNotFound (hide-existence).
	svc, _, _, _, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	err := svc.UnwatchIssue(nexusCtx, acmeIssue, "shadow", "shadow")
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org UnwatchIssue: got %v, want ErrIssueNotFound", err)
	}
}

func TestUnwatchIssue_AllowsSameOrgCaller(t *testing.T) {
	// Same-org caller can unwatch their own issue.
	svc, _, _, nexusIssue, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	if err := svc.UnwatchIssue(nexusCtx, nexusIssue, "shadow", "shadow"); err != nil {
		t.Errorf("in-org UnwatchIssue: %v", err)
	}
}

func TestWatchers_RejectsCrossOrgCaller(t *testing.T) {
	// Nexus-org caller reading watchers of an acme issue must get
	// ErrIssueNotFound (hide-existence). Watchers is a read path so
	// it also leaks cross-org data if ungated.
	svc, _, _, _, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	_, err := svc.Watchers(nexusCtx, acmeIssue)
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org Watchers: got %v, want ErrIssueNotFound", err)
	}
}

func TestWatchers_AllowsSameOrgCaller(t *testing.T) {
	// Same-org caller can read watchers of their own issue.
	svc, _, _, nexusIssue, _ := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	if _, err := svc.Watchers(nexusCtx, nexusIssue); err != nil {
		t.Errorf("in-org Watchers: %v", err)
	}
}
