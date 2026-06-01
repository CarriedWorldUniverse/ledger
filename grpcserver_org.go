package ledger

import (
	"context"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// orgServer implements cwbv1.OrgServiceServer.
type orgServer struct {
	cwbv1.UnimplementedOrgServiceServer
	svc *Service
}

// NewOrgServer wraps svc in the gRPC org service implementation.
func NewOrgServer(svc *Service) *orgServer {
	return &orgServer{svc: svc}
}

// PurgeOrg removes the caller's entire org (all projects and issues).
// Requires org:purge scope; issue:admin does NOT satisfy this.
func (s *orgServer) PurgeOrg(ctx context.Context, _ *cwbv1.OrgServicePurgeOrgRequest) (*cwbv1.OrgServicePurgeOrgResponse, error) {
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	if !hasScope(scopes, "org:purge") {
		return nil, status.Error(codes.PermissionDenied, "missing scope org:purge")
	}
	if err := s.svc.PurgeOrganisation(ContextWithAuth(ctx, claims), claims.Org); err != nil {
		return nil, toStatus(err)
	}
	return &cwbv1.OrgServicePurgeOrgResponse{Purged: claims.Org}, nil
}
