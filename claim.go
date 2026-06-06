package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrAlreadyClaimed is returned by ClaimIssue when the issue is already
// assigned to a DIFFERENT agent — the caller lost the race. Surfaced as
// HTTP 409 by the REST layer.
var ErrAlreadyClaimed = errors.New("ledger: issue already claimed by another agent")

// ErrNotClaimable is returned by ClaimIssue when the issue's type or
// current status can't be claimed (e.g. an Epic, or a Done/Cancelled
// ticket). Surfaced as HTTP 400.
var ErrNotClaimable = errors.New("ledger: issue is not in a claimable state")

// claimTargetStatus is the state a successful claim transitions the
// issue into — the story-like "in progress" state per workflow.go.
const claimTargetStatus = "In Progress"

// ClaimIssue atomically claims an issue for the calling agent: in a
// single DB transaction it verifies claimability, sets the assignee to
// the caller, transitions the issue to "In Progress", and appends a
// claim event. It returns the updated issue.
//
// This is the atomic replacement for the old two-call (assign, then
// transition) flow, which raced when two agents claimed concurrently.
// Concurrency rests on SQLite's single-writer model: the read-check and
// the conditional write happen inside one BeginTx, so a losing claimer
// re-reads the winner's assignee and gets ErrAlreadyClaimed.
//
// Semantics:
//   - unassigned & claimable type/state  → claim, transition, 200
//   - already assigned to the caller     → idempotent: ensure In Progress, 200
//   - assigned to a different agent       → ErrAlreadyClaimed (409)
//   - Epic or terminal/illegal state      → ErrNotClaimable (400)
//   - unknown key                         → ErrIssueNotFound (404)
//
// Tenancy: cross-org callers can't see the issue (same hide-existence
// pattern as GetIssue), enforced via callerCanAccessIssue before the tx.
func (s *Service) ClaimIssue(ctx context.Context, key, actor string) (*Issue, error) {
	if actor == "" {
		return nil, fmt.Errorf("ClaimIssue: actor required")
	}
	// Tenancy gate first (hide-existence on cross-org).
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var issueType, status, dod string
	var assignee sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT type, status, definition_of_done, assignee_aspect FROM issues WHERE key = ?`, key,
	).Scan(&issueType, &status, &dod, &assignee)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ClaimIssue: load %s: %w", key, err)
	}

	// Already claimed by someone else → lost the race.
	if assignee.Valid && assignee.String != "" && assignee.String != actor {
		return nil, ErrAlreadyClaimed
	}

	alreadyMine := assignee.Valid && assignee.String == actor

	// Determine whether a transition to "In Progress" is needed + legal.
	needTransition := status != claimTargetStatus
	if needTransition {
		if err := validateTransition(issueType, status, claimTargetStatus, dod); err != nil {
			// Illegal state machine move (e.g. Epic, Done, Cancelled) →
			// not claimable. validateTransition's error is descriptive but
			// we normalise to the sentinel so callers can branch to 400.
			return nil, fmt.Errorf("%w: %v", ErrNotClaimable, err)
		}
	}

	// Assign to the caller (no-op write if already mine — keeps the path
	// uniform and refreshes updated_at).
	if _, err := tx.ExecContext(ctx,
		`UPDATE issues SET assignee_aspect = ?, assignee_team = NULL, updated_at = datetime('now') WHERE key = ?`,
		actor, key,
	); err != nil {
		return nil, fmt.Errorf("ClaimIssue: assign: %w", err)
	}

	if needTransition {
		if _, err := tx.ExecContext(ctx,
			`UPDATE issues SET status = ?, updated_at = datetime('now') WHERE key = ?`,
			claimTargetStatus, key,
		); err != nil {
			return nil, fmt.Errorf("ClaimIssue: transition: %w", err)
		}
	}

	// One claim event records the whole atomic action.
	payload := map[string]any{
		"assignee":    actor,
		"from_status": status,
		"to_status":   claimTargetStatus,
		"reclaim":     alreadyMine,
	}
	if err := writeEvent(ctx, tx, key, "claim", actor, payload); err != nil {
		return nil, fmt.Errorf("ClaimIssue: event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Fire-and-forget operator notification, mirroring the other mutators.
	_ = s.notify.NotifyOperatorStream(ctx, fmt.Sprintf("%s claimed by %s", key, actor))

	return s.GetIssue(ctx, key)
}
