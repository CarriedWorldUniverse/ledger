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
