package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ReassignProject moves an issue to a new project. Allocates a new key
// from the destination's sequence, records an alias from the old key,
// writes a `move` event on the issue's timeline (carrying actor +
// reason for audit), and returns the new key.
//
// v1 rules:
//   - Reject the move if the issue has children in the SOURCE project
//     (cross-project parent links are disallowed). Children already
//     resident in the destination — left over from prior moves — do
//     NOT block.
//   - If the issue has a parent, drop the parent link and write a
//     `field_change` event recording the unhitch.
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

	// Check for children IN THE SOURCE PROJECT. Children that have
	// already moved to the destination (or anywhere else) shouldn't
	// block — the cross-project-parent constraint only applies to the
	// source.
	var childCount int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issues WHERE parent_key = ? AND project = ?`, oldKey, srcProject.String,
	).Scan(&childCount); err != nil {
		return "", err
	}
	if childCount > 0 {
		return "", fmt.Errorf("ReassignProject: issue has %d child(ren) in source project %q; resolve cross-project parents first", childCount, srcProject.String)
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

	// If the issue had a parent, write a field_change event for the
	// unhitch on the NEW key (events table FK cascades on UPDATE so
	// any prior events follow the key rename).
	if parentKey.Valid && parentKey.String != "" {
		if err := writeEvent(ctx, tx, newKey, "field_change", actor, map[string]any{
			"field":         "parent_key",
			"from":          parentKey.String,
			"to":            "",
			"reason":        "dropped on cross-project move",
			"move_old_key":  oldKey,
			"move_new_key":  newKey,
		}); err != nil {
			return "", err
		}
	}

	// Write the move event itself — captures actor + reason for audit
	// (the broken contract that the original code documented but never
	// implemented). Lands on the NEW key so the timeline is contiguous
	// post-rename.
	if err := writeEvent(ctx, tx, newKey, "move", actor, map[string]any{
		"old_key":     oldKey,
		"new_key":     newKey,
		"old_project": srcProject.String,
		"new_project": newProject,
		"reason":      reason,
	}); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newKey, nil
}
