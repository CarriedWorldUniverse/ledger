package ledger

import (
	"context"
	"errors"
	"testing"
)

// twoIssuesFixture creates two issues in the same project; returns
// their keys. Convenience for link-related tests where the issues
// themselves aren't load-bearing.
func twoIssuesFixture(t *testing.T) (svc *Service, a, b string) {
	t.Helper()
	ctx := context.Background()
	svc = newTestService(t)
	t.Cleanup(func() { svc.Close() })
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	ai, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "A",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	bi, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "B",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	return svc, ai.Key, bi.Key
}

func TestLinkIssues_BlocksRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)

	if err := svc.LinkIssues(ctx, a, b, LinkBlocks, "shadow"); err != nil {
		t.Fatalf("link: %v", err)
	}

	// b is blocked-by a; b.Links should show one incoming 'blocks'.
	bLinks, err := svc.Links(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(bLinks) != 1 {
		t.Fatalf("b links: got %d, want 1", len(bLinks))
	}
	if bLinks[0].Direction != Incoming {
		t.Errorf("b direction = %q, want incoming", bLinks[0].Direction)
	}
	if bLinks[0].Link.FromKey != a || bLinks[0].Link.ToKey != b || bLinks[0].Link.Type != LinkBlocks {
		t.Errorf("unexpected link shape: %+v", bLinks[0].Link)
	}

	// a's outgoing view.
	aLinks, _ := svc.Links(ctx, a)
	if len(aLinks) != 1 || aLinks[0].Direction != Outgoing {
		t.Errorf("a links: %+v", aLinks)
	}
}

func TestLinkIssues_IsBlockedFollowsBlockerStatus(t *testing.T) {
	// Core orchestration property: a ticket is IsBlocked iff at least
	// one of its 'blocks' incoming edges is from a non-terminal issue.
	// When the blocker reaches Done, IsBlocked flips to false.
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)

	if blocked, _ := svc.IsBlocked(ctx, b); blocked {
		t.Fatal("b shouldn't be blocked before any link")
	}

	_ = svc.LinkIssues(ctx, a, b, LinkBlocks, "shadow")

	// a is "To Do" (non-terminal) — b is now blocked.
	blocked, err := svc.IsBlocked(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Error("b should be blocked while a is To Do")
	}

	// Walk a to Done (the Ready to Start path lands in #29; use
	// direct path here so this test doesn't depend on that PR).
	_ = svc.TransitionIssue(ctx, a, "In Progress", "anvil")
	_ = svc.TransitionIssue(ctx, a, "In Review", "anvil")
	_ = svc.TransitionIssue(ctx, a, "Done", "anvil")

	blocked, err = svc.IsBlocked(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Error("b should no longer be blocked once a is Done")
	}
}

func TestLinkIssues_IsBlockedWithCancelledBlocker(t *testing.T) {
	// Cancelled is also a terminal state for the blocker — IsBlocked
	// should be false once the only blocker is cancelled.
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)
	_ = svc.LinkIssues(ctx, a, b, LinkBlocks, "shadow")
	_ = svc.TransitionIssue(ctx, a, "Cancelled", "shadow")

	blocked, _ := svc.IsBlocked(ctx, b)
	if blocked {
		t.Error("b should not be blocked once a is Cancelled")
	}
}

func TestLinkIssues_IdempotentLink(t *testing.T) {
	// Re-linking the same (from, to, type) is a no-op. Useful for
	// orchestration code that may re-emit the same link without
	// having to dedupe in the caller. Verifies no duplicate event
	// spam in the timeline.
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)
	_ = svc.LinkIssues(ctx, a, b, LinkBlocks, "shadow")
	_ = svc.LinkIssues(ctx, a, b, LinkBlocks, "shadow") // again

	links, _ := svc.Links(ctx, a)
	if len(links) != 1 {
		t.Errorf("expected 1 link after idempotent re-link, got %d", len(links))
	}

	// Should be exactly one link_to event (not two).
	timeline, _ := svc.Timeline(ctx, a)
	linkEvents := 0
	for _, e := range timeline {
		if e.Kind == "field_change" {
			if f, _ := e.Payload["field"].(string); f == "link_to" {
				linkEvents++
			}
		}
	}
	if linkEvents != 1 {
		t.Errorf("expected 1 link_to event after idempotent re-link, got %d", linkEvents)
	}
}

func TestLinkIssues_UnlinkRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)
	_ = svc.LinkIssues(ctx, a, b, LinkBlocks, "shadow")

	if err := svc.UnlinkIssues(ctx, a, b, LinkBlocks, "shadow"); err != nil {
		t.Fatalf("unlink: %v", err)
	}

	links, _ := svc.Links(ctx, a)
	if len(links) != 0 {
		t.Errorf("expected 0 links after unlink, got %d", len(links))
	}

	// IsBlocked clears too.
	blocked, _ := svc.IsBlocked(ctx, b)
	if blocked {
		t.Error("b should not be blocked after unlink")
	}
}

