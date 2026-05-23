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

// CreateProject inserts the project and seeds its sequence row.
// Both happen in a single transaction. Defaults Organisation to "nexus"
// if empty.
func (s *Service) CreateProject(ctx context.Context, p Project) error {
	if p.Key == "" || p.Name == "" {
		return fmt.Errorf("CreateProject: Key and Name required")
	}
	org := p.Organisation
	if org == "" {
		org = "nexus"
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
