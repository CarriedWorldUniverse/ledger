package ledger

import (
	"context"
	"testing"
)

func TestSearch_ByAssigneeAndStatus(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	mk := func(summary, assignee string) string {
		issue, _ := svc.CreateIssue(ctx, IssueDraft{
			Project: "NEX", Type: "Story", Summary: summary,
			DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: assignee,
		})
		return issue.Key
	}
	a := mk("for anvil", "anvil")
	_ = mk("for plumb", "plumb")
	_ = svc.TransitionIssue(ctx, a, "In Progress", "anvil")

	results, err := svc.Search(ctx, SearchFilter{
		AssigneeAspect: "anvil",
		Statuses:       []string{"In Progress"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Key != a {
		t.Errorf("Key = %q, want %q", results[0].Key, a)
	}
}

func TestListMy_ReturnsAspectAndTeamIssues(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "mine",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

	results, err := svc.ListMy(ctx, "anvil", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d", len(results))
	}
}

// NEX-323: FTS5-backed full-text search across summary / description /
// DoD / comment bodies. These tests live or die on the schema's FTS5
// triggers populating issue_search correctly under Create/Update/Comment
// flows — the SQL is small but easy to get wrong.

func TestFindByText_MatchesSummary(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	a, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "DeepSeek wire format quirks",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	_, _ = svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "unrelated thing",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})

	res, err := svc.FindByText(ctx, "DeepSeek", 10)
	if err != nil {
		t.Fatalf("FindByText: %v", err)
	}
	if len(res) != 1 || res[0].Key != a.Key {
		t.Fatalf("got %+v, want only %s", res, a.Key)
	}
}

func TestFindByText_MatchesDescriptionAndDoD(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	desc, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "s1",
		Description:      "Investigate intermittent broker disconnect under heavy load.",
		DefinitionOfDone: "- [ ] x", Reporter: "shadow",
	})
	dod, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "s2",
		DefinitionOfDone: "- [ ] document the throttling thresholds clearly",
		Reporter:         "shadow",
	})

	got1, _ := svc.FindByText(ctx, "broker", 10)
	if len(got1) != 1 || got1[0].Key != desc.Key {
		t.Fatalf("description match: got %+v, want %s", got1, desc.Key)
	}
	got2, _ := svc.FindByText(ctx, "throttling", 10)
	if len(got2) != 1 || got2[0].Key != dod.Key {
		t.Fatalf("DoD match: got %+v, want %s", got2, dod.Key)
	}
}

func TestFindByText_MatchesCommentBody(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "boring summary",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	if err := svc.CommentIssue(ctx, issue.Key, "anvil", "this turned out to be a permissions issue with the auth_classifier"); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}

	res, err := svc.FindByText(ctx, "auth_classifier", 10)
	if err != nil {
		t.Fatalf("FindByText: %v", err)
	}
	if len(res) != 1 || res[0].Key != issue.Key {
		t.Fatalf("got %+v, want %s", res, issue.Key)
	}
}

func TestFindByText_UpdatePropagates(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "original word",
		DefinitionOfDone: "- [ ] x", Reporter: "shadow",
	})
	// Update changes the summary; FTS row should reflect the new text.
	newSum := "freshNoun replaced original"
	if err := svc.UpdateIssue(ctx, issue.Key, UpdatePatch{Summary: &newSum}, "shadow"); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	hit, _ := svc.FindByText(ctx, "freshNoun", 10)
	if len(hit) != 1 || hit[0].Key != issue.Key {
		t.Fatalf("post-update: got %+v", hit)
	}
	// Old text shouldn't match if it's not in the new summary.
	miss, _ := svc.FindByText(ctx, "differentword", 10)
	if len(miss) != 0 {
		t.Fatalf("expected no match for term absent from updated content; got %+v", miss)
	}
}

