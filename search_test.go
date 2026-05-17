package ledger

import (
	"context"
	"testing"
)

func TestSearch_ByAssigneeAndStatus(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	mk := func(summary, assignee string) string {
		issue, _ := svc.CreateIssue(ctx, IssueDraft{
			Project: "NEX", Type: "Story", Summary: summary,
			DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: assignee,
		})
		return issue.Key
	}
	a := mk("for anvil", "anvil")
	_ = mk("for plumb", "plumb")
	_ = svc.TransitionIssue(ctx, a, "In Progress", "anvil")

	results, err := svc.Search(ctx, SearchFilter{
		AssigneeAspect: "anvil",
		Statuses:       []string{"In Progress"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Key != a {
		t.Errorf("Key = %q, want %q", results[0].Key, a)
	}
}

func TestListMy_ReturnsAspectAndTeamIssues(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "mine",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

	results, err := svc.ListMy(ctx, "anvil", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d", len(results))
	}
}

func TestListReady_ExcludesNonStartable(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "ready",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

	results, err := svc.ListReady(ctx, "anvil", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
}
