package ledger

import (
	"encoding/json"
	"errors"
	"strings"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// toStatus maps ledger error sentinels to gRPC status codes.
//
//	ErrIssueNotFound / ErrProjectNotFound / ErrOrgNotFound / ErrUserNotFound → codes.NotFound
//	ErrAlreadyClaimed                                                          → codes.Aborted
//	ErrNotClaimable / ErrInvalidLinkType / ErrSelfLink /
//	  ErrCrossProjectParent / ErrAssigneeNotInOrg / DoD-gate error             → codes.InvalidArgument
//	ErrCallerNotInOrg                                                          → codes.PermissionDenied
//	everything else                                                            → codes.Internal
func toStatus(err error) error {
	switch {
	case errors.Is(err, ErrIssueNotFound),
		errors.Is(err, ErrProjectNotFound),
		errors.Is(err, ErrOrgNotFound),
		errors.Is(err, ErrUserNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrAlreadyClaimed):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, ErrNotClaimable),
		errors.Is(err, ErrInvalidWorkflow),
		errors.Is(err, ErrInvalidLinkType),
		errors.Is(err, ErrSelfLink),
		errors.Is(err, ErrCrossProjectParent),
		errors.Is(err, ErrAssigneeNotInOrg),
		isDoDGateErr(err):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrCallerNotInOrg):
		return status.Error(codes.PermissionDenied, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// isDoDGateErr returns true for the validateTransition error that fires when an
// issue is transitioned to a terminal state with unticked DoD items. This error
// is a plain fmt.Errorf (no sentinel), so we recognise it by its fixed prefix.
func isDoDGateErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "definition of done has unticked items")
}

// toProtoIssue converts an internal Issue to the proto wire type.
func toProtoIssue(i *Issue) *cwbv1.Issue {
	if i == nil {
		return nil
	}
	return &cwbv1.Issue{
		Key:              i.Key,
		Project:          i.Project,
		Seq:              int32(i.Seq),
		Type:             i.Type,
		Status:           i.Status,
		Summary:          i.Summary,
		Description:      i.Description,
		DefinitionOfDone: i.DefinitionOfDone,
		Priority:         i.Priority,
		PriorityLocked:   i.PriorityLocked,
		AssigneeAspect:   i.AssigneeAspect,
		AssigneeTeam:     i.AssigneeTeam,
		Reporter:         i.Reporter,
		ParentKey:        i.ParentKey,
		ExternalRefs:     toProtoExternalRefs(i.ExternalRefs),
		Skills:           i.Skills,
		CreatedAt:        i.CreatedAt,
		UpdatedAt:        i.UpdatedAt,
	}
}

func toProtoIssueWithCategory(i *Issue, category cwbv1.StatusCategory) *cwbv1.Issue {
	out := toProtoIssue(i)
	if out != nil {
		out.Category = category
	}
	return out
}

// toProtoIssueRef converts an internal IssueRef to the proto wire type.
func toProtoIssueRef(r IssueRef) *cwbv1.IssueRef {
	return &cwbv1.IssueRef{
		Key:            r.Key,
		Project:        r.Project,
		Type:           r.Type,
		Status:         r.Status,
		Summary:        r.Summary,
		Priority:       r.Priority,
		AssigneeAspect: r.AssigneeAspect,
		AssigneeTeam:   r.AssigneeTeam,
		UpdatedAt:      r.UpdatedAt,
	}
}

func toProtoIssueRefWithCategory(r IssueRef, category cwbv1.StatusCategory) *cwbv1.IssueRef {
	out := toProtoIssueRef(r)
	out.Category = category
	return out
}

func statusCategoryForWorkflow(wf *cwbv1.Workflow, status string) cwbv1.StatusCategory {
	for _, st := range wf.GetStates() {
		if st.GetName() == status {
			return st.GetCategory()
		}
	}
	return cwbv1.StatusCategory_STATUS_CATEGORY_UNSPECIFIED
}

// toProtoExternalRefs converts a slice of ExternalRef.
func toProtoExternalRefs(refs []ExternalRef) []*cwbv1.ExternalRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*cwbv1.ExternalRef, len(refs))
	for i, r := range refs {
		out[i] = &cwbv1.ExternalRef{
			Tracker:     r.Tracker,
			Key:         r.Key,
			Url:         r.URL,
			Description: r.Description,
		}
	}
	return out
}

