package ledger

import (
	"context"
	"path/filepath"
	"testing"
)

func TestApplySchema_Idempotent(t *testing.T) {
	// NEX-291: calling ledger.New twice on the same DB used to fail
	// with "duplicate column name: organisation" because applySchema
	// re-ran the v8 ALTER TABLE migration without an idempotency guard.
	dir := t.TempDir()
	cfg := Config{DBPath: filepath.Join(dir, "ledger.db")}
	ctx := context.Background()

	svc1, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	svc1.Close()

	svc2, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("second New (should be idempotent): %v", err)
	}
	svc2.Close()

	// Third run for good measure — applySchema is reachable on every
	// service start, so any drift between runs would show up here too.
	svc3, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("third New: %v", err)
	}
	svc3.Close()
}

func TestSplitSQLStatements_HandlesCommentsAndEmptyBlocks(t *testing.T) {
	in := `
-- top comment, full line
CREATE TABLE foo (id INTEGER);

-- another full-comment block
-- followed by another comment

INSERT INTO foo VALUES (1);

-- trailing comment
`
	got, err := splitSQLStatements(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 real statements, got %d: %v", len(got), got)
	}
}

func TestIsAlterAddColumn(t *testing.T) {
	cases := []struct {
		stmt string
		want bool
	}{
		{"ALTER TABLE issues ADD COLUMN external_refs TEXT", true},
		{"alter table teams add column project text", true},
		{"CREATE TABLE x (id INTEGER)", false},
		{"ALTER TABLE x RENAME TO y", false},
		{"INSERT INTO x VALUES (1)", false},
	}
	for _, c := range cases {
		if got := isAlterAddColumn(c.stmt); got != c.want {
			t.Errorf("isAlterAddColumn(%q) = %v, want %v", c.stmt, got, c.want)
		}
	}
}

func TestIsDuplicateColumnErr(t *testing.T) {
	if isDuplicateColumnErr(nil) {
		t.Error("nil err should not match")
	}
	// We don't import the sqlite driver here just to construct the
	// real error type, but the message format is stable across driver
	// versions. Substring match covers both ncruces and mattn shapes.
	if !isDuplicateColumnErr(testErr("sqlite3: SQL logic error: duplicate column name: organisation")) {
		t.Error("driver-format error should match")
	}
	if isDuplicateColumnErr(testErr("something else entirely")) {
		t.Error("unrelated error should not match")
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }
