package ledger

import (
	"context"
	"testing"
)

// findEvent returns the first event of `kind` on the timeline, or nil.
func findEvent(events []Event, kind string) *Event {
	for i := range events {
		if events[i].Kind == kind {
			return &events[i]
		}
	}
	return nil
}

func TestReassignProject_AllocatesNewKeyAndAliases(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	oldKey := issue.Key
	newKey, err := svc.ReassignProject(ctx, oldKey, "OSS", "shadow", "rehome")
	if err != nil {
		t.Fatalf("ReassignProject: %v", err)
	}
	if newKey != "OSS-1" {
		t.Errorf("newKey = %q, want OSS-1", newKey)
	}

	// Direct lookup by new key works.
	got, err := svc.GetIssue(ctx, newKey)
	if err != nil {
		t.Fatalf("GetIssue(newKey): %v", err)
	}
	if got.Project != "OSS" {
		t.Errorf("Project after move = %q", got.Project)
	}

	// Lookup by old key resolves via alias.
	gotAlias, err := svc.GetIssue(ctx, oldKey)
	if err != nil {
		t.Fatalf("GetIssue(oldKey): %v", err)
	}
	if gotAlias.Key != newKey {
		t.Errorf("alias lookup returned %q, want %q", gotAlias.Key, newKey)
	}
}

// TestReassignProject_RecordsMoveEventWithActorAndReason verifies the
// previously-broken contract: the docstring claimed actor + reason
// were recorded; the original code stored neither. They now land on a
// move event on the issue's timeline.
func TestReassignProject_RecordsMoveEventWithActorAndReason(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})
	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "X",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	newKey, err := svc.ReassignProject(ctx, issue.Key, "OSS", "keel", "consolidating onto OSS")
	if err != nil {
		t.Fatalf("ReassignProject: %v", err)
	}

	timeline, err := svc.Timeline(ctx, newKey)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	mv := findEvent(timeline, "move")
	if mv == nil {
		t.Fatal("no move event recorded")
	}
	if mv.Actor != "keel" {
		t.Errorf("move event actor = %q, want %q", mv.Actor, "keel")
	}
	if got, _ := mv.Payload["reason"].(string); got != "consolidating onto OSS" {
		t.Errorf("move event reason = %q, want %q", got, "consolidating onto OSS")
	}
	if got, _ := mv.Payload["old_project"].(string); got != "NEX" {
		t.Errorf("move event old_project = %q, want NEX", got)
	}
	if got, _ := mv.Payload["new_project"].(string); got != "OSS" {
		t.Errorf("move event new_project = %q, want OSS", got)
	}
}

// TestReassignProject_RecordsParentDropEvent verifies the previously-
// undocumented behaviour: when a moved issue had a parent, the
// parent_key is silently dropped. Code now writes a field_change
// event for the unhitch so the timeline doesn't lose the relationship.
func TestReassignProject_RecordsParentDropEvent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})

	parent, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Epic", Summary: "Parent",
		DefinitionOfDone: "- [ ] root", Reporter: "shadow",
	})
	child, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "Child",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		ParentKey: parent.Key,
	})

	newKey, err := svc.ReassignProject(ctx, child.Key, "OSS", "keel", "rehome child")
	if err != nil {
		t.Fatalf("ReassignProject: %v", err)
	}

	timeline, _ := svc.Timeline(ctx, newKey)
	drop := findEvent(timeline, "field_change")
	if drop == nil {
		t.Fatal("no field_change event for parent drop")
	}
	if got, _ := drop.Payload["field"].(string); got != "parent_key" {
		t.Errorf("field_change field = %q, want parent_key", got)
	}
	if got, _ := drop.Payload["from"].(string); got != parent.Key {
		t.Errorf("field_change from = %q, want %q", got, parent.Key)
	}

	// Confirm the live issue actually has no parent.
	moved, _ := svc.GetIssue(ctx, newKey)
	if moved.ParentKey != "" {
		t.Errorf("moved issue ParentKey = %q, want empty", moved.ParentKey)
	}
}

// TestReassignProject_ChildCheckIsSourceProjectScoped verifies the
// fix to the previously project-wide child query: a child that's
// already been moved to a different project (NOT the source) should
// not block the parent's move.
func TestReassignProject_ChildCheckIsSourceProjectScoped(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})
	_ = svc.CreateProject(ctx, Project{Key: "WAKE", Name: "WakeStone"})

	parent, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Epic", Summary: "Parent",
		DefinitionOfDone: "- [ ] root", Reporter: "shadow",
	})
	child, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "Child",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		ParentKey: parent.Key,
	})

	// Move the child away first — child's parent_key still points at
	// the NEX parent's old key, but the child row itself now lives in
	// WAKE. The parent's source-scoped child-check should ignore it.
	if _, err := svc.ReassignProject(ctx, child.Key, "WAKE", "keel", "split"); err != nil {
		t.Fatalf("move child first: %v", err)
	}

	// Parent should now be free to move — no children remain IN NEX.
	if _, err := svc.ReassignProject(ctx, parent.Key, "OSS", "keel", "rehome parent"); err != nil {
		t.Fatalf("ReassignProject parent: unexpected error %v (child-in-source-only check failed)", err)
	}
}

// TestReassignProject_ChildInSourceProjectBlocks verifies the inverse:
// a still-in-source-project child correctly blocks the parent's move.
func TestReassignProject_ChildInSourceProjectBlocks(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})

	parent, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Epic", Summary: "Parent",
		DefinitionOfDone: "- [ ] root", Reporter: "shadow",
	})
	_, _ = svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "Child stays",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
		ParentKey: parent.Key,
	})

	if _, err := svc.ReassignProject(ctx, parent.Key, "OSS", "keel", "try-move-parent"); err == nil {
		t.Fatal("expected child-in-source-project to block parent move; got nil error")
	}
}
