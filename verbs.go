package ledger

import (
	"encoding/json"
	"errors"
	"net/http"
)

// callerAspect resolves the aspect a "my"/"ready" query is scoped to.
// The herald-gated path injects AuthClaims whose Sub is the calling
// agent — that wins. The embedded / in-process path (no claims) falls
// back to the ?aspect= query param so existing callers keep working.
func callerAspect(r *http.Request) string {
	if c := AuthFromContext(r.Context()); c != nil && c.Sub != "" {
		return c.Sub
	}
	return r.URL.Query().Get("aspect")
}

// AuthSubject returns the herald Subject from the request's injected
// claims, or "" if none (embedded/in-process path). Used by the mutation
// handlers to attribute actions to the individual agent.
func AuthSubject(r *http.Request) string {
	if c := AuthFromContext(r.Context()); c != nil {
		return c.Sub
	}
	return ""
}

// handleListMy powers GET /api/issues/my — the caller's open assigned
// issues (ListMy). Scoped to callerAspect; team membership is not
// resolved at the MVP (teams=nil), so this returns directly-assigned
// work, which is the agent daily-driver case.
func (s *Service) handleListMy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	aspect := callerAspect(r)
	if aspect == "" {
		http.Error(w, "aspect required", http.StatusBadRequest)
		return
	}
	refs, err := s.ListMy(r.Context(), aspect, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if refs == nil {
		refs = []IssueRef{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(refs)
}

// handleListReady powers GET /api/issues/ready — the top of the caller's
// ready pool (ListReady): assigned issues in a startable state, ordered
// priority-then-age. Scoped to callerAspect; teams=nil at the MVP.
func (s *Service) handleListReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	aspect := callerAspect(r)
	if aspect == "" {
		http.Error(w, "aspect required", http.StatusBadRequest)
		return
	}
	refs, err := s.ListReady(r.Context(), aspect, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if refs == nil {
		refs = []IssueRef{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(refs)
}

// respondClaim powers POST /api/issues/{key}/claim — the atomic claim
// verb. The actor is the calling agent: the herald Subject when present
// (gateway path), else the JSON body's "actor" (embedded/in-process).
// Maps the claim sentinels to HTTP: 409 ErrAlreadyClaimed, 400
// ErrNotClaimable, 404 ErrIssueNotFound.
func (s *Service) respondClaim(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actor := AuthSubject(r) // herald path
	if actor == "" {
		var raw struct {
			Actor string `json:"actor"`
		}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		actor = raw.Actor
	}
	if actor == "" {
		http.Error(w, "actor required", http.StatusBadRequest)
		return
	}
	issue, err := s.ClaimIssue(r.Context(), key, actor)
	switch {
	case errors.Is(err, ErrAlreadyClaimed):
		http.Error(w, `{"error":"already_claimed"}`, http.StatusConflict)
		return
	case errors.Is(err, ErrNotClaimable):
		http.Error(w, `{"error":"not_claimable"}`, http.StatusBadRequest)
		return
	case errors.Is(err, ErrIssueNotFound):
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(issue)
}
