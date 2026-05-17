package ledger

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCreateProject_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(context.Background(), Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Close()

	ctx := context.Background()
	if err := svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus engineering"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := svc.GetProject(ctx, "NEX")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Key != "NEX" || got.Name != "Nexus engineering" {
		t.Errorf("got %+v", got)
	}

	// Sequence row was auto-created.
	var nextSeq int
	if err := svc.db.QueryRowContext(ctx, `SELECT next_seq FROM project_sequences WHERE project = ?`, "NEX").Scan(&nextSeq); err != nil {
		t.Fatalf("sequence row missing: %v", err)
	}
	if nextSeq != 1 {
		t.Errorf("next_seq = %d, want 1", nextSeq)
	}
}
