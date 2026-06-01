package ledger

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCreateIssue_AllocatesKey(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	defer svc.Close()

	if err := svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	in := IssueDraft{
		Project:          "NEX",
		Type:             "Story",
		Summary:          "First story",
		Description:      "Hello",
		DefinitionOfDone: "- [ ] PR builds clean",
		Reporter:         "shadow",
	}
	created, err := svc.CreateIssue(ctx, in)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if created.Key != "NEX-1" {
		t.Errorf("Key = %q, want NEX-1", created.Key)
	}
	if created.Status != "To Do" {
		t.Errorf("default Status = %q, want To Do", created.Status)
	}

	// Second create increments.
	second, err := svc.CreateIssue(ctx, in)
	if err != nil {
		t.Fatalf("CreateIssue #2: %v", err)
	}
	if second.Key != "NEX-2" {
		t.Errorf("Key = %q, want NEX-2", second.Key)
	}

	// Round-trip via Get.
	got, err := svc.GetIssue(ctx, "NEX-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Summary != "First story" {
		t.Errorf("Summary = %q", got.Summary)
	}
}

// newTestService creates a service backed by a temp dir.
func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "ledger.db")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Close the DB before t.TempDir's RemoveAll runs (cleanups are LIFO), so the
	// SQLite file isn't held open — Windows can't unlink an open file.
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}