// fromProtoExternalRefs converts proto ExternalRefs back to internal type.
func fromProtoExternalRefs(refs []*cwbv1.ExternalRef) []ExternalRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]ExternalRef, len(refs))
	for i, r := range refs {
		out[i] = ExternalRef{
			Tracker:     r.Tracker,
			Key:         r.Key,
			URL:         r.Url,
			Description: r.Description,
		}
	}
	return out
}

// toProtoProject converts an internal Project to the proto wire type.
func toProtoProject(p *Project) *cwbv1.Project {
	if p == nil {
		return nil
	}
	return &cwbv1.Project{
		Key:          p.Key,
		Organisation: p.Organisation,
		Name:         p.Name,
		Description:  p.Description,
		DefaultTeam:  p.DefaultTeam,
		Archived:     p.Archived,
	}
}

// toProtoProjectSlice converts a slice of projects.
func toProtoProjectSlice(projects []Project) []*cwbv1.Project {
	out := make([]*cwbv1.Project, len(projects))
	for i, p := range projects {
		p := p
		out[i] = toProtoProject(&p)
	}
	return out
}

// toProtoOrg converts an internal Organisation to proto.
func toProtoOrg(o *Organisation) *cwbv1.Organisation {
	if o == nil {
		return nil
	}
	return &cwbv1.Organisation{Slug: o.Slug, Name: o.Name}
}

// toProtoOrgSlice converts a slice of orgs.
func toProtoOrgSlice(orgs []Organisation) []*cwbv1.Organisation {
	out := make([]*cwbv1.Organisation, len(orgs))
	for i, o := range orgs {
		o := o
		out[i] = toProtoOrg(&o)
	}
	return out
}

// toProtoUser converts an internal User to proto.
func toProtoUser(u *User) *cwbv1.User {
	if u == nil {
		return nil
	}
	return &cwbv1.User{Id: u.ID, Kind: u.Kind}
}

// toProtoUserSlice converts a slice of users.
func toProtoUserSlice(users []User) []*cwbv1.User {
	out := make([]*cwbv1.User, len(users))
	for i, u := range users {
		u := u
		out[i] = toProtoUser(&u)
	}
	return out
}

// toProtoMemberSlice converts a slice of OrgMembers.
func toProtoMemberSlice(members []OrgMember) []*cwbv1.OrgMember {
	out := make([]*cwbv1.OrgMember, len(members))
	for i, m := range members {
		out[i] = &cwbv1.OrgMember{Org: m.Org, UserId: m.UserID, Role: m.Role}
	}
	return out
}

// toProtoEvent converts an internal Event to proto.
// Payload (map[string]any) is re-marshalled to JSON string.
func toProtoEvent(e Event) *cwbv1.Event {
	payload := ""
	if len(e.Payload) > 0 {
		if b, err := json.Marshal(e.Payload); err == nil {
			payload = string(b)
		}
	}
	return &cwbv1.Event{
		Id:       e.ID,
		IssueKey: e.IssueKey,
		Seq:      int32(e.Seq),
		Kind:     e.Kind,
		Actor:    e.Actor,
		At:       e.At,
		Payload:  payload,
	}
}

// toProtoEventSlice converts a slice of events.
func toProtoEventSlice(events []Event) []*cwbv1.Event {
	out := make([]*cwbv1.Event, len(events))
	for i, e := range events {
		out[i] = toProtoEvent(e)
	}
	return out
}

// toProtoLinkRow converts a DirectedLink to the proto LinkRow wire type.
func toProtoLinkRow(dl DirectedLink) *cwbv1.LinkRow {
	return &cwbv1.LinkRow{
		FromKey:   dl.Link.FromKey,
		ToKey:     dl.Link.ToKey,
		Type:      string(dl.Link.Type),
		CreatedAt: dl.Link.CreatedAt,
		CreatedBy: dl.Link.CreatedBy,
		Direction: string(dl.Direction),
	}
}

// toProtoLinkRows converts a slice of DirectedLinks.
func toProtoLinkRows(dls []DirectedLink) []*cwbv1.LinkRow {
	out := make([]*cwbv1.LinkRow, len(dls))
	for i, dl := range dls {
		out[i] = toProtoLinkRow(dl)
	}
	return out
}
