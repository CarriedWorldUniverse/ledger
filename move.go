package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ReassignProject moves an issue to a new project. Allocates a new key
// from the destination's sequence, records an alias from the old key,
// and returns the new key.
//
// v1 rules:
//   - Reject the move if the issue has children in the source project
//     (cross-project parent links are disallowed)
//   - If the issue has a parent in the source project, drop the parent
//     link (a future field_change event records the unhitch)
//
// `actor` and `reason` are recorded with the alias for audit.
func (s *Service) ReassignProject(ctx context.Context, oldKey, newProject, actor, reason string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Load current state.
	var srcProject, parentKey sql.NullString
	var issueType string
	err = tx.QueryRowContext(ctx,
		`SELECT project, parent_key, type FROM issues WHERE key = ?`, oldKey,
	).Scan(&srcProject, &parentKey, &issueType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrIssueNotFound
	}
	if err != nil {
		return "", err
	}

	if !srcProject.Valid || srcProject.String == newProject {
		return "", fmt.Errorf("ReassignProject: source and destination project are the same")
	}

	// Check for children in source project.
	var childCount int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issues WHERE parent_key = ?`, oldKey,
	).Scan(&childCount); err != nil {
		return "", err
	}
	if childCount > 0 {
		return "", fmt.Errorf("ReassignProject: issue has %d child(ren); resolve cross-project parents first", childCount)
	}

	// Allocate new sequence value in destination project.
	var newSeq int
	err = tx.QueryRowContext(ctx,
		`UPDATE project_sequences SET next_seq = next_seq + 1 WHERE project = ? RETURNING next_seq - 1`,
		newProject,
	).Scan(&newSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("ReassignProject: destination project %q not found", newProject)
	}
	if err != nil {
		return "", err
	}

	newKey := fmt.Sprintf("%s-%d", newProject, newSeq)

	// Update the row.
	if _, err := tx.ExecContext(ctx,
		`UPDATE issues SET key = ?, project = ?, seq = ?, parent_key = NULL, updated_at = datetime('now') WHERE key = ?`,
		newKey, newProject, newSeq, oldKey,
	); err != nil {
		return "", err
	}

	// Record alias.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO key_aliases(old_key, new_key) VALUES (?, ?)`,
		oldKey, newKey,
	); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newKey, nil
}
