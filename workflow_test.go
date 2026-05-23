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

// TestTransition_ReadyToStart_DispatchFlow verifies the orchestration-
// driven path: To Do → Ready to Start → In Progress. The middle
// status is the signal the scheduler subscribes to so it doesn't
// have to compute readiness on every event.
func TestTransition_ReadyToStart_DispatchFlow(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
	})

	if err := svc.TransitionIssue(ctx, issue.Key, "Ready to Start", "shadow"); err != nil {
		t.Fatalf("To Do → Ready to Start: %v", err)
	}
	if err := svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil"); err != nil {
		t.Fatalf("Ready to Start → In Progress: %v", err)
	}

	got, _ := svc.GetIssue(ctx, issue.Key)
	if got.Status != "In Progress" {
		t.Errorf("status = %q, want In Progress", got.Status)
	}
}

// TestTransition_ReadyToStart_FromBlocked verifies the orchestration
// re-dispatch path: a Blocked ticket whose dependency clears moves
// back to Ready to Start so the scheduler re-picks it up.
func TestTransition_ReadyToStart_FromBlocked(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
	})

	_ = svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil")
	_ = svc.TransitionIssue(ctx, issue.Key, "Blocked", "anvil")

	// Blocker clears; ticket goes back to Ready to Start (not directly
	// to In Progress) so the scheduler sees the ready signal.
	if err := svc.TransitionIssue(ctx, issue.Key, "Ready to Start", "anvil"); err != nil {
		t.Fatalf("Blocked → Ready to Start: %v", err)
	}
}

// TestTransition_ReadyToStart_BackToToDo verifies operator can pull a
// ticket back to To Do if it was prematurely marked Ready (e.g.
// assignee got reassigned and now needs re-evaluation).
func TestTransition_ReadyToStart_BackToToDo(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
	})

	_ = svc.TransitionIssue(ctx, issue.Key, "Ready to Start", "shadow")
	if err := svc.TransitionIssue(ctx, issue.Key, "To Do", "shadow"); err != nil {
		t.Fatalf("Ready to Start → To Do: %v", err)
	}
}

// TestTransition_LegacyDirectPathStillWorks verifies the
// non-orchestrated (operator-driven) path still works after the new
// state was added. To Do → In Progress remains a one-shot transition
// for callers that aren't using the scheduler.
func TestTransition_LegacyDirectPathStillWorks(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
	})

	// Operator manually dispatches without going through scheduler.
	if err := svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil"); err != nil {
		t.Fatalf("legacy To Do → In Progress should still work: %v", err)
	}
}

// TestTransition_ReadyToStart_NotForEpic verifies Epics don't gain
// the new status — they use Brief / Sketch/Refined / In Development
// / Delivered / Cancelled. Epics are containers; the work that gets
// dispatched is their stories/tasks/subtasks.
func TestTransition_ReadyToStart_NotForEpic(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	epic, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Epic", Summary: "E",
		DefinitionOfDone: "- [x] done", Reporter: "shadow",
	})

	err := svc.TransitionIssue(ctx, epic.Key, "Ready to Start", "shadow")
	if err == nil {
		t.Errorf("Epic shouldn't accept Ready to Start; transition succeeded")
	}
}
