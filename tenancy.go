package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Hybrid multi-tenancy enforcement.
//
// The auth middleware (auth.go) injects *AuthClaims into the request
// context for HTTP callers. Service methods consult those claims via
// AuthFromContext and gate access accordingly. When no claims are
// present (in-process trusted callers — the broker calling the
// service directly, tests, the embedded Frame), the helpers in this
// file degrade to no-ops — backward compatible with the pre-tenancy
// service surface.
//
// "Hide existence" pattern: cross-org accesses return ErrProjectNotFound
// or ErrIssueNotFound rather than a permissions-denied error. The
// caller can't distinguish "doesn't exist" from "exists but not for
// you" — which is the right behaviour for a multi-tenant system that
// shouldn't leak the project keyspace of other orgs.

// callerCanAccessProject returns nil if the caller (per AuthFromContext)
// is allowed to act on the named project. Returns ErrProjectNotFound
// (NOT a permissions error) when the caller's org doesn't match, to
// avoid leaking project existence across org boundaries.
//
// When no auth claims are present in the context, returns nil — this
// is the hybrid promise: in-process callers (tests, broker, Frame)
// keep working without auth plumbing.
func (s *Service) callerCanAccessProject(ctx context.Context, projectKey string) error {
	claims := AuthFromContext(ctx)
	if claims == nil || claims.Org == "" {
		return nil
	}
	var projectOrg string
	err := s.db.QueryRowContext(ctx,
		`SELECT organisation FROM projects WHERE key = ?`, projectKey,
	).Scan(&projectOrg)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrProjectNotFound
	}
	if err != nil {
		return err
	}
	if projectOrg != claims.Org {
		return ErrProjectNotFound
	}
	return nil
}

// callerCanAccessIssue is the issue-level counterpart of
// callerCanAccessProject. Resolves the issue's project and delegates.
// Returns ErrIssueNotFound on cross-org access (NOT permissions).
func (s *Service) callerCanAccessIssue(ctx context.Context, issueKey string) error {
	claims := AuthFromContext(ctx)
	if claims == nil || claims.Org == "" {
		return nil
	}
	var projectKey string
	err := s.db.QueryRowContext(ctx,
		`SELECT project FROM issues WHERE key = ?`, issueKey,
	).Scan(&projectKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrIssueNotFound
	}
	if err != nil {
		return err
	}
	if err := s.callerCanAccessProject(ctx, projectKey); err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return ErrIssueNotFound
		}
		return err
	}
	return nil
}

// aspectInOrg returns true if the named aspect (user id) is a member
// of the given org per org_members. Used by AssignIssue to verify the
// proposed assignee is reachable inside the ticket's project's org —
// prevents assigning work to an aspect that has no membership in the
// org and therefore can't act on it.
func (s *Service) aspectInOrg(ctx context.Context, aspectName, orgSlug string) (bool, error) {
	if aspectName == "" || orgSlug == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_members WHERE org = ? AND user_id = ?`,
		orgSlug, aspectName,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ErrAssigneeNotInOrg signals a refused AssignIssue because the
// proposed assignee aspect isn't a member of the ticket's project's
// organisation. Distinct from ErrIssueNotFound (caller IS allowed
// to see the issue, but the assignment target isn't valid).
var ErrAssigneeNotInOrg = errors.New("ledger: assignee aspect is not a member of the project's organisation")

// ErrCrossProjectParent signals an UpdateIssue refusal because the
// proposed parent_key is an issue in a different project. v1 rule:
// parent links cannot cross project boundaries. ReassignProject is
// the path for moving issues across projects (and drops parent_key
// in the process — move.go).
var ErrCrossProjectParent = errors.New("ledger: parent issue must be in the same project")

// projectOfIssue is a small helper for the cross-project parent check.
// Returns ErrIssueNotFound when the issue key (or its alias) doesn't
// resolve.
func (s *Service) projectOfIssue(ctx context.Context, issueKey string) (string, error) {
	var p string
	err := s.db.QueryRowContext(ctx,
		`SELECT project FROM issues WHERE key = ?`, issueKey,
	).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		// Try alias resolution.
		var newKey string
		err2 := s.db.QueryRowContext(ctx,
			`SELECT new_key FROM key_aliases WHERE old_key = ?`, issueKey,
		).Scan(&newKey)
		if errors.Is(err2, sql.ErrNoRows) {
			return "", ErrIssueNotFound
		}
		if err2 != nil {
			return "", fmt.Errorf("projectOfIssue: alias resolution: %w", err2)
		}
		return s.projectOfIssue(ctx, newKey)
	}
	if err != nil {
		return "", err
	}
	return p, nil
}
