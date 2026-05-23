package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Team is an operator-curated group of aspects, scoped to one
// project. Each team belongs to exactly one project; same team name
// in different projects is NOT supported in v1 (teams.name remains
// globally unique PK). Operator uses scoped naming when needed
// (e.g. "nex-backend" vs "oss-backend").
type Team struct {
	Name        string
	Project     string
	Description string
	CreatedAt   string
	Members     []string // populated by GetTeam; empty on List/Create return
}

var (
	// ErrTeamNotFound is returned when a team lookup misses.
	ErrTeamNotFound = errors.New("ledger: team not found")

	// ErrTeamProjectMismatch signals an attempt to assign a team to
	// a ticket in a different project from the team's project. Per
	// the orchestration spec, team assignment is project-scoped:
	// you can't assign @backend-team (project=NEX) to OSS-42.
	ErrTeamProjectMismatch = errors.New("ledger: team belongs to a different project than the ticket")

	// ErrTeamNotEmpty is returned by DeleteTeam when the team still
	// has members (or could be assigned to live issues — checked
	// separately). Forces an explicit drain step.
	ErrTeamNotEmpty = errors.New("ledger: team has members; remove them before deleting")
)

// CreateTeam adds a team scoped to the given project. The project
// must exist; rejected with ErrProjectNotFound otherwise. The team
// name must be globally unique (PK constraint); SQLite's UNIQUE
// violation surfaces as an error wrapping the raw SQLite text.
//
// Tenancy: when an auth context is present, the caller must be able
// to access the project (callerCanAccessProject) — cross-org callers
// see ErrProjectNotFound (hide-existence).
func (s *Service) CreateTeam(ctx context.Context, name, project, description string) error {
	if name == "" || project == "" {
		return fmt.Errorf("CreateTeam: name and project required")
	}
	// App-layer FK: teams.project has no SQL FK (ALTER TABLE ADD
	// COLUMN can't add REFERENCES — same trap as projects.organisation).
	// Explicit existence check prevents orphan team rows.
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM projects WHERE key = ?)`, project,
	).Scan(&exists); err != nil {
		return fmt.Errorf("CreateTeam: check project: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("%w: %q", ErrProjectNotFound, project)
	}

	if err := s.callerCanAccessProject(ctx, project); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO teams(name, project, description) VALUES (?, ?, ?)`,
		name, project, description,
	); err != nil {
		return fmt.Errorf("CreateTeam: %w", err)
	}
	return tx.Commit()
}

// GetTeam returns a team by name, with its member list populated.
// Returns ErrTeamNotFound on miss. Tenancy: caller must be able to
// access the team's project.
func (s *Service) GetTeam(ctx context.Context, name string) (*Team, error) {
	var t Team
	err := s.db.QueryRowContext(ctx,
		`SELECT name, project, description, created_at FROM teams WHERE name = ?`,
		name,
	).Scan(&t.Name, &t.Project, &t.Description, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTeamNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.callerCanAccessProject(ctx, t.Project); err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT aspect FROM team_members WHERE team = ? ORDER BY aspect`, name,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var aspect string
		if err := rows.Scan(&aspect); err != nil {
			return nil, err
		}
		t.Members = append(t.Members, aspect)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &t, nil
}

// ListTeams returns teams in the given project, sorted by name.
// When project is empty AND no auth context is present, returns all
// teams across all projects (in-process callers). With auth context,
// project must be specified (cross-org listing would leak).
func (s *Service) ListTeams(ctx context.Context, project string) ([]Team, error) {
	if project != "" {
		if err := s.callerCanAccessProject(ctx, project); err != nil {
			return nil, err
		}
	} else {
		// No project filter requested. If caller is authed, refuse
		// rather than silently leak across orgs.
		if claims := AuthFromContext(ctx); claims != nil && claims.Org != "" {
			return nil, fmt.Errorf("ListTeams: project required when auth context is present")
		}
	}

	q := `SELECT name, project, description, created_at FROM teams`
	args := []any{}
	if project != "" {
		q += ` WHERE project = ?`
		args = append(args, project)
	}
	q += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.Name, &t.Project, &t.Description, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

// AddTeamMember inserts an aspect into a team. Idempotent (INSERT
// OR IGNORE) — adding the same aspect twice is a no-op without
// error. Tenancy gate via the team's project.
func (s *Service) AddTeamMember(ctx context.Context, teamName, aspect string) error {
	if teamName == "" || aspect == "" {
		return fmt.Errorf("AddTeamMember: team and aspect required")
	}
	t, err := s.GetTeam(ctx, teamName)
	if err != nil {
		return err
	}
	_ = t // team-existence + tenancy already verified by GetTeam

	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO team_members(team, aspect) VALUES (?, ?)`,
		teamName, aspect,
	); err != nil {
		return fmt.Errorf("AddTeamMember: %w", err)
	}
	return nil
}

// RemoveTeamMember deletes an aspect from a team. Idempotent —
// removing a non-member is a no-op.
func (s *Service) RemoveTeamMember(ctx context.Context, teamName, aspect string) error {
	if teamName == "" || aspect == "" {
		return fmt.Errorf("RemoveTeamMember: team and aspect required")
	}
	if _, err := s.GetTeam(ctx, teamName); err != nil {
		return err // tenancy + existence gate
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM team_members WHERE team = ? AND aspect = ?`,
		teamName, aspect,
	); err != nil {
		return fmt.Errorf("RemoveTeamMember: %w", err)
	}
	return nil
}

// DeleteTeam removes an empty team. Returns ErrTeamNotEmpty if it
// still has members — operator must explicitly drain first. This
// is a guard against accidental deletion that would orphan
// assignee_team references; the issues FK uses ON DELETE SET NULL
// but losing the team's identity silently is rarely what's intended.
func (s *Service) DeleteTeam(ctx context.Context, name string) error {
	t, err := s.GetTeam(ctx, name)
	if err != nil {
		return err
	}
	if len(t.Members) > 0 {
		return ErrTeamNotEmpty
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE name = ?`, name); err != nil {
		return fmt.Errorf("DeleteTeam: %w", err)
	}
	return nil
}

// teamProject returns the project for the named team. Internal
// helper used by AssignIssue to enforce the team-must-be-in-same-
// project-as-ticket rule. Returns ErrTeamNotFound on miss.
func (s *Service) teamProject(ctx context.Context, teamName string) (string, error) {
	var p string
	err := s.db.QueryRowContext(ctx,
		`SELECT project FROM teams WHERE name = ?`, teamName,
	).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTeamNotFound
	}
	if err != nil {
		return "", err
	}
	return p, nil
}
