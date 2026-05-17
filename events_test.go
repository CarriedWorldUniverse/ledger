package ledger

import (
	"context"
	"testing"
)

func TestTransition_WritesEvent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil"); err != nil {
		t.Fatal(err)
	}

	events, err := svc.Timeline(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	found := false
	for _, e := range events {
		if e.Kind == "transition" && e.Actor == "anvil" {
			found = true
		}
	}
	if !found {
		t.Errorf("transition event missing: %+v", events)
	}
}

func TestCreate_WritesEvent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "create event test",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := svc.Timeline(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "create" && e.Actor == "shadow" {
			found = true
		}
	}
	if !found {
		t.Errorf("create event missing: %+v", events)
	}
}

func TestListMyUpdates_ReturnsEventsForAssignedAspect(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		AssigneeAspect: "anvil",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "first")
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "second")

	upd, err := svc.ListMyUpdates(ctx, "anvil", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(upd) < 2 {
		t.Errorf("expected ≥2 events for assigned aspect; got %d", len(upd))
	}
}

func TestListMyUpdates_RespectsSinceTimestamp(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		AssigneeAspect: "anvil",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "first")

	// With since="" should include all events.
	all, err := svc.ListMyUpdates(ctx, "anvil", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("expected events with empty since")
	}

	// With since=far-future should return none.
	none, err := svc.ListMyUpdates(ctx, "anvil", "2999-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 events with future since; got %d", len(none))
	}
}

func TestListMyUpdates_ReturnsEventsForWatchedIssue(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		AssigneeAspect: "anvil",
	})
	if err != nil {
		t.Fatal(err)
	}
	// plumb is NOT assigned but watches.
	_ = svc.WatchIssue(ctx, issue.Key, "plumb", "plumb")
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "hello")

	upd, err := svc.ListMyUpdates(ctx, "plumb", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(upd) == 0 {
		t.Error("expected events for watching aspect")
	}
	// All returned events should be on the watched issue.
	for _, e := range upd {
		if e.IssueKey != issue.Key {
			t.Errorf("unexpected issue %s in updates", e.IssueKey)
		}
	}
}

func TestListMyUpdates_EmptyForIrrelevantAspect(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		AssigneeAspect: "anvil",
	})
	if err != nil {
		t.Fatal(err)
	}

	// forge has no assignment and no watches.
	upd, err := svc.ListMyUpdates(ctx, "forge", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(upd) != 0 {
		t.Errorf("expected 0 events for irrelevant aspect; got %d", len(upd))
	}
}

func TestAssign_WritesEvent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "assign event test",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.AssignIssue(ctx, issue.Key, "anvil", "", "shadow"); err != nil {
		t.Fatal(err)
	}

	events, err := svc.Timeline(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "field_change" && e.Actor == "shadow" {
			found = true
		}
	}
	if !found {
		t.Errorf("assign event missing: %+v", events)
	}
}
