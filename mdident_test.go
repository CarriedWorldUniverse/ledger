package ledger

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func mdCtxLedger(org, subject string, scopes ...string) context.Context {
	scopeStr := ""
	for i, s := range scopes {
		if i > 0 {
			scopeStr += " "
		}
		scopeStr += s
	}
	md := metadata.Pairs(
		"cwb-org", org,
		"cwb-subject", subject,
		"cwb-scopes", scopeStr,
	)
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestIdentityFromMD_OK(t *testing.T) {
	ctx := mdCtxLedger("acme", "agent:foo", "issue:read")
	claims, scopes, ok := identityFromMD(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if claims.Org != "acme" {
		t.Errorf("Org=%q, want acme", claims.Org)
	}
	if claims.Sub != "agent:foo" {
		t.Errorf("Sub=%q, want agent:foo", claims.Sub)
	}
	if len(scopes) != 1 || scopes[0] != "issue:read" {
		t.Errorf("scopes=%v, want [issue:read]", scopes)
	}
}

func TestIdentityFromMD_MissingOrg(t *testing.T) {
	md := metadata.Pairs("cwb-subject", "agent:foo", "cwb-scopes", "issue:read")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, _, ok := identityFromMD(ctx)
	if ok {
		t.Fatal("expected ok=false when org missing")
	}
}

func TestIdentityFromMD_MissingSubject(t *testing.T) {
	md := metadata.Pairs("cwb-org", "acme", "cwb-scopes", "issue:read")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, _, ok := identityFromMD(ctx)
	if ok {
		t.Fatal("expected ok=false when subject missing")
	}
}

func TestIdentityFromMD_NoMetadata(t *testing.T) {
	_, _, ok := identityFromMD(context.Background())
	if ok {
		t.Fatal("expected ok=false with no metadata")
	}
}

func TestHasScope_ExactMatch(t *testing.T) {
	scopes := []string{"issue:read", "issue:write"}
	if !hasScope(scopes, "issue:read") {
		t.Error("expected issue:read to match")
	}
	if !hasScope(scopes, "issue:write") {
		t.Error("expected issue:write to match")
	}
	if hasScope(scopes, "issue:claim") {
		t.Error("expected issue:claim NOT to match")
	}
}

func TestHasScope_AdminSupersetOrdinaryScope(t *testing.T) {
	scopes := []string{"issue:admin"}
	// issue:admin satisfies ordinary scopes
	for _, need := range []string{"issue:read", "issue:write", "issue:claim"} {
		if !hasScope(scopes, need) {
			t.Errorf("issue:admin should satisfy %q", need)
		}
	}
}

func TestHasScope_AdminNotOrgPurge(t *testing.T) {
	scopes := []string{"issue:admin"}
	if hasScope(scopes, "org:purge") {
		t.Error("issue:admin must NOT satisfy org:purge")
	}
}

func TestHasScope_OrgPurgeExplicit(t *testing.T) {
	scopes := []string{"org:purge"}
	if !hasScope(scopes, "org:purge") {
		t.Error("org:purge should satisfy org:purge")
	}
}
