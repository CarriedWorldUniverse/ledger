package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Organisation owns projects. One org can have many projects; a project
// belongs to exactly one org. The default "nexus" org wraps all existing
// projects pre-multi-tenancy.
type Organisation struct {
	Slug string
	Name string
}

// User is a ledger-authenticated identity. Kind is "human" or "ai".
type User struct {
	ID   string
	Kind string
}

// OrgMember links a user to an organisation with a role.
type OrgMember struct {
	Org    string
	UserID string
	Role   string // owner, admin, member, viewer
}

// CreateOrganisation inserts a new organisation.
func (s *Service) CreateOrganisation(ctx context.Context, slug, name string) (*Organisation, error) {
	if slug == "" || name == "" {
		return nil, fmt.Errorf("CreateOrganisation: slug and name required")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO organisations(slug, name) VALUES (?, ?)`, slug, name,
	); err != nil {
		return nil, fmt.Errorf("CreateOrganisation: %w", err)
	}
	return &Organisation{Slug: slug, Name: name}, nil
}

// GetOrganisation loads an org by slug.
func (s *Service) GetOrganisation(ctx context.Context, slug string) (*Organisation, error) {
	var o Organisation
	err := s.db.QueryRowContext(ctx,
		`SELECT slug, name FROM organisations WHERE slug = ?`, slug,
	).Scan(&o.Slug, &o.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("GetOrganisation: %s not found", slug)
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// CreateUser inserts a new user. Kind must be "human" or "ai".
func (s *Service) CreateUser(ctx context.Context, id, kind string) (*User, error) {
	if id == "" {
		return nil, fmt.Errorf("CreateUser: id required")
	}
	if kind != "human" && kind != "ai" {
		return nil, fmt.Errorf("CreateUser: kind must be 'human' or 'ai'")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users(id, kind) VALUES (?, ?)`, id, kind,
	); err != nil {
		return nil, fmt.Errorf("CreateUser: %w", err)
	}
	return &User{ID: id, Kind: kind}, nil
}

// GetUser loads a user by id.
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, kind FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("GetUser: %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// AddOrgMember adds or updates a user's membership in an org.
func (s *Service) AddOrgMember(ctx context.Context, org, userID, role string) error {
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return fmt.Errorf("AddOrgMember: invalid role %q", role)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO org_members(org, user_id, role) VALUES (?, ?, ?)
		 ON CONFLICT(org, user_id) DO UPDATE SET role = excluded.role`,
		org, userID, role,
	); err != nil {
		return fmt.Errorf("AddOrgMember: %w", err)
	}
	return nil
}

// ListOrganisations returns all orgs, newest first.
func (s *Service) ListOrganisations(ctx context.Context) ([]Organisation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT slug, name FROM organisations ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Organisation
	for rows.Next() {
		var o Organisation
		if err := rows.Scan(&o.Slug, &o.Name); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// UpdateOrganisation changes an org's name.
func (s *Service) UpdateOrganisation(ctx context.Context, slug, name string) error {
	if name == "" {
		return fmt.Errorf("UpdateOrganisation: name required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE organisations SET name = ? WHERE slug = ?`, name, slug,
	)
	if err != nil {
		return fmt.Errorf("UpdateOrganisation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("UpdateOrganisation: %s not found", slug)
	}
	return nil
}

// PurgeOrganisation deletes an org and cascades its projects/issues. Unlike
// DeleteOrganisation it is idempotent: an absent slug is a no-op (nil). Used by
// the cross-org wipe (NEX-402).
//
// Note: projects.organisation was added via ALTER TABLE ADD COLUMN so SQLite
// does not enforce a FK cascade from organisations→projects. This method
// therefore performs an explicit transactional delete: issues (for the org's
// projects) → projects → org.
func (s *Service) PurgeOrganisation(ctx context.Context, slug string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("PurgeOrganisation: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete all issues belonging to any project in this org.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM issues WHERE project IN (SELECT key FROM projects WHERE organisation = ?)`, slug,
	); err != nil {
		return fmt.Errorf("PurgeOrganisation: delete issues: %w", err)
	}
	// Delete all projects in this org.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM projects WHERE organisation = ?`, slug,
	); err != nil {
		return fmt.Errorf("PurgeOrganisation: delete projects: %w", err)
	}
	// Delete the org itself (idempotent: 0 rows affected is fine).
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM organisations WHERE slug = ?`, slug,
	); err != nil {
		return fmt.Errorf("PurgeOrganisation: delete org: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("PurgeOrganisation: commit: %w", err)
	}
	return nil
}

// DeleteOrganisation removes an org. Fails if projects still reference it.
func (s *Service) DeleteOrganisation(ctx context.Context, slug string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM organisations WHERE slug = ?`, slug,
	)
	if err != nil {
		return fmt.Errorf("DeleteOrganisation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("DeleteOrganisation: %s not found", slug)
	}
	return nil
}

// ListUsers returns all users, newest first.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind FROM users ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Kind); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUser changes a user's kind.
func (s *Service) UpdateUser(ctx context.Context, id, kind string) error {
	if kind != "human" && kind != "ai" {
		return fmt.Errorf("UpdateUser: kind must be 'human' or 'ai'")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET kind = ? WHERE id = ?`, kind, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateUser: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("UpdateUser: %s not found", id)
	}
	return nil
}

// DeleteUser removes a user. Fails if they hold org memberships.
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM users WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("DeleteUser: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("DeleteUser: %s not found", id)
	}
	return nil
}

// RemoveOrgMember removes a user from an org.
func (s *Service) RemoveOrgMember(ctx context.Context, org, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM org_members WHERE org = ? AND user_id = ?`, org, userID,
	)
	if err != nil {
		return fmt.Errorf("RemoveOrgMember: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("RemoveOrgMember: %s not a member of %s", userID, org)
	}
	return nil
}

// ListOrgMembers returns all members of an org.
func (s *Service) ListOrgMembers(ctx context.Context, org string) ([]OrgMember, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT org, user_id, role FROM org_members WHERE org = ? ORDER BY joined_at ASC`, org,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgMember
	for rows.Next() {
		var m OrgMember
		if err := rows.Scan(&m.Org, &m.UserID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
