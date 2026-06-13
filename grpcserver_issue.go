package ledger

import (
	"context"
	"strings"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// issueServer implements cwbv1.IssueServiceServer backed by the
// unchanged Service/store layer. Identity is read from gRPC metadata
// (cwb-* keys injected by interchange) instead of HTTP headers.
type issueServer struct {
	cwbv1.UnimplementedIssueServiceServer
	svc *Service
}

// NewIssueServer wraps svc in the gRPC issue service implementation.
func NewIssueServer(svc *Service) *issueServer {
	return &issueServer{svc: svc}
}

func (s *issueServer) toProtoIssue(ctx context.Context, iss *Issue) (*cwbv1.Issue, error) {
	if iss == nil {
		return nil, nil
	}
	wf, err := s.svc.workflowForProject(ctx, iss.Project)
	if err != nil {
		return nil, err
	}
	return toProtoIssueWithCategory(iss, statusCategoryForWorkflow(wf, iss.Status)), nil
}

func (s *issueServer) toProtoIssueRefs(ctx context.Context, refs []IssueRef) ([]*cwbv1.IssueRef, error) {
	out := make([]*cwbv1.IssueRef, len(refs))
	workflows := make(map[string]*cwbv1.Workflow)
	for i, r := range refs {
		wf, ok := workflows[r.Project]
		if !ok {
			var err error
			wf, err = s.svc.workflowForProject(ctx, r.Project)
			if err != nil {
				return nil, err
			}
			workflows[r.Project] = wf
		}
		out[i] = toProtoIssueRefWithCategory(r, statusCategoryForWorkflow(wf, r.Status))
	}
	return out, nil
}

func (s *issueServer) CreateIssue(ctx context.Context, r *cwbv1.CreateIssueRequest) (*cwbv1.CreateIssueResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	// Reporter: use the authenticated subject (gateway path), falling back
	// to the request-supplied reporter (same pattern as rest.go).
	reporter := r.Reporter
	if claims.Sub != "" {
		reporter = claims.Sub
	}
	d := IssueDraft{
		Project:          r.Project,
		Type:             r.Type,
		Summary:          r.Summary,
		Description:      r.Description,
		DefinitionOfDone: r.DefinitionOfDone,
		Priority:         r.Priority,
		Reporter:         reporter,
		ParentKey:        r.ParentKey,
		AssigneeAspect:   r.AssigneeAspect,
		AssigneeTeam:     r.AssigneeTeam,
		ExternalRefs:     fromProtoExternalRefs(r.ExternalRefs),
		Skills:           r.Skills,
	}
	iss, err := s.svc.CreateIssue(ContextWithAuth(ctx, claims), d)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := s.toProtoIssue(ctx, iss)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.CreateIssueResponse{Issue: out}, nil
}

func (s *issueServer) GetIssue(ctx context.Context, r *cwbv1.GetIssueRequest) (*cwbv1.GetIssueResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	iss, err := s.svc.GetIssue(ContextWithAuth(ctx, claims), r.Key)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := s.toProtoIssue(ctx, iss)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.GetIssueResponse{Issue: out}, nil
}

func (s *issueServer) UpdateIssue(ctx context.Context, r *cwbv1.UpdateIssueRequest) (*cwbv1.UpdateIssueResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	// Actor: use the authenticated subject; allow caller to override via r.Actor
	// only if the gateway hasn't injected one (same pattern as effectiveActor).
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	patch := UpdatePatch{}
	if r.Summary != "" {
		v := r.Summary
		patch.Summary = &v
	}
	if r.Description != "" {
		v := r.Description
		patch.Description = &v
	}
	if r.DefinitionOfDone != "" {
		v := r.DefinitionOfDone
		patch.DefinitionOfDone = &v
	}
	if r.Priority != "" {
		v := r.Priority
		patch.Priority = &v
	}
	if r.ParentKey != "" {
		v := r.ParentKey
		patch.ParentKey = &v
	}
	if len(r.ExternalRefs) > 0 {
		refs := fromProtoExternalRefs(r.ExternalRefs)
		patch.ExternalRefs = &refs
	}
	if err := s.svc.UpdateIssue(ContextWithAuth(ctx, claims), r.Key, patch, actor); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.UpdateIssueResponse{}, nil
}

func (s *issueServer) TransitionIssue(ctx context.Context, r *cwbv1.TransitionIssueRequest) (*cwbv1.TransitionIssueResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	if err := s.svc.TransitionIssue(ContextWithAuth(ctx, claims), r.Key, r.Status, actor); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.TransitionIssueResponse{}, nil
}

func (s *issueServer) SetProjectWorkflow(ctx context.Context, r *cwbv1.SetProjectWorkflowRequest) (*cwbv1.SetProjectWorkflowResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	if err := s.svc.SetProjectWorkflow(ContextWithAuth(ctx, claims), r.Project, r.Workflow); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.SetProjectWorkflowResponse{}, nil
}

func (s *issueServer) GetProjectWorkflow(ctx context.Context, r *cwbv1.GetProjectWorkflowRequest) (*cwbv1.GetProjectWorkflowResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	wf, err := s.svc.GetProjectWorkflow(ContextWithAuth(ctx, claims), r.Project)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.GetProjectWorkflowResponse{Workflow: wf}, nil
}

func (s *issueServer) AssignIssue(ctx context.Context, r *cwbv1.AssignIssueRequest) (*cwbv1.AssignIssueResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	if err := s.svc.AssignIssue(ContextWithAuth(ctx, claims), r.Key, r.Aspect, r.Team, actor); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.AssignIssueResponse{}, nil
}

func (s *issueServer) CommentIssue(ctx context.Context, r *cwbv1.CommentIssueRequest) (*cwbv1.CommentIssueResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	if err := s.svc.CommentIssue(ContextWithAuth(ctx, claims), r.Key, actor, r.Body); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.CommentIssueResponse{}, nil
}

func (s *issueServer) ListComments(ctx context.Context, r *cwbv1.ListCommentsRequest) (*cwbv1.ListCommentsResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	authCtx := ContextWithAuth(ctx, claims)
	if err := s.svc.callerCanAccessIssue(authCtx, r.Key); err != nil {
		return nil, toStatus(err)
	}
	events, err := s.svc.Timeline(authCtx, r.Key)
	if err != nil {
		return nil, toStatus(err)
	}
	// Filter to comment events only.
	var comments []*cwbv1.Event
	for _, e := range events {
		if e.Kind == "comment" {
			e := e
			comments = append(comments, toProtoEvent(e))
		}
	}
	if comments == nil {
		comments = []*cwbv1.Event{}
	}
	return &cwbv1.ListCommentsResponse{Comments: comments}, nil
}

func (s *issueServer) ClaimIssue(ctx context.Context, r *cwbv1.ClaimIssueRequest) (*cwbv1.ClaimIssueResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:claim") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:claim")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	iss, err := s.svc.ClaimIssue(ContextWithAuth(ctx, claims), r.Key, actor)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := s.toProtoIssue(ctx, iss)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.ClaimIssueResponse{Issue: out}, nil
}

