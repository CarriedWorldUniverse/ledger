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
