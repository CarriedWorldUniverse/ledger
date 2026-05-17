package ledger

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

// applySchema runs the embedded idempotent DDL. Safe to call on every
// service start.
func applySchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("ledger.applySchema: %w", err)
	}
	return nil
}