func (s *issueServer) AddWatcher(ctx context.Context, r *cwbv1.AddWatcherRequest) (*cwbv1.AddWatcherResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	if err := s.svc.WatchIssue(ContextWithAuth(ctx, claims), r.Key, r.Aspect, actor); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.AddWatcherResponse{}, nil
}

func (s *issueServer) ListWatchers(ctx context.Context, r *cwbv1.ListWatchersRequest) (*cwbv1.ListWatchersResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	watchers, err := s.svc.Watchers(ContextWithAuth(ctx, claims), r.Key)
	if err != nil {
		return nil, toStatus(err)
	}
	if watchers == nil {
		watchers = []string{}
	}
	return &cwbv1.ListWatchersResponse{Watchers: watchers}, nil
}

func (s *issueServer) RemoveWatcher(ctx context.Context, r *cwbv1.RemoveWatcherRequest) (*cwbv1.RemoveWatcherResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	if err := s.svc.UnwatchIssue(ContextWithAuth(ctx, claims), r.Key, r.Aspect, actor); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.RemoveWatcherResponse{}, nil
}

func (s *issueServer) AddLink(ctx context.Context, r *cwbv1.AddLinkRequest) (*cwbv1.AddLinkResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	if err := s.svc.LinkIssues(ContextWithAuth(ctx, claims), r.Key, r.ToKey, LinkType(r.Type), actor); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.AddLinkResponse{FromKey: r.Key, ToKey: r.ToKey, Type: r.Type}, nil
}

