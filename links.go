package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// LinkType discriminates issue-link relationships. v1 supports
// 'blocks' (orchestration-load-bearing) and 'relates-to' (editorial).
// More types may be added later; the type column is validated in
// code rather than via a SQL CHECK so additions don't require a
// table recreate.
type LinkType string

const (
	LinkBlocks    LinkType = "blocks"
	LinkRelatesTo LinkType = "relates-to"
)

func (t LinkType) valid() bool {
	switch t {
	case LinkBlocks, LinkRelatesTo:
		return true
	}
	return false
}

// IssueLink is one edge in the issue-links graph.
type IssueLink struct {
	FromKey   string
	ToKey     string
	Type      LinkType
	CreatedAt string
	CreatedBy string
}

// Direction tags a link from the perspective of a specific issue.
// Used by Links() so callers can render "this issue blocks X / is
// blocked by Y" without re-querying.
type Direction string

const (
	Outgoing Direction = "outgoing" // from = this issue
	Incoming Direction = "incoming" // to   = this issue
)

// DirectedLink pairs a Link with the direction it travels relative
// to a given issue's vantage point.
type DirectedLink struct {
	Link      IssueLink
	Direction Direction
}

var (
	// ErrInvalidLinkType signals an unknown LinkType. v1 valid set
	// is {blocks, relates-to}.
	ErrInvalidLinkType = errors.New("ledger: invalid link type")

	// ErrSelfLink signals an attempt to link an issue to itself.
	// Blocks-self would deadlock the orchestration ready check;
	// relates-to-self is harmless but useless. Reject both.
	ErrSelfLink = errors.New("ledger: issue cannot link to itself")
)

// LinkIssues creates an edge from→to of the given type. Tenancy:
// caller must be able to access both endpoints (cross-org callers
// see ErrIssueNotFound from callerCanAccessIssue). Idempotent —
// re-linking the same (from, to, type) tuple is a no-op (INSERT OR
// IGNORE). Writes a field_change event on BOTH issues so the
// timelines show the relationship from both sides.
func (s *Service) LinkIssues(ctx context.Context, fromKey, toKey string, linkType LinkType, actor string) error {
	if !linkType.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidLinkType, linkType)
	}
	if fromKey == toKey {
		return ErrSelfLink
	}
	if err := s.callerCanAccessIssue(ctx, fromKey); err != nil {
		return err
	}
	if err := s.callerCanAccessIssue(ctx, toKey); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO issue_links(from_key, to_key, type, created_by) VALUES (?, ?, ?, ?)`,
		fromKey, toKey, string(linkType), actor,
	)
	if err != nil {
		return fmt.Errorf("LinkIssues: insert: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// Already existed; idempotent return without event spam.
		return tx.Commit()
	}

	// Emit a field_change event on both ends so the timelines reflect
	// the relationship symmetrically. Payload distinguishes the two
	// perspectives ('link_to' vs 'link_from') for renderers.
	if err := writeEvent(ctx, tx, fromKey, "field_change", actor, map[string]any{
		"field":    "link_to",
		"link_type": string(linkType),
		"to_key":   toKey,
	}); err != nil {
		return fmt.Errorf("LinkIssues: from-event: %w", err)
	}
	if err := writeEvent(ctx, tx, toKey, "field_change", actor, map[string]any{
		"field":      "link_from",
		"link_type":  string(linkType),
		"from_key":   fromKey,
	}); err != nil {
		return fmt.Errorf("LinkIssues: to-event: %w", err)
	}

	return tx.Commit()
}

// UnlinkIssues removes the edge from→to of the given type. No-op
// if the edge doesn't exist (idempotent). Writes a field_change
// event on both issues when an edge is actually removed.
func (s *Service) UnlinkIssues(ctx context.Context, fromKey, toKey string, linkType LinkType, actor string) error {
	if !linkType.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidLinkType, linkType)
	}
	if err := s.callerCanAccessIssue(ctx, fromKey); err != nil {
		return err
	}
	if err := s.callerCanAccessIssue(ctx, toKey); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`DELETE FROM issue_links WHERE from_key = ? AND to_key = ? AND type = ?`,
		fromKey, toKey, string(linkType),
	)
	if err != nil {
		return fmt.Errorf("UnlinkIssues: delete: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// Nothing to do; no event spam.
		return tx.Commit()
	}

	if err := writeEvent(ctx, tx, fromKey, "field_change", actor, map[string]any{
		"field":     "unlink_to",
		"link_type": string(linkType),
		"to_key":    toKey,
	}); err != nil {
		return fmt.Errorf("UnlinkIssues: from-event: %w", err)
	}
	if err := writeEvent(ctx, tx, toKey, "field_change", actor, map[string]any{
		"field":     "unlink_from",
		"link_type": string(linkType),
		"from_key":  fromKey,
	}); err != nil {
		return fmt.Errorf("UnlinkIssues: to-event: %w", err)
	}

	return tx.Commit()
}

// Links returns every edge touching `key`, tagged with direction
// (outgoing = key is from; incoming = key is to). Includes ALL
// link types — callers filter by Type if they want a specific
// relationship. Cross-org callers see ErrIssueNotFound via the
// access gate.
func (s *Service) Links(ctx context.Context, key string) ([]DirectedLink, error) {
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT from_key, to_key, type, created_at, created_by, 'outgoing' AS dir
		FROM issue_links WHERE from_key = ?
		UNION ALL
		SELECT from_key, to_key, type, created_at, created_by, 'incoming' AS dir
		FROM issue_links WHERE to_key = ?
		ORDER BY created_at`, key, key,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DirectedLink
	for rows.Next() {
		var l IssueLink
		var dir string
		if err := rows.Scan(&l.FromKey, &l.ToKey, &l.Type, &l.CreatedAt, &l.CreatedBy, &dir); err != nil {
			return nil, err
		}
		out = append(out, DirectedLink{Link: l, Direction: Direction(dir)})
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

// IsBlocked reports whether `key` has at least one 'blocks' incoming
// edge from an issue in a non-terminal state (anything other than
// Done / Cancelled). Used by the orchestration scheduler's ready-
// computation: a ticket is ready_to_start IFF it's assigned AND
// not IsBlocked.
//
// Returns false (not blocked) when the issue itself doesn't exist —
// callers should validate existence separately if they care; this
// method is for the scheduler's hot path and shouldn't gatekeep
// existence checks for performance.
func (s *Service) IsBlocked(ctx context.Context, key string) (bool, error) {
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		// Hide existence — same pattern as GetIssue.
		if errors.Is(err, ErrIssueNotFound) {
			return false, err
		}
		return false, err
	}

	var hasBlocker int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM issue_links l
			JOIN issues i ON i.key = l.from_key
			WHERE l.to_key = ? AND l.type = ?
			  AND i.status NOT IN ('Done', 'Cancelled')
		)`, key, string(LinkBlocks),
	).Scan(&hasBlocker)
	if err != nil {
		return false, fmt.Errorf("IsBlocked: %w", err)
	}
	return hasBlocker == 1, nil
}

// Blockers returns the keys of all issues that 'blocks'-link to
// `key`, regardless of their current status. Used by the scheduler
// to log/report which dependencies are holding a ticket back.
func (s *Service) Blockers(ctx context.Context, key string) ([]string, error) {
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_key FROM issue_links WHERE to_key = ? AND type = ? ORDER BY from_key`,
		key, string(LinkBlocks),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}
