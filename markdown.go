package ledger

import (
	"context"
	"fmt"
	"strings"
)

// MaterialiseMarkdown returns the aspect-facing markdown document for
// an issue: front-matter, sections for description / DoD / links /
// attachments / timeline.
func (s *Service) MaterialiseMarkdown(ctx context.Context, key string) (string, error) {
	issue, err := s.GetIssue(ctx, key)
	if err != nil {
		return "", err
	}
	timeline, err := s.Timeline(ctx, issue.Key)
	if err != nil {
		return "", err
	}
	watchers, _ := s.Watchers(ctx, issue.Key)

	var b strings.Builder
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "key: %s\n", issue.Key)
	fmt.Fprintf(&b, "project: %s\n", issue.Project)
	fmt.Fprintf(&b, "type: %s\n", issue.Type)
	fmt.Fprintf(&b, "status: %s\n", issue.Status)
	fmt.Fprintf(&b, "priority: %s\n", issue.Priority)
	if issue.AssigneeAspect != "" {
		fmt.Fprintf(&b, "assignee_aspect: %s\n", issue.AssigneeAspect)
	}
	if issue.AssigneeTeam != "" {
		fmt.Fprintf(&b, "assignee_team: %s\n", issue.AssigneeTeam)
	}
	fmt.Fprintf(&b, "reporter: %s\n", issue.Reporter)
	fmt.Fprintf(&b, "created: %s\n", issue.CreatedAt)
	if issue.ParentKey != "" {
		fmt.Fprintf(&b, "parent: %s\n", issue.ParentKey)
	}
	if len(watchers) > 0 {
		fmt.Fprintf(&b, "watchers: [%s]\n", strings.Join(watchers, ", "))
	}
	fmt.Fprintf(&b, "---\n\n")

	fmt.Fprintf(&b, "# %s\n\n", issue.Summary)
	fmt.Fprintf(&b, "## Description\n\n%s\n\n", issue.Description)
	fmt.Fprintf(&b, "## Definition of Done\n\n%s\n\n", issue.DefinitionOfDone)

	if len(timeline) > 0 {
		fmt.Fprintf(&b, "## Timeline\n\n")
		for _, e := range timeline {
			fmt.Fprintf(&b, "### %s — %s (%s)\n", e.At, e.Actor, e.Kind)
			switch e.Kind {
			case "comment":
				if body, ok := e.Payload["body"].(string); ok {
					fmt.Fprintf(&b, "%s\n\n", body)
				}
			case "transition":
				if from, ok := e.Payload["from"].(string); ok {
					fmt.Fprintf(&b, "%s → %s\n\n", from, e.Payload["to"])
				}
			case "field_change":
				if field, ok := e.Payload["field"].(string); ok {
					fmt.Fprintf(&b, "%s: %v\n\n", field, e.Payload["value"])
				}
			default:
				fmt.Fprintf(&b, "(event payload: %v)\n\n", e.Payload)
			}
		}
	}
	return b.String(), nil
}
