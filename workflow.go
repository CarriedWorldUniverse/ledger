package ledger

import (
	"fmt"
	"strings"
)

// allowedTransitions maps {issueType: {fromStatus: [allowedToStatuses]}}.
// Cancelled is reachable from any non-terminal state for every type.
var allowedTransitions = map[string]map[string][]string{
	"Epic": {
		"Brief":          {"Sketch/Refined", "Cancelled"},
		"Sketch/Refined": {"In Development", "Brief", "Cancelled"},
		"In Development": {"Delivered", "Sketch/Refined", "Cancelled"},
		"Delivered":      {}, // terminal
		"Cancelled":      {}, // terminal
	},
	"Story":   storyLikeTransitions(),
	"Task":    storyLikeTransitions(),
	"Bug":     storyLikeTransitions(),
	"Subtask": storyLikeTransitions(),
}

func storyLikeTransitions() map[string][]string {
	return map[string][]string{
		"To Do":       {"In Progress", "Cancelled"},
		"In Progress": {"Blocked", "In Review", "Cancelled"},
		"Blocked":     {"In Progress", "Cancelled"},
		"In Review":   {"In Progress", "Done", "Cancelled"},
		"Done":        {},
		"Cancelled":   {},
	}
}

// terminalStates is the set of statuses that gate DoD enforcement.
var terminalStates = map[string]bool{
	"Done":      true,
	"Delivered": true,
}

// validateTransition checks the state machine + DoD gate. Returns nil
// if the transition is legal.
func validateTransition(issueType, fromStatus, toStatus, definitionOfDone string) error {
	rules, ok := allowedTransitions[issueType]
	if !ok {
		return fmt.Errorf("unknown issue type %q", issueType)
	}
	allowed, ok := rules[fromStatus]
	if !ok {
		return fmt.Errorf("no transitions defined from %q for %s", fromStatus, issueType)
	}
	if !contains(allowed, toStatus) {
		return fmt.Errorf("transition %q → %q not allowed for %s", fromStatus, toStatus, issueType)
	}
	if terminalStates[toStatus] {
		if !dodComplete(definitionOfDone) {
			return fmt.Errorf("cannot transition to %q: definition of done has unticked items", toStatus)
		}
	}
	return nil
}

// dodComplete returns true iff the DoD markdown contains at least one
// ticked checklist item AND no unticked ones.
func dodComplete(dod string) bool {
	lines := strings.Split(dod, "\n")
	ticked := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") {
			return false // any unticked item disqualifies
		}
		if strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]") {
			ticked++
		}
	}
	return ticked > 0
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
