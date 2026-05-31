package main

import (
	"net/http"
	"strings"

	"github.com/CarriedWorldUniverse/ledger"
)

// middleware is a standard http.Handler decorator.
type middleware func(http.Handler) http.Handler

// isPublicPath reports whether a path is served without identity in
// every mode: liveness/readiness probes are tokenless. (The gateway
// never reaches these from outside in production — they're on the
// ClusterIP for kubelet — but the middleware must let them through
// regardless of mode.)
func isPublicPath(path string) bool {
	return path == "/healthz/issues"
}

// scopeForMethodPath maps an inbound request to the herald scope that
// gates it (spec §3 / §9 vocabulary). Read surface → issue:read; the
// mutating verbs → issue:write; the atomic claim verb → issue:claim;
// /api/admin/* → issue:admin. An unknown path falls back to the most
// restrictive sensible gate (issue:write) so a new mutating route is
// never accidentally world-open.
func scopeForMethodPath(method, path string) string {
	switch {
	case method == http.MethodDelete && path == "/api/org":
		return "org:purge"
	case strings.HasPrefix(path, "/api/admin/"):
		return "issue:admin"
	case path == "/api/projects" && method == http.MethodPost:
		// Creating a project is a structural/operator op (parallels org+user
		// setup), not an everyday agent write — gate it like admin.
		return "issue:admin"
	case strings.HasSuffix(path, "/claim") && method == http.MethodPost:
		return "issue:claim"
	case method == http.MethodGet:
		return "issue:read"
	case method == http.MethodPost && (path == "/api/issues/search" || path == "/api/issues/search/text"):
		return "issue:read"
	default:
		// POST create, PATCH, transition, assign, comments, watchers.
		return "issue:write"
	}
}

// buildAuthMiddleware returns the auth decorator for the configured
// deployment mode.
//
//   - "embedded": the existing in-nexus HS256 path. We do NOT re-wrap
//     here; the library's own admin mux already runs authMiddleware for
//     /api/admin/*, and the embedded broker mounts the rest behind its
//     own listener. In standalone embedded mode we simply pass through —
//     identity is asserted by the HS256 token the library resolves per
//     request (resolveAuth). This keeps the embedded path byte-for-byte
//     the library's existing behaviour.
//   - "gateway" (default): trust X-CWB-* injected by the mTLS gateway.
func buildAuthMiddleware(cfg serverConfig) middleware {
	if cfg.AuthMode == "embedded" {
		return func(next http.Handler) http.Handler { return next }
	}
	return gatewayIdentity
}

// gatewayIdentity reads the gateway-injected X-CWB-* headers, maps them
// to a ledger.AuthClaims, enforces the per-route scope, and injects the
// claims into the request context so the library's tenancy gates fire.
// Absent identity headers on a gated path → 401 (the gateway only omits
// them when it did not authenticate — which must never reach a gated
// route).
func gatewayIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		sub := r.Header.Get("X-CWB-Subject")
		org := r.Header.Get("X-CWB-Org")
		if sub == "" || org == "" {
			http.Error(w, `{"error":"identity_required"}`, http.StatusUnauthorized)
			return
		}

		scopes := parseScopes(r.Header.Get("X-CWB-Scopes"))
		need := scopeForMethodPath(r.Method, r.URL.Path)
		if !hasScope(scopes, need) {
			http.Error(w, `{"error":"insufficient_scope"}`, http.StatusForbidden)
			return
		}

		// Map herald identity → ledger claims. Org drives the tenancy
		// gate; Sub is the actor (the mutation handlers thread it in).
		// Role is left empty: gateway-path authorization is scope-based,
		// not the embedded role ladder. The library's role checks only
		// run on the embedded HS256 path.
		claims := &ledger.AuthClaims{Sub: sub, Org: org}
		ctx := ledger.ContextWithAuth(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func parseScopes(raw string) []string {
	return strings.Fields(raw) // space- or whitespace-separated; gateway space-joins
}

func hasScope(have []string, need string) bool {
	for _, s := range have {
		if s == need || s == "issue:admin" {
			return true // issue:admin is a superset
		}
	}
	return false
}
