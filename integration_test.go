//go:build integration

package ledger

import (
	"context"
	"testing"
)

// TestE2E_FullLifecycle exercises every Phase 2 layer in a single scenario:
// create → assign → watch → comment+mention → transition path →
// ListMyUpdates → markdown materialisation → notification assertions.
func TestE2E_FullLifecycle(t *testing.T) {
	ctx := context.Background()
	n := &captureNotifier{}
	svc := newTestServiceWithNotifier(t, n)
	defer svc.Close()

	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project:          "NEX",
		Type:             "Story",
		Summary:          "e2e",
		Description:      "desc",
		DefinitionOfDone: "- [x] step 1",
		Reporter:         "shadow",
		AssigneeAspect:   "anvil",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Assign
	_ = svc.AssignIssue(ctx, issue.Key, "anvil", "", "shadow")

	// Watch
	_ = svc.WatchIssue(ctx, issue.Key, "plumb", "plumb")

	// Comment with mention
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "@plumb starting work")

	// Transition path
	_ = svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil")
	_ = svc.TransitionIssue(ctx, issue.Key, "Blocked", "anvil")
	_ = svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil")
	_ = svc.TransitionIssue(ctx, issue.Key, "In Review", "anvil")
	_ = svc.TransitionIssue(ctx, issue.Key, "Done", "anvil")

	// ListMyUpdates: assigned aspect sees events
	anvilUpdates, err := svc.ListMyUpdates(ctx, "anvil", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(anvilUpdates) == 0 {
		t.Error("anvil should have updates (assigned)")
	}

	// ListMyUpdates: watcher sees events
	plumbUpdates, err := svc.ListMyUpdates(ctx, "plumb", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plumbUpdates) == 0 {
		t.Error("plumb should have updates (watcher)")
	}

	// ListMyUpdates: irrelevant aspect sees nothing
	forgeUpdates, err := svc.ListMyUpdates(ctx, "forge", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(forgeUpdates) != 0 {
		t.Errorf("forge should have no updates; got %d", len(forgeUpdates))
	}

	// ListMyUpdates: since filter respects timestamp
	since := anvilUpdates[len(anvilUpdates)-1].At
	afterLast, err := svc.ListMyUpdates(ctx, "anvil", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterLast) != 0 {
		t.Errorf("expected 0 events after last timestamp; got %d", len(afterLast))
	}

	// Markdown materialisation
	md, err := svc.MaterialiseMarkdown(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(md) < 200 {
		t.Errorf("markdown too short: %d chars", len(md))
	}

	// Notifications: anvil received assignment notice
	if len(n.aspectMsg["anvil"]) == 0 {
		t.Error("anvil should have received assignment notice")
	}

	// Notifications: plumb received mention + blocker transition notices
	if len(n.aspectMsg["plumb"]) == 0 {
		t.Error("plumb should have received mention + blocker notices")
	}

	// Operator stream populated
	if len(n.opStream) < 5 {
		t.Errorf("operator stream too sparse: %d entries", len(n.opStream))
	}
}
