package ledger

import (
	"context"
	"testing"
)

func TestAssign_ToAspect(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	if err := svc.AssignIssue(ctx, issue.Key, "anvil", "", "shadow"); err != nil {
		t.Fatalf("AssignIssue: %v", err)
	}
	got, _ := svc.GetIssue(ctx, issue.Key)
	if got.AssigneeAspect != "anvil" {
		t.Errorf("AssigneeAspect = %q", got.AssigneeAspect)
	}
	if got.AssigneeTeam != "" {
		t.Errorf("AssigneeTeam should be empty, got %q", got.AssigneeTeam)
	}
}

func TestAssign_RejectsBoth(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	err := svc.AssignIssue(ctx, issue.Key, "anvil", "oss-nexus-dev", "shadow")
	if err == nil {
		t.Fatal("expected error when both aspect and team set")
	}
}

func TestUpdateIssue_ChangesSummary(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "Original",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	newSummary := "Updated summary"
	if err := svc.UpdateIssue(ctx, issue.Key, UpdatePatch{Summary: &newSummary}, "shadow"); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	got, _ := svc.GetIssue(ctx, issue.Key)
	if got.Summary != newSummary {
		t.Errorf("Summary = %q, want %q", got.Summary, newSummary)
	}
}
