package ledger

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNew_OpensFreshDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ledger.db")

	svc, err := New(context.Background(), Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Close()

	if svc == nil {
		t.Fatal("New returned nil service")
	}
}

func TestNew_AppliesSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ledger.db")

	svc, err := New(context.Background(), Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Close()

	var version int
	err = svc.db.QueryRow(`SELECT version FROM schema_versions ORDER BY version DESC LIMIT 1`).Scan(&version)
	if err != nil {
		t.Fatalf("schema_versions not present: %v", err)
	}
	if version < 1 {
		t.Errorf("expected schema version >= 1, got %d", version)
	}
}
