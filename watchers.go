package ledger

import (
	"context"
)

// WatchIssue adds an aspect to an issue's watcher list. Idempotent —
// watching an already-watched issue is a no-op.
func (s *Service) WatchIssue(ctx context.Context, key, aspect, actor string) error {
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO watchers(issue_key, aspect) VALUES (?, ?)`,
		key, aspect,
	); err != nil {
		return err
	}

	if err := writeEvent(ctx, tx, key, "watch", actor, map[string]any{
		"aspect": aspect,
	}); err != nil {
		return err
	}

	return tx.Commit()
}

// UnwatchIssue removes an aspect from an issue's watcher list.
// Unwatching a non-watched issue is a no-op.
func (s *Service) UnwatchIssue(ctx context.Context, key, aspect, actor string) error {
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM watchers WHERE issue_key = ? AND aspect = ?`,
		key, aspect,
	); err != nil {
		return err
	}

	if err := writeEvent(ctx, tx, key, "unwatch", actor, map[string]any{
		"aspect": aspect,
	}); err != nil {
		return err
	}

	return tx.Commit()
}

// Watchers returns the list of aspects watching an issue, ordered by
// when they started watching (oldest first).
func (s *Service) Watchers(ctx context.Context, key string) ([]string, error) {
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT aspect FROM watchers WHERE issue_key = ? ORDER BY since ASC`, key,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
