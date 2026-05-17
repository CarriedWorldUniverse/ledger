package ledger

import (
	"context"
	"testing"
)

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
