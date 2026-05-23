package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Event is a timeline entry.
type Event struct {
	ID       int64
	IssueKey string
	Seq      int
	Kind     string
	Actor    string
	At       string
	Payload  map[string]any
}

// writeEvent appends an event within tx. Callers wrap this in the same
// transaction as the mutation it describes.
func writeEvent(ctx context.Context, tx *sql.Tx, issueKey, kind, actor string, payload map[string]any) error {
	var nextSeq int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM events WHERE issue_key = ?`, issueKey,
	).Scan(&nextSeq)
	if err != nil {
		return err
	}
	pjson, _ := json.Marshal(payload)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO events(issue_key, seq, kind, actor, payload) VALUES (?, ?, ?, ?, ?)`,
		issueKey, nextSeq, kind, actor, string(pjson),
	)
	return err
}

// DefaultUpdatesLimit caps a single ListMyUpdates response when the
// caller doesn't specify a limit. Tuned to bound network + memory
// without forcing clients to paginate trivial inboxes.
const DefaultUpdatesLimit = 200

// MaxUpdatesLimit hard-caps the upper bound a caller can request to
// prevent a misbehaving aspect from pulling the entire timeline in
// one shot.
const MaxUpdatesLimit = 1000

// ListMyUpdates returns events on issues assigned to OR watched by
// `aspect`, with `e.id > sinceID`, ordered by id ASC, up to `limit`
// rows. The cursor is the AUTOINCREMENT event id (NOT the wall-clock
// timestamp) — second-precision timestamps would lose events that
// cluster within a single tick under the previous WHERE at > ? query.
//
// Polling protocol:
//   - First poll: pass sinceID = 0 (returns the head of the inbox).
//   - Each subsequent poll: pass sinceID = highest id seen so far.
//   - If len(returned) == limit, there may be more — poll again
//     immediately with the new cursor before sleeping.
//
// limit ≤ 0 substitutes DefaultUpdatesLimit; limit > MaxUpdatesLimit
// is clamped down. Result is empty (not nil) when no events match.
func (s *Service) ListMyUpdates(ctx context.Context, aspect string, sinceID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = DefaultUpdatesLimit
	}
	if limit > MaxUpdatesLimit {
		limit = MaxUpdatesLimit
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.issue_key, e.seq, e.kind, e.actor, e.at, e.payload
		FROM events e
		JOIN issues i ON i.key = e.issue_key
		LEFT JOIN watchers w ON w.issue_key = e.issue_key AND w.aspect = ?
		WHERE (i.assignee_aspect = ? OR w.aspect IS NOT NULL)
		  AND e.id > ?
		ORDER BY e.id ASC
		LIMIT ?`, aspect, aspect, sinceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var payload string
		if err := rows.Scan(&e.ID, &e.IssueKey, &e.Seq, &e.Kind, &e.Actor, &e.At, &payload); err != nil {
			return nil, err
		}
		if payload != "" {
			_ = json.Unmarshal([]byte(payload), &e.Payload)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

// Timeline returns all events for an issue, ordered by seq ascending.
func (s *Service) Timeline(ctx context.Context, issueKey string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, issue_key, seq, kind, actor, at, payload FROM events WHERE issue_key = ? ORDER BY seq ASC`,
		issueKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var payload string
		if err := rows.Scan(&e.ID, &e.IssueKey, &e.Seq, &e.Kind, &e.Actor, &e.At, &payload); err != nil {
			return nil, err
		}
		if payload != "" {
			_ = json.Unmarshal([]byte(payload), &e.Payload)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}
