package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ErrInvalidWorkflow is returned when a caller attempts to persist an
// unusable workflow definition.
var ErrInvalidWorkflow = errors.New("ledger: invalid workflow")

func defaultWorkflow() *cwbv1.Workflow {
	return &cwbv1.Workflow{
		States: []*cwbv1.WorkflowState{
			{Name: "To Do", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
			{Name: "Ready to Start", Category: cwbv1.StatusCategory_STATUS_CATEGORY_READY},
			{Name: "In Progress", Category: cwbv1.StatusCategory_STATUS_CATEGORY_ACTIVE},
			{Name: "Blocked", Category: cwbv1.StatusCategory_STATUS_CATEGORY_BLOCKED},
			{Name: "In Review", Category: cwbv1.StatusCategory_STATUS_CATEGORY_IN_REVIEW},
			{Name: "Done", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DONE, DodGate: true},
			{Name: "Cancelled", Category: cwbv1.StatusCategory_STATUS_CATEGORY_CANCELLED},
			{Name: "Brief", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
			{Name: "Sketch/Refined", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
			{Name: "In Development", Category: cwbv1.StatusCategory_STATUS_CATEGORY_ACTIVE},
			{Name: "Delivered", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DONE, DodGate: true},
		},
		Transitions: []*cwbv1.WorkflowTransition{
			{From: "To Do", To: []string{"Ready to Start", "In Progress", "Cancelled"}},
			{From: "Ready to Start", To: []string{"In Progress", "Blocked", "To Do", "Cancelled"}},
			{From: "In Progress", To: []string{"Blocked", "In Review", "Ready to Start", "Cancelled"}},
			{From: "Blocked", To: []string{"In Progress", "Ready to Start", "Cancelled"}},
			{From: "In Review", To: []string{"In Progress", "Done", "Cancelled"}},
			{From: "Done", To: []string{}},
			{From: "Cancelled", To: []string{}},
			{From: "Brief", To: []string{"Sketch/Refined", "Cancelled"}},
			{From: "Sketch/Refined", To: []string{"In Development", "Brief", "Cancelled"}},
			{From: "In Development", To: []string{"Delivered", "Sketch/Refined", "Cancelled"}},
			{From: "Delivered", To: []string{}},
		},
	}
}

func validateWorkflow(wf *cwbv1.Workflow) error {
	if wf == nil {
		return fmt.Errorf("%w: workflow required", ErrInvalidWorkflow)
	}
	if len(wf.GetStates()) == 0 {
		return fmt.Errorf("%w: at least one state required", ErrInvalidWorkflow)
	}
	if len(wf.GetTransitions()) == 0 {
		return fmt.Errorf("%w: at least one transition required", ErrInvalidWorkflow)
	}
	for _, st := range wf.GetStates() {
		if strings.TrimSpace(st.GetName()) == "" {
			return fmt.Errorf("%w: state name required", ErrInvalidWorkflow)
		}
	}
	for _, tr := range wf.GetTransitions() {
		if strings.TrimSpace(tr.GetFrom()) == "" {
			return fmt.Errorf("%w: transition from required", ErrInvalidWorkflow)
		}
	}
	return nil
}

// workflowForProject loads a project's stored workflow. Projects without a
// stored workflow use the in-code default seed.
func (s *Service) workflowForProject(ctx context.Context, project string) (*cwbv1.Workflow, error) {
	if _, err := s.GetProject(ctx, project); err != nil {
		return nil, err
	}

	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT workflow_json FROM workflows WHERE project = ?`,
		project,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultWorkflow(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("workflowForProject: load %s: %w", project, err)
	}

	wf := &cwbv1.Workflow{}
	if err := protojson.Unmarshal([]byte(raw), wf); err != nil {
		return nil, fmt.Errorf("workflowForProject: decode %s: %w", project, err)
	}
	return wf, nil
}

func (s *Service) SetProjectWorkflow(ctx context.Context, project string, wf *cwbv1.Workflow) error {
	if strings.TrimSpace(project) == "" {
		return fmt.Errorf("%w: project required", ErrProjectNotFound)
	}
	if _, err := s.GetProject(ctx, project); err != nil {
		return err
	}
	if err := validateWorkflow(wf); err != nil {
		return err
	}

	raw, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(wf)
	if err != nil {
		return fmt.Errorf("SetProjectWorkflow: encode %s: %w", project, err)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO workflows(project, workflow_json, updated_at)
VALUES (?, ?, datetime('now'))
ON CONFLICT(project) DO UPDATE SET
  workflow_json = excluded.workflow_json,
  updated_at = datetime('now')`,
		project, string(raw),
	); err != nil {
		return fmt.Errorf("SetProjectWorkflow: save %s: %w", project, err)
	}
	return nil
}

func (s *Service) GetProjectWorkflow(ctx context.Context, project string) (*cwbv1.Workflow, error) {
	wf, err := s.workflowForProject(ctx, project)
	if err != nil {
		return nil, err
	}
	return proto.Clone(wf).(*cwbv1.Workflow), nil
}