func TestFindByText_DedupesPerIssue(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	issue, _ := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "uniqueTerm here",
		Description: "uniqueTerm again in description", DefinitionOfDone: "- [ ] go",
		Reporter: "shadow",
	})
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "uniqueTerm in a comment too")
	_ = svc.CommentIssue(ctx, issue.Key, "anvil", "and uniqueTerm one more time")

	res, _ := svc.FindByText(ctx, "uniqueTerm", 10)
	if len(res) != 1 {
		t.Fatalf("expected single deduped result, got %d: %+v", len(res), res)
	}
}

func TestFindByText_EmptyQueryRejected(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	if _, err := svc.FindByText(ctx, "   ", 10); err == nil {
		t.Errorf("expected error on empty query")
	}
}

func TestListReady_ExcludesNonStartable(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "ready",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

	results, err := svc.ListReady(ctx, "anvil", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
}

func TestCreateIssue_SkillsRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})

	created, err := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "skilled",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil",
		Skills: []string{"go", "ledger"}})
	if err != nil {
		t.Fatal(err)
	}
	// CreateIssue returns via GetIssue, so this already exercises the read path.
	if got := created.Skills; len(got) != 2 || got[0] != "go" || got[1] != "ledger" {
		t.Fatalf("created skills = %v, want [go ledger]", got)
	}
	// Explicit re-fetch to be sure it persisted.
	fetched, err := svc.GetIssue(ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Skills) != 2 {
		t.Fatalf("fetched skills = %v, want 2", fetched.Skills)
	}
}

func TestListReady_SkillFilter(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	mk := func(summary string, skills []string) string {
		iss, err := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: summary,
			DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil", Skills: skills})
		if err != nil {
			t.Fatal(err)
		}
		return iss.Key
	}
	goKey := mk("go work", []string{"go"})
	mixKey := mk("mixed", []string{"rust", "python"}) // requires a skill the worker lacks
	bareKey := mk("no skills", nil)

	// Worker serves only "go": should get the go issue + the no-skill issue,
	// NOT the rust/python one (python ⊄ {go}).
	got, err := svc.ListReady(ctx, "anvil", nil, []string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, r := range got {
		keys[r.Key] = true
	}
	if !keys[goKey] || !keys[bareKey] {
		t.Errorf("want go+bare in results, got %v", keys)
	}
	if keys[mixKey] {
		t.Errorf("rust/python issue should be excluded for a go-only worker")
	}

	// Empty skill set = no filtering: all three returned.
	all, err := svc.ListReady(ctx, "anvil", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("no-filter len = %d, want 3", len(all))
	}
}

func TestListReady_ExcludesBlocked(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	mk := func(summary string) string {
		iss, err := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: summary,
			DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})
		if err != nil {
			t.Fatal(err)
		}
		return iss.Key
	}
	blocker := mk("blocker")
	blocked := mk("blocked")
	// blocker blocks blocked: 'blocked' is not ready until 'blocker' is terminal.
	if err := svc.LinkIssues(ctx, blocker, blocked, LinkBlocks, "shadow"); err != nil {
		t.Fatal(err)
	}

	readyKeys := func() map[string]bool {
		refs, err := svc.ListReady(ctx, "anvil", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, r := range refs {
			m[r.Key] = true
		}
		return m
	}

	got := readyKeys()
	if !got[blocker] {
		t.Errorf("blocker should be ready (it has no open blocker)")
	}
	if got[blocked] {
		t.Errorf("blocked issue should be excluded while its blocker is open")
	}

	// Cancel the blocker (terminal) — the blocked issue becomes ready.
	if err := svc.TransitionIssue(ctx, blocker, "Cancelled", "shadow"); err != nil {
		t.Fatal(err)
	}
	got = readyKeys()
	if !got[blocked] {
		t.Errorf("blocked issue should be ready once its blocker is terminal")
	}
	if got[blocker] {
		t.Errorf("cancelled blocker should no longer be ready (not a startable status)")
	}
}
