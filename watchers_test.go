package ledger

import (
	"context"
	"testing"
)

func TestWatchUnwatch_Roundtrip(t *testing.T) {
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

	if err := svc.WatchIssue(ctx, issue.Key, "plumb", "plumb"); err != nil {
		t.Fatal(err)
	}

	list, err := svc.Watchers(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != "plumb" {
		t.Errorf("watchers after watch = %v, want [plumb]", list)
	}

	// Unwatch.
	if err := svc.UnwatchIssue(ctx, issue.Key, "plumb", "plumb"); err != nil {
		t.Fatal(err)
	}

	list2, err := svc.Watchers(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 0 {
		t.Errorf("watchers after unwatch = %v, want []", list2)
	}
}

func TestWatch_IsIdempotent(t *testing.T) {
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

	if err := svc.WatchIssue(ctx, issue.Key, "plumb", "plumb"); err != nil {
		t.Fatal(err)
	}
	if err := svc.WatchIssue(ctx, issue.Key, "plumb", "plumb"); err != nil {
		t.Fatal(err)
	}

	list, err := svc.Watchers(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("watchers = %v, want [plumb]", list)
	}
}

func TestWatch_WritesEvent(t *testing.T) {
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

	if err := svc.WatchIssue(ctx, issue.Key, "plumb", "plumb"); err != nil {
		t.Fatal(err)
	}

	tl, err := svc.Timeline(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range tl {
		if e.Kind == "watch" && e.Actor == "plumb" && e.Payload["aspect"] == "plumb" {
			found = true
		}
	}
	if !found {
		t.Errorf("watch event missing from timeline")
	}
}

func TestUnwatch_WritesEvent(t *testing.T) {
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

	if err := svc.WatchIssue(ctx, issue.Key, "plumb", "plumb"); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnwatchIssue(ctx, issue.Key, "plumb", "plumb"); err != nil {
		t.Fatal(err)
	}

	tl, err := svc.Timeline(ctx, issue.Key)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range tl {
		if e.Kind == "unwatch" && e.Actor == "plumb" && e.Payload["aspect"] == "plumb" {
			found = true
		}
	}
	if !found {
		t.Errorf("unwatch event missing from timeline")
	}
}
