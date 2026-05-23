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

	upd, err := svc.ListMyUpdates(ctx, "anvil", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(upd) < 2 {
		t.Errorf("expected ≥2 events for assigned aspect; got %d", len(upd))
	}
}

func TestListMyUpdates_RespectsSinceID(t *testing.T) {
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

	// sinceID=0 returns everything.
	all, err := svc.ListMyUpdates(ctx, "anvil", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("expected events with sinceID=0")
	}

	// sinceID = highest id seen → no more events (until new ones land).
	last := all[len(all)-1].ID
	more, err := svc.ListMyUpdates(ctx, "anvil", last, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 0 {
		t.Errorf("expected 0 events past sinceID=%d; got %d", last, len(more))
	}

	// New event lands; next poll picks it up exactly.
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "second")
	next, err := svc.ListMyUpdates(ctx, "anvil", last, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 {
		t.Errorf("expected exactly 1 new event after second comment; got %d", len(next))
	}
}

// TestListMyUpdates_LimitHonoured exercises the limit param + the
// polling-protocol contract: when the response is full, the caller
// can re-poll with the new cursor and get the rest. This is the
// regression test for the original bug — LIMIT 200 with no cursor
// meant active aspects silently missed events past 200/poll.
func TestListMyUpdates_LimitHonoured(t *testing.T) {
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
	// Generate 15 events so we can test small limits.
	for i := 0; i < 15; i++ {
		_ = svc.CommentIssue(ctx, issue.Key, "anvil", "msg")
	}

	// First page: 5 events.
	page1, err := svc.ListMyUpdates(ctx, "anvil", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 5 {
		t.Fatalf("page1: want 5 events (limit), got %d", len(page1))
	}

	// Continue from the last id; another 5.
	page2, err := svc.ListMyUpdates(ctx, "anvil", page1[4].ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 5 {
		t.Fatalf("page2: want 5 events, got %d", len(page2))
	}

	// Pages must be strictly increasing in id (cursor protocol works).
	if page2[0].ID <= page1[4].ID {
		t.Errorf("page2 first id %d not > page1 last id %d", page2[0].ID, page1[4].ID)
	}

	// limit=0 falls back to DefaultUpdatesLimit; should fetch everything
	// remaining (>5 still pending, well under 200 default).
	rest, err := svc.ListMyUpdates(ctx, "anvil", page2[4].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) < 5 {
		t.Errorf("rest: expected at least 5 remaining events; got %d", len(rest))
	}
}

// TestListMyUpdates_LimitClamped verifies a misbehaving caller can't
// request the entire timeline in one shot — limit > MaxUpdatesLimit
// is clamped down. Defensive bound per the docstring.
func TestListMyUpdates_LimitClamped(t *testing.T) {
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
	// Generate more events than MaxUpdatesLimit by a small margin.
	for i := 0; i < MaxUpdatesLimit+10; i++ {
		_ = svc.CommentIssue(ctx, issue.Key, "anvil", "msg")
	}

	upd, err := svc.ListMyUpdates(ctx, "anvil", 0, MaxUpdatesLimit*10)
	if err != nil {
		t.Fatal(err)
	}
	if len(upd) > MaxUpdatesLimit {
		t.Errorf("limit clamping failed: got %d events, max is %d", len(upd), MaxUpdatesLimit)
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

	upd, err := svc.ListMyUpdates(ctx, "plumb", 0, 0)
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
	upd, err := svc.ListMyUpdates(ctx, "forge", 0, 0)
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
