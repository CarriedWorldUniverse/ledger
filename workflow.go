package ledger

import (
	"fmt"
	"strings"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
)

// validateTransition checks the state machine + DoD gate. Returns nil
// if the transition is legal.
func validateTransition(wf *cwbv1.Workflow, fromStatus, toStatus, definitionOfDone string) error {
	states := workflowStates(wf)
	if _, ok := states[fromStatus]; !ok {
		return fmt.Errorf("unknown workflow state %q", fromStatus)
	}
	toState, ok := states[toStatus]
	if !ok {
		return fmt.Errorf("unknown workflow state %q", toStatus)
	}

	allowed := workflowTransitions(wf)[fromStatus]
	if !contains(allowed, toStatus) {
		return fmt.Errorf("transition %q → %q not allowed by workflow", fromStatus, toStatus)
	}
	if toState.GetDodGate() {
		if !dodComplete(definitionOfDone) {
			return fmt.Errorf("cannot transition to %q: definition of done has unticked items", toStatus)
		}
	}
	return nil
}

func workflowStates(wf *cwbv1.Workflow) map[string]*cwbv1.WorkflowState {
	out := map[string]*cwbv1.WorkflowState{}
	for _, st := range wf.GetStates() {
		out[st.GetName()] = st
	}
	return out
}

func workflowTransitions(wf *cwbv1.Workflow) map[string][]string {
	out := map[string][]string{}
	for _, tr := range wf.GetTransitions() {
		out[tr.GetFrom()] = tr.GetTo()
	}
	return out
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
