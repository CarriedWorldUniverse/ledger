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
	tail := strings.TrimPrefix(r.URL.Path, "/api/issues/")
	parts := strings.SplitN(tail, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	key := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		s.respondGet(w, r, key)
	case r.Method == http.MethodPatch && action == "":
		s.respondPatch(w, r, key)
	case r.Method == http.MethodPost && action == "transition":
		s.respondTransition(w, r, key)
	case r.Method == http.MethodPost && action == "assign":
		s.respondAssign(w, r, key)
	case r.Method == http.MethodPost && action == "comments":
		s.respondComment(w, r, key)
	case action == "watchers":
		s.respondWatchers(w, r, key)
	default:
		http.Error(w, "method/path not supported", http.StatusMethodNotAllowed)
	}
}

func (s *Service) respondGet(w http.ResponseWriter, r *http.Request, key string) {
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
}

func (s *Service) respondPatch(w http.ResponseWriter, r *http.Request, key string) {
	var raw struct {
		Summary          *string `json:"summary"`
		Description      *string `json:"description"`
		DefinitionOfDone *string `json:"definition_of_done"`
		Priority         *string `json:"priority"`
		ParentKey        *string `json:"parent_key"`
		Actor            string  `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	patch := UpdatePatch{
		Summary: raw.Summary, Description: raw.Description,
		DefinitionOfDone: raw.DefinitionOfDone, Priority: raw.Priority, ParentKey: raw.ParentKey,
	}
	if err := s.UpdateIssue(r.Context(), key, patch, raw.Actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) respondTransition(w http.ResponseWriter, r *http.Request, key string) {
	var raw struct {
		Status string `json:"status"`
		Actor  string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.TransitionIssue(r.Context(), key, raw.Status, raw.Actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) respondAssign(w http.ResponseWriter, r *http.Request, key string) {
	var raw struct {
		Aspect string `json:"aspect"`
		Team   string `json:"team"`
		Actor  string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.AssignIssue(r.Context(), key, raw.Aspect, raw.Team, raw.Actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) respondComment(w http.ResponseWriter, r *http.Request, key string) {
	var raw struct {
		Actor string `json:"actor"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.CommentIssue(r.Context(), key, raw.Actor, raw.Body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Service) respondWatchers(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.Watchers(r.Context(), key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []string{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)

	case http.MethodPost:
		var raw struct {
			Aspect string `json:"aspect"`
			Actor  string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.WatchIssue(r.Context(), key, raw.Aspect, raw.Actor); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)

	case http.MethodDelete:
		var raw struct {
			Aspect string `json:"aspect"`
			Actor  string `json:"actor"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.UnwatchIssue(r.Context(), key, raw.Aspect, raw.Actor); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)

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
