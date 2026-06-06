package ledger

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

// identityFromMD reads the cwb-* gRPC metadata keys injected by interchange
// and returns AuthClaims + scopes. Returns (nil, nil, false) if either
// cwb-subject or cwb-org is absent (the gateway always sets both for authed
// requests; their absence means the request didn't transit the gateway).
func identityFromMD(ctx context.Context) (claims *AuthClaims, scopes []string, ok bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, nil, false
	}
	get := func(k string) string {
		v := md.Get(k)
		if len(v) == 0 {
			return ""
		}
		return v[0]
	}
	sub := get("cwb-subject")
	org := get("cwb-org")
	if sub == "" || org == "" {
		return nil, nil, false
	}
	c := &AuthClaims{
		Sub: sub,
		Org: org,
	}
	sc := strings.Fields(get("cwb-scopes"))
	return c, sc, true
}

// hasScope reports whether the caller holds the required scope.
// issue:admin is a superset for ordinary scopes BUT must NOT satisfy
// org:purge (the destructive org-wipe scope, which must be held explicitly).
func hasScope(have []string, need string) bool {
	for _, s := range have {
		if s == need {
			return true
		}
		// issue:admin is a superset for ordinary scopes, but NOT for org:purge.
		if s == "issue:admin" && need != "org:purge" {
			return true
		}
	}
	return false
}
