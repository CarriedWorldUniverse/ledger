package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func claimTestService(t *testing.T) *Service {
	t.Helper()
	svc := verbsTestService(t) // reuse the helper from verbs_test.go
	return svc
}

func TestClaimIssue_FreshClaimSetsAssigneeAndInProgress(t *testing.T) {
	ctx := context.Background()
	svc := claimTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "claim me",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	got, err := svc.ClaimIssue(ctx, iss.Key, "anvil")
	if err != nil {
		t.Fatal(err)
	}
	if got.AssigneeAspect != "anvil" {
		t.Errorf("assignee = %q, want anvil", got.AssigneeAspect)
	}
	if got.Status != "In Progress" {
		t.Errorf("status = %q, want In Progress", got.Status)
	}

	// A claim event was appended.
	tl, _ := svc.Timeline(ctx, iss.Key)
	var sawClaim bool
	for _, e := range tl {
		if e.Kind == "claim" && e.Actor == "anvil" {
			sawClaim = true
		}
	}
	if !sawClaim {
		t.Error("no claim event for anvil")
	}
}

func TestClaimIssue_AlreadyClaimedByOther409(t *testing.T) {
	ctx := context.Background()
	svc := claimTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	if _, err := svc.ClaimIssue(ctx, iss.Key, "anvil"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ClaimIssue(ctx, iss.Key, "keel")
	if !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("second claim err = %v, want ErrAlreadyClaimed", err)
	}
}

func TestClaimIssue_IdempotentForSameAgent(t *testing.T) {
	ctx := context.Background()
	svc := claimTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	if _, err := svc.ClaimIssue(ctx, iss.Key, "anvil"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ClaimIssue(ctx, iss.Key, "anvil")
	if err != nil {
		t.Fatalf("re-claim by same agent err = %v", err)
	}
	if got.AssigneeAspect != "anvil" || got.Status != "In Progress" {
		t.Fatalf("idempotent re-claim = %+v", got)
	}
}

func TestClaimIssue_EpicNotClaimable(t *testing.T) {
	ctx := context.Background()
	svc := claimTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Epic", Summary: "big",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	_, err := svc.ClaimIssue(ctx, iss.Key, "anvil")
	if !errors.Is(err, ErrNotClaimable) {
		t.Fatalf("epic claim err = %v, want ErrNotClaimable", err)
	}
}

func TestClaimIssue_UnknownKeyNotFound(t *testing.T) {
	ctx := context.Background()
	svc := claimTestService(t)
	_, err := svc.ClaimIssue(ctx, "NEX-999", "anvil")
	if !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("unknown-key claim err = %v, want ErrIssueNotFound", err)
	}
}

func TestClaimIssue_ConcurrentClaimsExactlyOneWins(t *testing.T) {
	ctx := context.Background()
	svc := claimTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "race",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	const n = 8
	var wg sync.WaitGroup
	wins := make([]bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			agent := "agent" + string(rune('A'+i))
			_, err := svc.ClaimIssue(ctx, iss.Key, agent)
			wins[i] = err == nil
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, w := range wins {
		if w {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1", winners)
	}
}

func TestRESTClaim_SuccessAndConflict(t *testing.T) {
	ctx := context.Background()
	svc := claimTestService(t)
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()

	// First claim — pass actor in the body (no claims context here).
	body := bytes.NewBufferString(`{"actor":"anvil"}`)
	resp, err := http.Post(srv.URL+"/api/issues/"+iss.Key+"/claim", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d, want 200", resp.StatusCode)
	}
	var got Issue
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.AssigneeAspect != "anvil" || got.Status != "In Progress" {
		t.Fatalf("claimed issue = %+v", got)
	}

	// Second claim by a different agent → 409.
	body2 := bytes.NewBufferString(`{"actor":"keel"}`)
	resp2, err := http.Post(srv.URL+"/api/issues/"+iss.Key+"/claim", "application/json", body2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting claim status = %d, want 409", resp2.StatusCode)
	}
}
