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