func TestLinkIssues_UnlinkIdempotent(t *testing.T) {
	// Unlinking a non-existent edge is a no-op (no error, no event).
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)

	if err := svc.UnlinkIssues(ctx, a, b, LinkBlocks, "shadow"); err != nil {
		t.Errorf("unlink-when-absent should be no-op, got %v", err)
	}
}

func TestLinkIssues_RejectsSelfLink(t *testing.T) {
	ctx := context.Background()
	svc, a, _ := twoIssuesFixture(t)

	err := svc.LinkIssues(ctx, a, a, LinkBlocks, "shadow")
	if !errors.Is(err, ErrSelfLink) {
		t.Errorf("got %v, want ErrSelfLink", err)
	}
}

func TestLinkIssues_RejectsInvalidType(t *testing.T) {
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)

	err := svc.LinkIssues(ctx, a, b, LinkType("invalid"), "shadow")
	if !errors.Is(err, ErrInvalidLinkType) {
		t.Errorf("got %v, want ErrInvalidLinkType", err)
	}
}

func TestLinkIssues_RelatesToDoesNotBlock(t *testing.T) {
	// 'relates-to' is editorial only — must NOT affect IsBlocked.
	ctx := context.Background()
	svc, a, b := twoIssuesFixture(t)
	_ = svc.LinkIssues(ctx, a, b, LinkRelatesTo, "shadow")

	if blocked, _ := svc.IsBlocked(ctx, b); blocked {
		t.Error("relates-to should not block; b should not be IsBlocked")
	}
}

func TestLinkIssues_BlockersListed(t *testing.T) {
	// Multiple blockers: Blockers() returns them all (sorted by key).
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	a, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "A", DefinitionOfDone: "- [x]g", Reporter: "shadow"})
	b, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "B", DefinitionOfDone: "- [x]g", Reporter: "shadow"})
	c, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "C", DefinitionOfDone: "- [x]g", Reporter: "shadow"})

	_ = svc.LinkIssues(ctx, a.Key, c.Key, LinkBlocks, "shadow")
	_ = svc.LinkIssues(ctx, b.Key, c.Key, LinkBlocks, "shadow")

	blockers, err := svc.Blockers(ctx, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 2 {
		t.Fatalf("expected 2 blockers, got %d: %v", len(blockers), blockers)
	}
	// Order is by key ascending.
	if blockers[0] != a.Key || blockers[1] != b.Key {
		t.Errorf("blockers = %v, want sorted [%s %s]", blockers, a.Key, b.Key)
	}
}

func TestLinkIssues_LinksFollowKeyRenameOnMove(t *testing.T) {
	// Moving an issue across projects rewrites its key (see move.go).
	// FK ON UPDATE CASCADE on issue_links should follow that rename
	// — links survive the move with the new key.
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_ = svc.CreateProject(ctx, Project{Key: "OSS", Name: "OSS"})
	a, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "A", DefinitionOfDone: "- [x]g", Reporter: "shadow"})
	b, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "B", DefinitionOfDone: "- [x]g", Reporter: "shadow"})
	_ = svc.LinkIssues(ctx, a.Key, b.Key, LinkBlocks, "shadow")

	// Move a to OSS — a's key changes.
	newKey, err := svc.ReassignProject(ctx, a.Key, "OSS", "shadow", "rehome")
	if err != nil {
		t.Fatal(err)
	}

	// The link should still exist, with the new from_key.
	bLinks, _ := svc.Links(ctx, b.Key)
	if len(bLinks) != 1 {
		t.Fatalf("expected 1 link to b after move, got %d", len(bLinks))
	}
	if bLinks[0].Link.FromKey != newKey {
		t.Errorf("link from_key = %q, want renamed %q", bLinks[0].Link.FromKey, newKey)
	}

	// IsBlocked still works (a-now-newKey is still To Do).
	if blocked, _ := svc.IsBlocked(ctx, b.Key); !blocked {
		t.Error("b should still be blocked after blocker's key rename")
	}
}

func TestLinkIssues_HidesCrossOrgLink(t *testing.T) {
	// Tenancy: a nexus-org caller can't link to an acme-org issue.
	// Per the hide-existence pattern, the acme issue looks Not Found.
	svc, _, _, nexusIssue, acmeIssue := seedMultiOrgFixture(t)
	nexusCtx := withClaims(context.Background(), "shadow", "nexus", "member")

	err := svc.LinkIssues(nexusCtx, nexusIssue, acmeIssue, LinkBlocks, "shadow")
	if !errors.Is(err, ErrIssueNotFound) {
		t.Errorf("cross-org link: got %v, want ErrIssueNotFound", err)
	}
}
