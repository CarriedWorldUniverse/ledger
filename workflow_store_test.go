package ledger

import (
	"context"
	"reflect"
	"testing"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/protobuf/proto"
)

func TestDefaultWorkflow_MatchesAllowedTransitions(t *testing.T) {
	wf := defaultWorkflow()

	gotTransitions := workflowTransitionsByFrom(wf)
	wantTransitions := map[string][]string{
		"To Do":          {"Ready to Start", "In Progress", "Cancelled"},
		"Ready to Start": {"In Progress", "Blocked", "To Do", "Cancelled"},
		"In Progress":    {"Blocked", "In Review", "Ready to Start", "Cancelled"},
		"Blocked":        {"In Progress", "Ready to Start", "Cancelled"},
		"In Review":      {"In Progress", "Done", "Cancelled"},
		"Done":           {},
		"Cancelled":      {},
		"Brief":          {"Sketch/Refined", "Cancelled"},
		"Sketch/Refined": {"In Development", "Brief", "Cancelled"},
		"In Development": {"Delivered", "Sketch/Refined", "Cancelled"},
		"Delivered":      {},
	}
	if !reflect.DeepEqual(gotTransitions, wantTransitions) {
		t.Fatalf("transitions mismatch\ngot:  %#v\nwant: %#v", gotTransitions, wantTransitions)
	}

	gotStates := workflowStatesByName(wf)
	wantStates := map[string]struct {
		category cwbv1.StatusCategory
		dodGate  bool
	}{
		"To Do":          {category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
		"Ready to Start": {category: cwbv1.StatusCategory_STATUS_CATEGORY_READY},
		"In Progress":    {category: cwbv1.StatusCategory_STATUS_CATEGORY_ACTIVE},
		"Blocked":        {category: cwbv1.StatusCategory_STATUS_CATEGORY_BLOCKED},
		"In Review":      {category: cwbv1.StatusCategory_STATUS_CATEGORY_IN_REVIEW},
		"Done":           {category: cwbv1.StatusCategory_STATUS_CATEGORY_DONE, dodGate: true},
		"Cancelled":      {category: cwbv1.StatusCategory_STATUS_CATEGORY_CANCELLED},
		"Brief":          {category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
		"Sketch/Refined": {category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
		"In Development": {category: cwbv1.StatusCategory_STATUS_CATEGORY_ACTIVE},
		"Delivered":      {category: cwbv1.StatusCategory_STATUS_CATEGORY_DONE, dodGate: true},
	}
	if !reflect.DeepEqual(gotStates, wantStates) {
		t.Fatalf("states mismatch\ngot:  %#v\nwant: %#v", gotStates, wantStates)
	}
}

func TestWorkflowForProject_DefaultFallback(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if err := svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := svc.workflowForProject(ctx, "NEX")
	if err != nil {
		t.Fatalf("workflowForProject: %v", err)
	}
	if !proto.Equal(got, defaultWorkflow()) {
		t.Fatalf("workflowForProject fallback did not return default workflow")
	}
}

func TestProjectWorkflow_SetGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if err := svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	want := &cwbv1.Workflow{
		States: []*cwbv1.WorkflowState{
			{Name: "Open", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
			{Name: "Closed", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DONE, DodGate: true},
		},
		Transitions: []*cwbv1.WorkflowTransition{
			{From: "Open", To: []string{"Closed"}},
			{From: "Closed", To: []string{}},
		},
	}
	if err := svc.SetProjectWorkflow(ctx, "NEX", want); err != nil {
		t.Fatalf("SetProjectWorkflow: %v", err)
	}

	got, err := svc.GetProjectWorkflow(ctx, "NEX")
	if err != nil {
		t.Fatalf("GetProjectWorkflow: %v", err)
	}
	if !proto.Equal(got, want) {
		t.Fatalf("GetProjectWorkflow mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

func workflowTransitionsByFrom(wf *cwbv1.Workflow) map[string][]string {
	out := map[string][]string{}
	for _, tr := range wf.GetTransitions() {
		out[tr.GetFrom()] = append([]string{}, tr.GetTo()...)
	}
	return out
}

func workflowStatesByName(wf *cwbv1.Workflow) map[string]struct {
	category cwbv1.StatusCategory
	dodGate  bool
} {
	out := map[string]struct {
		category cwbv1.StatusCategory
		dodGate  bool
	}{}
	for _, st := range wf.GetStates() {
		out[st.GetName()] = struct {
			category cwbv1.StatusCategory
			dodGate  bool
		}{
			category: st.GetCategory(),
			dodGate:  st.GetDodGate(),
		}
	}
	return out
}
