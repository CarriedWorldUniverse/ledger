package ledger

import (
	"context"
	"fmt"
	"strings"
)

// SearchFilter is the structured query shape. Empty fields = no filter.
type SearchFilter struct {
	Projects       []string
	Types          []string
	Statuses       []string
	Priorities     []string
	AssigneeAspect string
	AssigneeTeam   string
	Reporter       string
	ParentKey      string
	OrderBy        string // "priority" | "created" | "updated" (default: "updated")
	OrderDir       string // "asc" | "desc" (default: "desc")
	Limit          int    // default 50, max 200
}

// IssueRef is the lightweight projection returned from Search.
type IssueRef struct {
	Key            string
	Project        string
	Type           string
	Status         string
	Summary        string
	Priority       string
	AssigneeAspect string
	AssigneeTeam   string
	UpdatedAt      string
}

// Search runs the structured filter.
func (s *Service) Search(ctx context.Context, f SearchFilter) ([]IssueRef, error) {
	clauses := []string{}
	args := []any{}

	addIn := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		placeholders := strings.Repeat("?,", len(vals))
		placeholders = strings.TrimRight(placeholders, ",")
		clauses = append(clauses, fmt.Sprintf("%s IN (%s)", col, placeholders))
		for _, v := range vals {
			args = append(args, v)
		}
	}

	addIn("project", f.Projects)
	addIn("type", f.Types)
	addIn("status", f.Statuses)
	addIn("priority", f.Priorities)

	if f.AssigneeAspect != "" {
		clauses = append(clauses, "assignee_aspect = ?")
		args = append(args, f.AssigneeAspect)
	}
	if f.AssigneeTeam != "" {
		clauses = append(clauses, "assignee_team = ?")
		args = append(args, f.AssigneeTeam)
	}
	if f.Reporter != "" {
		clauses = append(clauses, "reporter = ?")
		args = append(args, f.Reporter)
	}
	if f.ParentKey != "" {
		clauses = append(clauses, "parent_key = ?")
		args = append(args, f.ParentKey)
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	orderBy := "updated_at"
	switch f.OrderBy {
	case "priority":
		orderBy = `CASE priority WHEN 'Highest' THEN 5 WHEN 'High' THEN 4 WHEN 'Medium' THEN 3 WHEN 'Low' THEN 2 WHEN 'Lowest' THEN 1 ELSE 0 END`
	case "created":
		orderBy = "created_at"
	case "updated", "":
		orderBy = "updated_at"
	}
	dir := "DESC"
	if strings.EqualFold(f.OrderDir, "asc") {
		dir = "ASC"
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	stmt := fmt.Sprintf(`
		SELECT key, project, type, status, summary, priority,
		       COALESCE(assignee_aspect, ''), COALESCE(assignee_team, ''), updated_at
		FROM issues
		%s
		ORDER BY %s %s
		LIMIT %d`,
		where, orderBy, dir, limit)

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IssueRef
	for rows.Next() {
		var r IssueRef
		if err := rows.Scan(&r.Key, &r.Project, &r.Type, &r.Status, &r.Summary, &r.Priority,
			&r.AssigneeAspect, &r.AssigneeTeam, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListMy returns issues assigned to the given aspect, either directly
// (assignee_aspect = aspect) or via a team membership (aspect in team_members
// where teams.name = assignee_team).
func (s *Service) ListMy(ctx context.Context, aspect string, teams []string) ([]IssueRef, error) {
	clauses := []string{"assignee_aspect = ?"}
	args := []any{aspect}
	if len(teams) > 0 {
		ph := strings.Repeat("?,", len(teams))
		ph = strings.TrimRight(ph, ",")
		clauses = append(clauses, fmt.Sprintf("assignee_team IN (%s)", ph))
		for _, t := range teams {
			args = append(args, t)
		}
	}

	stmt := fmt.Sprintf(`
			SELECT key, project, type, status, summary, priority,
			       COALESCE(assignee_aspect, ''), COALESCE(assignee_team, ''), updated_at
			FROM issues
			WHERE (%s) AND status NOT IN ('Done', 'Cancelled', 'Delivered')
			ORDER BY updated_at DESC
			LIMIT 100`,
		strings.Join(clauses, " OR "))

	return s.runRefQuery(ctx, stmt, args)
}

// ListReady returns the top of the ready pool for the caller: issues
// assigned to them (directly or via team) that are in a startable
// state ("To Do" or "In Progress" continuing). Ordered by priority
// then age.
func (s *Service) ListReady(ctx context.Context, aspect string, teams []string) ([]IssueRef, error) {
	clauses := []string{"assignee_aspect = ?"}
	args := []any{aspect}
	if len(teams) > 0 {
		ph := strings.Repeat("?,", len(teams))
		ph = strings.TrimRight(ph, ",")
		clauses = append(clauses, fmt.Sprintf("assignee_team IN (%s)", ph))
		for _, t := range teams {
			args = append(args, t)
		}
	}

	stmt := fmt.Sprintf(`
			SELECT key, project, type, status, summary, priority,
			       COALESCE(assignee_aspect, ''), COALESCE(assignee_team, ''), updated_at
			FROM issues
			WHERE (%s) AND status IN ('To Do', 'In Progress')
			ORDER BY
			  CASE priority WHEN 'Highest' THEN 5 WHEN 'High' THEN 4 WHEN 'Medium' THEN 3 WHEN 'Low' THEN 2 ELSE 1 END DESC,
			  created_at ASC
			LIMIT 50`,
		strings.Join(clauses, " OR "))

	return s.runRefQuery(ctx, stmt, args)
}

func (s *Service) runRefQuery(ctx context.Context, stmt string, args []any) ([]IssueRef, error) {
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IssueRef
	for rows.Next() {
		var r IssueRef
		if err := rows.Scan(&r.Key, &r.Project, &r.Type, &r.Status, &r.Summary, &r.Priority,
			&r.AssigneeAspect, &r.AssigneeTeam, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
