package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Issue is the materialised row form. Aspects don't see this directly —
// they see the materialised markdown document (see markdown.go).
type Issue struct {
	Key              string
	Project          string
	Seq              int
	Type             string
	Status           string
	Summary          string
	Description      string
	DefinitionOfDone string
	Priority         string
	PriorityLocked   bool
	AssigneeAspect   string // empty if unset
	AssigneeTeam     string // empty if unset
	Reporter         string
	ParentKey        string // empty if no parent
	CreatedAt        string
	UpdatedAt        string
}

// IssueDraft is the input to CreateIssue.
type IssueDraft struct {
	Project          string
	Type             string
	Summary          string
	Description      string
	DefinitionOfDone string
	Priority         string // default "Medium"
	Reporter         string
	ParentKey        string
	AssigneeAspect   string
	AssigneeTeam     string
}

// UpdatePatch holds optional field updates. Empty/nil fields = no change.
type UpdatePatch struct {
	Summary          *string
	Description      *string
	DefinitionOfDone *string
	Priority         *string
	ParentKey        *string
}

// ErrIssueNotFound is returned when no issue matches a key (or any alias).
var ErrIssueNotFound = errors.New("ledger: issue not found")

// CreateIssue allocates the next key in the project's sequence and
// inserts the row. Transitions to status "To Do" (or "Brief" for Epic).
func (s *Service) CreateIssue(ctx context.Context, d IssueDraft) (*Issue, error) {
	if err := validateDraft(d); err != nil {
		return nil, err
	}

	defaultStatus := initialStatus(d.Type)
	priority := d.Priority
	if priority == "" {
		priority = "Medium"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Atomically take + bump the sequence.
	var seq int
	err = tx.QueryRowContext(ctx,
		`UPDATE project_sequences SET next_seq = next_seq + 1 WHERE project = ? RETURNING next_seq - 1`,
		d.Project,
	).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("CreateIssue: project %q not found", d.Project)
	}
	if err != nil {
		return nil, fmt.Errorf("allocate seq: %w", err)
	}

	key := fmt.Sprintf("%s-%d", d.Project, seq)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO issues(key, project, seq, type, status, summary, description, definition_of_done,
			priority, reporter, parent_key, assignee_aspect, assignee_team)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key, d.Project, seq, d.Type, defaultStatus, d.Summary, d.Description, d.DefinitionOfDone,
		priority, d.Reporter, nullable(d.ParentKey), nullable(d.AssigneeAspect), nullable(d.AssigneeTeam),
	); err != nil {
		return nil, fmt.Errorf("insert issue: %w", err)
	}

	if err := writeEvent(ctx, tx, key, "create", d.Reporter, map[string]any{
		"type":    d.Type,
		"summary": d.Summary,
	}); err != nil {
		return nil, fmt.Errorf("write create event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetIssue(ctx, key)
}

// GetIssue loads an issue by canonical key (or alias). Returns
// ErrIssueNotFound if absent OR if the caller's auth context (per
// AuthFromContext) belongs to a different org from the issue's
// project — cross-org access looks identical to "not found" to
// avoid leaking issue keyspace across orgs.
func (s *Service) GetIssue(ctx context.Context, key string) (*Issue, error) {
	got, err := s.fetchIssueByKey(ctx, key)
	if errors.Is(err, ErrIssueNotFound) {
		// Fallback: resolve via alias.
		var newKey string
		aErr := s.db.QueryRowContext(ctx,
			`SELECT new_key FROM key_aliases WHERE old_key = ?`, key,
		).Scan(&newKey)
		if errors.Is(aErr, sql.ErrNoRows) {
			return nil, ErrIssueNotFound
		}
		if aErr != nil {
			return nil, aErr
		}
		got, err = s.fetchIssueByKey(ctx, newKey)
	}
	if err != nil {
		return nil, err
	}
	// Tenancy check — hides the issue from cross-org callers.
	if claims := AuthFromContext(ctx); claims != nil && claims.Org != "" {
		var projectOrg string
		if perr := s.db.QueryRowContext(ctx,
			`SELECT organisation FROM projects WHERE key = ?`, got.Project,
		).Scan(&projectOrg); perr != nil {
			return nil, perr
		}
		if projectOrg != claims.Org {
			return nil, ErrIssueNotFound
		}
	}
	return got, nil
}

// TransitionIssue moves an issue to a new status after validating the
// state machine + DoD gate. The actor is recorded for the timeline
// (events table; written by callers in Phase 2 — for now status-only).
func (s *Service) TransitionIssue(ctx context.Context, key, toStatus, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var issueType, fromStatus, dod string
	err = tx.QueryRowContext(ctx,
		`SELECT type, status, definition_of_done FROM issues WHERE key = ?`, key,
	).Scan(&issueType, &fromStatus, &dod)
	if err != nil {
		return fmt.Errorf("TransitionIssue: load %s: %w", key, err)
	}

	if err := validateTransition(issueType, fromStatus, toStatus, dod); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE issues SET status = ?, updated_at = datetime('now') WHERE key = ?`,
		toStatus, key,
	); err != nil {
		return err
	}
	if err := writeEvent(ctx, tx, key, "transition", actor, map[string]any{
		"from": fromStatus, "to": toStatus,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Fire-and-forget notifications after the transaction lands.
	_ = s.notify.NotifyOperatorStream(ctx,
		fmt.Sprintf("%s: %s → %s by %s", key, fromStatus, toStatus, actor))
	if toStatus == "Blocked" || fromStatus == "Blocked" {
		if watchers, _ := s.Watchers(ctx, key); len(watchers) > 0 {
			for _, w := range watchers {
				_ = s.notify.NotifyAspect(ctx, w,
					fmt.Sprintf("%s blocker %s → %s", key, fromStatus, toStatus))
			}
		}
	}
	return nil
}

// AssignIssue sets assignee_aspect or assignee_team (exactly one, or
// both empty to clear). When an aspect is assigned and the caller's
// auth context is set, the aspect must be a member of the ticket's
// project's organisation — refused with ErrAssigneeNotInOrg
// otherwise. Cross-org access to the issue is also enforced via
// the same hide-existence pattern as GetIssue.
func (s *Service) AssignIssue(ctx context.Context, key, aspect, team, actor string) error {
	if aspect != "" && team != "" {
		return fmt.Errorf("AssignIssue: set aspect OR team, not both")
	}

	// Tenancy gate (caller can see the issue) — same hide-existence
	// pattern as GetIssue so attackers can't probe assignment to
	// confirm an issue's existence.
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return err
	}

	// If assigning to an aspect AND we have an auth context, verify
	// the aspect is a member of the ticket's project's org. Skips
	// when no auth context (in-process trusted caller).
	if aspect != "" {
		if claims := AuthFromContext(ctx); claims != nil && claims.Org != "" {
			ok, err := s.aspectInOrg(ctx, aspect, claims.Org)
			if err != nil {
				return fmt.Errorf("AssignIssue: aspect-org check: %w", err)
			}
			if !ok {
				return ErrAssigneeNotInOrg
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE issues SET assignee_aspect = ?, assignee_team = ?, updated_at = datetime('now') WHERE key = ?`,
		nullable(aspect), nullable(team), key,
	); err != nil {
		return err
	}

	val := aspect
	if team != "" {
		val = team
	}
	if err := writeEvent(ctx, tx, key, "field_change", actor, map[string]any{
		"field": "assignee", "value": val,
	}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Fire-and-forget notifications after the transaction lands.
	if aspect != "" {
		_ = s.notify.NotifyAspect(ctx, aspect, fmt.Sprintf("Assigned: %s", key))
	}
	_ = s.notify.NotifyOperatorStream(ctx, fmt.Sprintf("%s assigned %s to %s", actor, key, val))
	return nil
}

// UpdateIssue applies a patch atomically. When the patch sets
// ParentKey to a non-empty value, the new parent must live in the
// same project as the issue being updated — cross-project parents
// are refused with ErrCrossProjectParent. Cross-project moves go
// through ReassignProject (move.go), which drops parent_key as part
// of the move.
//
// Tenancy: cross-org access to the issue is gated the same way as
// GetIssue — caller without rights sees ErrIssueNotFound.
func (s *Service) UpdateIssue(ctx context.Context, key string, patch UpdatePatch, actor string) error {
	if err := s.callerCanAccessIssue(ctx, key); err != nil {
		return err
	}

	// Cross-project parent guard. Runs before the transaction so
	// the validation cost is paid up front and we don't pollute the
	// audit trail with rolled-back events.
	if patch.ParentKey != nil && *patch.ParentKey != "" {
		issueProject, err := s.projectOfIssue(ctx, key)
		if err != nil {
			return fmt.Errorf("UpdateIssue: load issue project: %w", err)
		}
		parentProject, err := s.projectOfIssue(ctx, *patch.ParentKey)
		if errors.Is(err, ErrIssueNotFound) {
			return fmt.Errorf("UpdateIssue: parent %q not found", *patch.ParentKey)
		}
		if err != nil {
			return fmt.Errorf("UpdateIssue: load parent project: %w", err)
		}
		if parentProject != issueProject {
			return ErrCrossProjectParent
		}
	}

	sets := []string{}
	args := []any{}
	events := []struct{ field, value string }{}
	if patch.Summary != nil {
		sets = append(sets, "summary = ?")
		args = append(args, *patch.Summary)
		events = append(events, struct{ field, value string }{"summary", *patch.Summary})
	}
	if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *patch.Description)
		events = append(events, struct{ field, value string }{"description", *patch.Description})
	}
	if patch.DefinitionOfDone != nil {
		sets = append(sets, "definition_of_done = ?")
		args = append(args, *patch.DefinitionOfDone)
		events = append(events, struct{ field, value string }{"definition_of_done", *patch.DefinitionOfDone})
	}
	if patch.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *patch.Priority)
		events = append(events, struct{ field, value string }{"priority", *patch.Priority})
	}
	if patch.ParentKey != nil {
		sets = append(sets, "parent_key = ?")
		args = append(args, nullable(*patch.ParentKey))
		events = append(events, struct{ field, value string }{"parent_key", *patch.ParentKey})
	}
	if len(sets) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sets = append(sets, "updated_at = datetime('now')")
	args = append(args, key)
	stmt := "UPDATE issues SET " + strings.Join(sets, ", ") + " WHERE key = ?"
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return err
	}

	for _, ev := range events {
		if err := writeEvent(ctx, tx, key, "field_change", actor, map[string]any{
			"field": ev.field, "value": ev.value,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Service) fetchIssueByKey(ctx context.Context, key string) (*Issue, error) {
	var i Issue
	var assigneeAspect, assigneeTeam, parentKey sql.NullString
	var priorityLocked int
	err := s.db.QueryRowContext(ctx, `
		SELECT key, project, seq, type, status, summary, description, definition_of_done,
		       priority, priority_locked, assignee_aspect, assignee_team, reporter,
		       parent_key, created_at, updated_at
		FROM issues WHERE key = ?`, key,
	).Scan(&i.Key, &i.Project, &i.Seq, &i.Type, &i.Status, &i.Summary, &i.Description,
		&i.DefinitionOfDone, &i.Priority, &priorityLocked, &assigneeAspect, &assigneeTeam,
		&i.Reporter, &parentKey, &i.CreatedAt, &i.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	i.AssigneeAspect = assigneeAspect.String
	i.AssigneeTeam = assigneeTeam.String
	i.ParentKey = parentKey.String
	i.PriorityLocked = priorityLocked != 0
	return &i, nil
}

func validateDraft(d IssueDraft) error {
	if d.Project == "" {
		return fmt.Errorf("CreateIssue: Project required")
	}
	if !validType(d.Type) {
		return fmt.Errorf("CreateIssue: Type %q invalid (want Epic|Story|Task|Subtask|Bug)", d.Type)
	}
	if strings.TrimSpace(d.Summary) == "" {
		return fmt.Errorf("CreateIssue: Summary required")
	}
	if strings.TrimSpace(d.DefinitionOfDone) == "" {
		return fmt.Errorf("CreateIssue: DefinitionOfDone required (minimum one checklist item)")
	}
	if d.Reporter == "" {
		return fmt.Errorf("CreateIssue: Reporter required")
	}
	if d.AssigneeAspect != "" && d.AssigneeTeam != "" {
		return fmt.Errorf("CreateIssue: set either AssigneeAspect OR AssigneeTeam, not both")
	}
	return nil
}

func validType(t string) bool {
	switch t {
	case "Epic", "Story", "Task", "Subtask", "Bug":
		return true
	}
	return false
}

func initialStatus(t string) string {
	if t == "Epic" {
		return "Brief"
	}
	return "To Do"
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
