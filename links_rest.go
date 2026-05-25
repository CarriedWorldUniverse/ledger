package ledger

import (
	"encoding/json"
	"errors"
	"net/http"
)

// respondLinks dispatches GET / POST / DELETE on /api/issues/{key}/links.
//
//	GET    → list every directional link touching {key} (LinkIssues +
//	         direction tag). Returns array of {from_key, to_key, type,
//	         created_at, created_by, direction}.
//	POST   → create a link. Body: {to_key, type, actor}. {key} is
//	         the from-side; "blocks" means {key} blocks to_key.
//	DELETE → remove a link. Body: {to_key, type, actor}. Idempotent
//	         (deleting a non-existent edge returns 200, no error).
//
// All variants gate on callerCanAccessIssue for both endpoints via the
// underlying LinkIssues / UnlinkIssues / Links methods.
func (s *Service) respondLinks(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodGet:
		s.respondListLinks(w, r, key)
	case http.MethodPost:
		s.respondLinkCreate(w, r, key)
	case http.MethodDelete:
		s.respondLinkDelete(w, r, key)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// linkRESTRow flattens DirectedLink to the JSON shape clients see.
// Inlined struct rather than reusing DirectedLink so the wire format
// is decoupled from internal field renaming.
type linkRESTRow struct {
	FromKey   string `json:"from_key"`
	ToKey     string `json:"to_key"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	Direction string `json:"direction"` // "outgoing" | "incoming"
}

func (s *Service) respondListLinks(w http.ResponseWriter, r *http.Request, key string) {
	dls, err := s.Links(r.Context(), key)
	if err != nil {
		if errors.Is(err, ErrIssueNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]linkRESTRow, 0, len(dls))
	for _, dl := range dls {
		out = append(out, linkRESTRow{
			FromKey:   dl.Link.FromKey,
			ToKey:     dl.Link.ToKey,
			Type:      string(dl.Link.Type),
			CreatedAt: dl.Link.CreatedAt,
			CreatedBy: dl.Link.CreatedBy,
			Direction: string(dl.Direction),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"links": out})
}

type linkMutateReq struct {
	ToKey string `json:"to_key"`
	Type  string `json:"type"`
	Actor string `json:"actor"`
}

func (s *Service) respondLinkCreate(w http.ResponseWriter, r *http.Request, key string) {
	var req linkMutateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ToKey == "" || req.Type == "" {
		http.Error(w, "to_key and type required", http.StatusBadRequest)
		return
	}
	err := s.LinkIssues(r.Context(), key, req.ToKey, LinkType(req.Type), req.Actor)
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"from_key": key,
		"to_key":   req.ToKey,
		"type":     req.Type,
	})
}

func (s *Service) respondLinkDelete(w http.ResponseWriter, r *http.Request, key string) {
	var req linkMutateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ToKey == "" || req.Type == "" {
		http.Error(w, "to_key and type required", http.StatusBadRequest)
		return
	}
	err := s.UnlinkIssues(r.Context(), key, req.ToKey, LinkType(req.Type), req.Actor)
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"from_key": key,
		"to_key":   req.ToKey,
		"type":     req.Type,
		"removed":  true,
	})
}

// writeLinkError translates link-specific sentinel errors to HTTP codes.
// 404 hides existence (callerCanAccessIssue); 400 for shape/semantic
// errors; 500 otherwise.
func (s *Service) writeLinkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrIssueNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrInvalidLinkType), errors.Is(err, ErrSelfLink):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
