package ledger

import (
	"context"
	"testing"
)

func TestTransition_Story_HappyPath(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	if err := svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"}); err != nil {
		t.Fatal(err)
	}
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project:          "NEX",
		Type:             "Story",
		Summary:          "X",
		DefinitionOfDone: "- [x] done",
		Reporter:         "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil"); err != nil {
		t.Fatalf("To Do→In Progress: %v", err)
	}
	if err := svc.TransitionIssue(ctx, issue.Key, "In Review", "anvil"); err != nil {
		t.Fatalf("In Progress→In Review: %v", err)
	}
	if err := svc.TransitionIssue(ctx, issue.Key, "Done", "anvil"); err != nil {
		t.Fatalf("In Review→Done: %v", err)
	}

	got, err := svc.GetIssue(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "Done" {
		t.Errorf("final status = %q, want Done", got.Status)
	}
}

func TestTransition_RejectsDoneWithUntickedDoD(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	if err := svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"}); err != nil {
		t.Fatal(err)
	}
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project:          "NEX",
		Type:             "Story",
		Summary:          "X",
		DefinitionOfDone: "- [ ] not done yet",
		Reporter:         "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil")
	_ = svc.TransitionIssue(ctx, issue.Key, "In Review", "anvil")

	err = svc.TransitionIssue(ctx, issue.Key, "Done", "anvil")
	if err == nil {
		t.Fatalf("expected error transitioning to Done with unticked DoD")
	}
}

func TestTransition_RejectsInvalid(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project:          "NEX",
		Type:             "Story",
		Summary:          "X",
		DefinitionOfDone: "- [x] done",
		Reporter:         "shadow",
	})

	// Story can't go directly To Do → Done.
	err := svc.TransitionIssue(ctx, issue.Key, "Done", "anvil")
	if err == nil {
		t.Fatalf("expected error on direct To Do → Done")
	}
}
