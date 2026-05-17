package ledger

import (
	"context"
	"fmt"
	"strings"
)

// CommentIssue appends a comment to the issue's timeline. Comments are
// immutable; the only way to "correct" one is a new comment.
func (s *Service) CommentIssue(ctx context.Context, key, actor, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("CommentIssue: empty body")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := writeEvent(ctx, tx, key, "comment", actor, map[string]any{"body": body}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE issues SET updated_at = datetime('now') WHERE key = ?`, key,
	); err != nil {
		return err
	}
	return tx.Commit()
}
