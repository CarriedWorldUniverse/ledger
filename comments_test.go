package ledger

import (
	"context"
	"testing"
)

func TestCommentIssue_AppendsEvent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.CommentIssue(ctx, issue.Key, "anvil", "hello"); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	tl, err := svc.Timeline(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range tl {
		if e.Kind == "comment" && e.Payload["body"] == "hello" {
			found = true
		}
	}
	if !found {
		t.Errorf("comment event missing")
	}
}