func (s *issueServer) ListLinks(ctx context.Context, r *cwbv1.ListLinksRequest) (*cwbv1.ListLinksResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	dls, err := s.svc.Links(ContextWithAuth(ctx, claims), r.Key)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.ListLinksResponse{Links: toProtoLinkRows(dls)}, nil
}

func (s *issueServer) RemoveLink(ctx context.Context, r *cwbv1.RemoveLinkRequest) (*cwbv1.RemoveLinkResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:write") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:write")
	}
	actor := claims.Sub
	if actor == "" {
		actor = r.Actor
	}
	if err := s.svc.UnlinkIssues(ContextWithAuth(ctx, claims), r.Key, r.ToKey, LinkType(r.Type), actor); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.RemoveLinkResponse{FromKey: r.Key, ToKey: r.ToKey, Type: r.Type, Removed: true}, nil
}

func (s *issueServer) ListMyIssues(ctx context.Context, r *cwbv1.ListMyIssuesRequest) (*cwbv1.ListMyIssuesResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	// aspect: use authenticated subject; allow request field fallback for backwards compat.
	aspect := claims.Sub
	if aspect == "" {
		aspect = r.Aspect
	}
	refs, err := s.svc.ListMy(ContextWithAuth(ctx, claims), aspect, nil)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := s.toProtoIssueRefs(ctx, refs)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.ListMyIssuesResponse{Issues: out}, nil
}

func (s *issueServer) ListReadyIssues(ctx context.Context, r *cwbv1.ListReadyIssuesRequest) (*cwbv1.ListReadyIssuesResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	aspect := claims.Sub
	if aspect == "" {
		aspect = r.Aspect
	}
	refs, err := s.svc.ListReady(ContextWithAuth(ctx, claims), aspect, nil, r.Skills)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := s.toProtoIssueRefs(ctx, refs)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.ListReadyIssuesResponse{Issues: out}, nil
}

func (s *issueServer) SearchIssues(ctx context.Context, r *cwbv1.SearchIssuesRequest) (*cwbv1.SearchIssuesResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	f := SearchFilter{}
	if r.Filter != nil {
		f = SearchFilter{
			Projects:       r.Filter.Projects,
			Types:          r.Filter.Types,
			Statuses:       r.Filter.Statuses,
			Priorities:     r.Filter.Priorities,
			AssigneeAspect: r.Filter.AssigneeAspect,
			AssigneeTeam:   r.Filter.AssigneeTeam,
			Reporter:       r.Filter.Reporter,
			ParentKey:      r.Filter.ParentKey,
			OrderBy:        r.Filter.OrderBy,
			OrderDir:       r.Filter.OrderDir,
			Limit:          int(r.Filter.Limit),
		}
	}
	refs, err := s.svc.Search(ContextWithAuth(ctx, claims), f)
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := s.toProtoIssueRefs(ctx, refs)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.SearchIssuesResponse{Refs: out}, nil
}

func (s *issueServer) SearchIssuesText(ctx context.Context, r *cwbv1.SearchIssuesTextRequest) (*cwbv1.SearchIssuesTextResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	if strings.TrimSpace(r.Q) == "" {
		return nil, status.Error(codes.InvalidArgument, "q required")
	}
	refs, err := s.svc.FindByText(ContextWithAuth(ctx, claims), r.Q, int(r.Limit))
	if err != nil {
		return nil, toStatus(err)
	}
	out, err := s.toProtoIssueRefs(ctx, refs)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.SearchIssuesTextResponse{Refs: out}, nil
}

func (s *issueServer) ListUpdates(ctx context.Context, r *cwbv1.ListUpdatesRequest) (*cwbv1.ListUpdatesResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	aspect := claims.Sub
	if aspect == "" {
		aspect = r.Aspect
	}
	events, err := s.svc.ListMyUpdates(ContextWithAuth(ctx, claims), aspect, r.SinceId, int(r.Limit))
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.ListUpdatesResponse{Events: toProtoEventSlice(events)}, nil
}
