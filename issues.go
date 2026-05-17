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

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetIssue(ctx, key)
}

// GetIssue loads an issue by canonical key (or alias). Returns ErrIssueNotFound.
func (s *Service) GetIssue(ctx context.Context, key string) (*Issue, error) {
	got, err := s.fetchIssueByKey(ctx, key)
	if err == nil {
		return got, nil
	}
	if !errors.Is(err, ErrIssueNotFound) {
		return nil, err
	}
	// Fallback: resolve via alias.
	var newKey string
	err = s.db.QueryRowContext(ctx,
		`SELECT new_key FROM key_aliases WHERE old_key = ?`, key,
	).Scan(&newKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.fetchIssueByKey(ctx, newKey)
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
	return tx.Commit()
}

// AssignIssue sets assignee_aspect or assignee_team (exactly one, or
// both empty to clear). The actor is for the future events row.
func (s *Service) AssignIssue(ctx context.Context, key, aspect, team, actor string) error {
	if aspect != "" && team != "" {
		return fmt.Errorf("AssignIssue: set aspect OR team, not both")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE issues SET assignee_aspect = ?, assignee_team = ?, updated_at = datetime('now') WHERE key = ?`,
		nullable(aspect), nullable(team), key,
	)
	return err
}

// UpdateIssue applies a patch atomically.
func (s *Service) UpdateIssue(ctx context.Context, key string, patch UpdatePatch, actor string) error {
	sets := []string{}
	args := []any{}
	if patch.Summary != nil {
		sets = append(sets, "summary = ?")
		args = append(args, *patch.Summary)
	}
	if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *patch.Description)
	}
	if patch.DefinitionOfDone != nil {
		sets = append(sets, "definition_of_done = ?")
		args = append(args, *patch.DefinitionOfDone)
	}
	if patch.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *patch.Priority)
	}
	if patch.ParentKey != nil {
		sets = append(sets, "parent_key = ?")
		args = append(args, nullable(*patch.ParentKey))
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = datetime('now')")
	args = append(args, key)
	stmt := "UPDATE issues SET " + strings.Join(sets, ", ") + " WHERE key = ?"
	_, err := s.db.ExecContext(ctx, stmt, args...)
	return err
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
