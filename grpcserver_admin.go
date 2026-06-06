package ledger

import (
	"context"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// adminServer implements cwbv1.AdminServiceServer.
type adminServer struct {
	cwbv1.UnimplementedAdminServiceServer
	svc *Service
}

// NewAdminServer wraps svc in the gRPC admin service implementation.
func NewAdminServer(svc *Service) *adminServer {
	return &adminServer{svc: svc}
}

// --- orgs ---

func (s *adminServer) CreateOrg(ctx context.Context, r *cwbv1.CreateOrgRequest) (*cwbv1.CreateOrgResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	org, err := s.svc.CreateOrganisation(ctx, r.Slug, r.Name)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.CreateOrgResponse{Org: toProtoOrg(org)}, nil
}

func (s *adminServer) ListOrgs(ctx context.Context, _ *cwbv1.ListOrgsRequest) (*cwbv1.ListOrgsResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	orgs, err := s.svc.ListOrganisations(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	if orgs == nil {
		orgs = []Organisation{}
	}
	return &cwbv1.ListOrgsResponse{Orgs: toProtoOrgSlice(orgs)}, nil
}

func (s *adminServer) GetOrg(ctx context.Context, r *cwbv1.GetOrgRequest) (*cwbv1.GetOrgResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	org, err := s.svc.GetOrganisation(ctx, r.Slug)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.GetOrgResponse{Org: toProtoOrg(org)}, nil
}

func (s *adminServer) UpdateOrg(ctx context.Context, r *cwbv1.UpdateOrgRequest) (*cwbv1.UpdateOrgResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	if err := s.svc.UpdateOrganisation(ctx, r.Slug, r.Name); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.UpdateOrgResponse{}, nil
}

func (s *adminServer) DeleteOrg(ctx context.Context, r *cwbv1.DeleteOrgRequest) (*cwbv1.DeleteOrgResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	if err := s.svc.DeleteOrganisation(ctx, r.Slug); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.DeleteOrgResponse{}, nil
}

// --- members ---

func (s *adminServer) AddMember(ctx context.Context, r *cwbv1.AddMemberRequest) (*cwbv1.AddMemberResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	if err := s.svc.AddOrgMember(ctx, r.Slug, r.UserId, r.Role); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.AddMemberResponse{}, nil
}

func (s *adminServer) ListMembers(ctx context.Context, r *cwbv1.ListMembersRequest) (*cwbv1.ListMembersResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	members, err := s.svc.ListOrgMembers(ctx, r.Slug)
	if err != nil {
		return nil, toStatus(err)
	}
	if members == nil {
		members = []OrgMember{}
	}
	return &cwbv1.ListMembersResponse{Members: toProtoMemberSlice(members)}, nil
}

func (s *adminServer) RemoveMember(ctx context.Context, r *cwbv1.RemoveMemberRequest) (*cwbv1.RemoveMemberResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	if err := s.svc.RemoveOrgMember(ctx, r.Slug, r.UserId); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.RemoveMemberResponse{}, nil
}

// --- users ---

func (s *adminServer) CreateUser(ctx context.Context, r *cwbv1.CreateUserRequest) (*cwbv1.CreateUserResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	user, err := s.svc.CreateUser(ctx, r.Id, r.Kind)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.CreateUserResponse{User: toProtoUser(user)}, nil
}

func (s *adminServer) ListUsers(ctx context.Context, _ *cwbv1.ListUsersRequest) (*cwbv1.ListUsersResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	users, err := s.svc.ListUsers(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	if users == nil {
		users = []User{}
	}
	return &cwbv1.ListUsersResponse{Users: toProtoUserSlice(users)}, nil
}

func (s *adminServer) GetUser(ctx context.Context, r *cwbv1.GetUserRequest) (*cwbv1.GetUserResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	user, err := s.svc.GetUser(ctx, r.Id)
	if err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.GetUserResponse{User: toProtoUser(user)}, nil
}

func (s *adminServer) UpdateUser(ctx context.Context, r *cwbv1.UpdateUserRequest) (*cwbv1.UpdateUserResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	if err := s.svc.UpdateUser(ctx, r.Id, r.Kind); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.UpdateUserResponse{}, nil
}

func (s *adminServer) DeleteUser(ctx context.Context, r *cwbv1.DeleteUserRequest) (*cwbv1.DeleteUserResponse, error) {
	_, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "issue:admin") {
		return nil, status.Error(codes.PermissionDenied, "missing scope issue:admin")
	}
	if err := s.svc.DeleteUser(ctx, r.Id); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.DeleteUserResponse{}, nil
}
