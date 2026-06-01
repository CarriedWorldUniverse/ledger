package ledger

import (
	"context"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// projectServer implements cwbv1.ProjectServiceServer.
type projectServer struct {
	cwbv1.UnimplementedProjectServiceServer
	svc *Service
}

// NewProjectServer wraps svc in the gRPC project service implementation.
func NewProjectServer(svc *Service) *projectServer {
	return &projectServer{svc: svc}
}

func (s *projectServer) CreateProject(ctx context.Context, r *cwbv1.CreateProjectRequest) (*cwbv1.CreateProjectResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	// Organisation comes from the auth context (tenancy), never the body.
	p := Project{
		Key:          r.Key,
		Name:         r.Name,
		Description:  r.Description,
		DefaultTeam:  r.DefaultTeam,
		Organisation: claims.Org,
	}
	if err := s.svc.CreateProject(ContextWithAuth(ctx, claims), p); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.CreateProjectResponse{
		Key:          r.Key,
		Organisation: claims.Org,
		Name:         r.Name,
	}, nil
}

func (s *projectServer) ListProjects(ctx context.Context, r *cwbv1.ListProjectsRequest) (*cwbv1.ListProjectsResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:read") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:read")
	}
	projects, err := s.svc.ListProjects(ContextWithAuth(ctx, claims), r.IncludeArchived)
	if err != nil {
		return nil, toStatus(err)
	}
	if projects == nil {
		projects = []Project{}
	}
	return &cwbv1.ListProjectsResponse{Projects: toProtoProjectSlice(projects)}, nil
}
