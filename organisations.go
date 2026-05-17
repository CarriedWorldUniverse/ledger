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
