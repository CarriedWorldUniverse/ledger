package ledger

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Handler returns the http.Handler that serves /api/issues/* + /healthz/issues.
// Mount under the nexus.exe broker's existing HTTPS listener.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz/issues", s.HealthzHandler())
	mux.HandleFunc("/api/issues", s.handleCreate)
	mux.HandleFunc("/api/issues/", s.handleIssueByKey)
	mux.HandleFunc("/api/issues/search", s.handleSearch)
	return mux
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var raw struct {
		Project          string `json:"project"`
		Type             string `json:"type"`
		Summary          string `json:"summary"`
		Description      string `json:"description"`
		DefinitionOfDone string `json:"definition_of_done"`
		Priority         string `json:"priority"`
		Reporter         string `json:"reporter"`
		ParentKey        string `json:"parent_key"`
		AssigneeAspect   string `json:"assignee_aspect"`
		AssigneeTeam     string `json:"assignee_team"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	d := IssueDraft{
		Project: raw.Project, Type: raw.Type, Summary: raw.Summary,
		Description: raw.Description, DefinitionOfDone: raw.DefinitionOfDone,
		Priority: raw.Priority, Reporter: raw.Reporter, ParentKey: raw.ParentKey,
		AssigneeAspect: raw.AssigneeAspect, AssigneeTeam: raw.AssigneeTeam,
	}
	issue, err := s.CreateIssue(r.Context(), d)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(issue)
}

func (s *Service) handleIssueByKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
	if key == "" || strings.Contains(key, "/") {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		issue, err := s.GetIssue(r.Context(), key)
		if errors.Is(err, ErrIssueNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(issue)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var f SearchFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	refs, err := s.Search(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(refs)
}
