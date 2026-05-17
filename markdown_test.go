package ledger

import (
	"context"
	"strings"
	"testing"
)

func TestMaterialiseMarkdown_Basic(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "Story title",
		Description: "some body", DefinitionOfDone: "- [x] done",
		Reporter: "shadow", AssigneeAspect: "anvil",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "first comment")
	_ = svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil")

	md, err := svc.MaterialiseMarkdown(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"key: " + issue.Key,
		"Story title",
		"## Description",
		"some body",
		"## Definition of Done",
		"- [x] done",
		"## Timeline",
		"anvil (comment)",
		"first comment",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n----\n%s", want, md)
		}
	}
}

func TestMaterialiseMarkdown_IncludesWatchers(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "watched",
		Description: "body", DefinitionOfDone: "- [x] go",
		Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.WatchIssue(ctx, issue.Key, "plumb", "plumb")
	_ = svc.WatchIssue(ctx, issue.Key, "anvil", "anvil")

	md, err := svc.MaterialiseMarkdown(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(md, "watchers: [") || !strings.Contains(md, "plumb") || !strings.Contains(md, "anvil") {
		t.Errorf("watchers line missing or wrong:\n%s", md)
	}
}
