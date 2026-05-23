package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Project is a top-level container for issues with its own key sequence.
// Every project belongs to exactly one organisation.
type Project struct {
	Key          string
	Organisation string
	Name         string
	Description  string
	DefaultTeam  string // nullable; empty string = none
	Archived     bool
}

// ErrProjectNotFound is returned by GetProject when no row matches.
var ErrProjectNotFound = errors.New("ledger: project not found")

// ErrOrgNotFound signals an attempt to create / reference a project
// in an organisation that doesn't exist. The schema can't enforce
// the projects.organisation → organisations.slug FK (SQLite ALTER
// TABLE ADD COLUMN doesn't accept FK constraints — see schema.sql),
// so the integrity check lives at the application layer here.
var ErrOrgNotFound = errors.New("ledger: organisation not found")

// ErrCallerNotInOrg signals a refused CreateProject because the
// authenticated caller (per AuthFromContext) isn't a member of the
// target organisation. Hybrid: when no auth context is present
// (in-process trusted callers), this check is skipped — see the
// tenancy.go comment for the model.
var ErrCallerNotInOrg = errors.New("ledger: caller is not a member of the target organisation")

// CreateProject inserts the project and seeds its sequence row.
// Both happen in a single transaction. Defaults Organisation to
// "nexus" when empty.
//
// Integrity checks (run before the transaction):
//
//   1. Organisation must exist (ErrOrgNotFound otherwise). Schema
//      can't FK-enforce; app-layer guard prevents orphan projects
//      pointing at deleted / never-existing orgs.
//   2. When auth context is present, the caller must be a member of
//      the target org (ErrCallerNotInOrg otherwise). Skipped when
//      no claims (in-process trusted caller) — hybrid promise.
func (s *Service) CreateProject(ctx context.Context, p Project) error {
	if p.Key == "" || p.Name == "" {
		return fmt.Errorf("CreateProject: Key and Name required")
	}
	org := p.Organisation
	if org == "" {
		org = "nexus"
	}

	// #14: app-layer FK enforcement — organisation must exist.
	var orgExists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM organisations WHERE slug = ?)`, org,
	).Scan(&orgExists); err != nil {
		return fmt.Errorf("CreateProject: check org existence: %w", err)
	}
	if orgExists == 0 {
		return fmt.Errorf("%w: %q", ErrOrgNotFound, org)
	}

	// #13: caller-must-be-org-member when auth context present.
	if claims := AuthFromContext(ctx); claims != nil && claims.Sub != "" {
		var isMember int
		if err := s.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM org_members WHERE org = ? AND user_id = ?)`,
			org, claims.Sub,
		).Scan(&isMember); err != nil {
			return fmt.Errorf("CreateProject: check membership: %w", err)
		}
		if isMember == 0 {
			return fmt.Errorf("%w: caller=%q org=%q", ErrCallerNotInOrg, claims.Sub, org)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	defaultTeam := sql.NullString{Valid: p.DefaultTeam != "", String: p.DefaultTeam}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects(key, organisation, name, description, default_team, archived) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Key, org, p.Name, p.Description, defaultTeam, boolToInt(p.Archived),
	); err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO project_sequences(project, next_seq) VALUES (?, 1)`,
		p.Key,
	); err != nil {
		return fmt.Errorf("insert project_sequence: %w", err)
	}
	return tx.Commit()
}

// GetProject loads a project by key. Returns ErrProjectNotFound if
// the project is absent OR if the caller's auth context (per
// AuthFromContext) belongs to a different org — cross-org access
// looks identical to "not found" to avoid leaking project keyspace
// across orgs. Callers without an auth context (in-process trusted
// callers) bypass the org check.
func (s *Service) GetProject(ctx context.Context, key string) (*Project, error) {
	var p Project
	var defaultTeam sql.NullString
	var archived int
	err := s.db.QueryRowContext(ctx,
		`SELECT key, organisation, name, description, default_team, archived FROM projects WHERE key = ?`,
		key,
	).Scan(&p.Key, &p.Organisation, &p.Name, &p.Description, &defaultTeam, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	p.DefaultTeam = defaultTeam.String
	p.Archived = archived != 0
	// Tenancy check — hides the project from cross-org callers.
	if claims := AuthFromContext(ctx); claims != nil && claims.Org != "" && claims.Org != p.Organisation {
		return nil, ErrProjectNotFound
	}
	return &p, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpdateProjectPatch holds optional project field updates. Empty/nil
// fields = no change. Key + Organisation are immutable — moving a
// project to a different org isn't a v1 operation and would require
// a multi-step lifecycle (membership re-check, cross-org link audit).
//
// DefaultTeam is intentionally NOT in the patch v1 — proper
// validation depends on teams.project (lands in #32). Follow-up PR
// adds DefaultTeam support once #32 is on main.
type UpdateProjectPatch struct {
	Name        *string
	Description *string
}

// ListProjects returns projects, optionally filtered to the caller's
// org when an auth context is present (cross-org listing would leak
// the projects keyspace). Archived projects are excluded unless
// includeArchived is true.
//
// Result is sorted by project key for deterministic output.
func (s *Service) ListProjects(ctx context.Context, includeArchived bool) ([]Project, error) {
	q := `SELECT key, organisation, name, description, default_team, archived FROM projects`
	args := []any{}
	conds := []string{}

	if claims := AuthFromContext(ctx); claims != nil && claims.Org != "" {
		conds = append(conds, "organisation = ?")
		args = append(args, claims.Org)
	}
	if !includeArchived {
		conds = append(conds, "archived = 0")
	}
	if len(conds) > 0 {
		q += " WHERE " + conds[0]
		for i := 1; i < len(conds); i++ {
			q += " AND " + conds[i]
		}
	}
	q += " ORDER BY key"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListProjects: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		var defaultTeam sql.NullString
		var archived int
		if err := rows.Scan(&p.Key, &p.Organisation, &p.Name, &p.Description, &defaultTeam, &archived); err != nil {
			return nil, err
		}
		p.DefaultTeam = defaultTeam.String
		p.Archived = archived != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

// UpdateProject applies a patch atomically. Cross-org callers see
// ErrProjectNotFound (hide-existence via GetProject's check).
func (s *Service) UpdateProject(ctx context.Context, key string, patch UpdateProjectPatch) error {
	// Tenancy + existence gate. GetProject hides cross-org as not-
	// found so the caller can't probe.
	if _, err := s.GetProject(ctx, key); err != nil {
		return err
	}

	sets := []string{}
	args := []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *patch.Description)
	}
	if len(sets) == 0 {
		return nil
	}

	args = append(args, key)
	stmt := "UPDATE projects SET " + sets[0]
	for i := 1; i < len(sets); i++ {
		stmt += ", " + sets[i]
	}
	stmt += " WHERE key = ?"
	if _, err := s.db.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("UpdateProject: %w", err)
	}
	return nil
}

// ArchiveProject marks the project as archived. Existing issues
// stay accessible (no cascade); the project just disappears from
// default ListProjects results.
func (s *Service) ArchiveProject(ctx context.Context, key string) error {
	return s.setArchived(ctx, key, true)
}

// UnarchiveProject flips an archived project back to active.
func (s *Service) UnarchiveProject(ctx context.Context, key string) error {
	return s.setArchived(ctx, key, false)
}

func (s *Service) setArchived(ctx context.Context, key string, archived bool) error {
	if _, err := s.GetProject(ctx, key); err != nil {
		return err // tenancy + existence gate
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE projects SET archived = ? WHERE key = ?`,
		boolToInt(archived), key,
	); err != nil {
		return fmt.Errorf("setArchived: %w", err)
	}
	return nil
}
