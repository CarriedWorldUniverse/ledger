package ledger

import (
	"encoding/json"
	"net/http"
	"strings"
)

// adminMux registers all /api/admin/* routes. Mounted in Handler().
func (s *Service) adminMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/orgs", s.handleAdminOrgs)
	mux.HandleFunc("/api/admin/orgs/", s.handleAdminOrgBySlug)
	mux.HandleFunc("/api/admin/users", s.handleAdminUsers)
	mux.HandleFunc("/api/admin/users/", s.handleAdminUserByID)
	return mux
}

func (s *Service) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.adminToken == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == s.adminToken {
		return true
	}
	http.Error(w, `{"error":"admin_required"}`, http.StatusForbidden)
	return false
}

// --- orgs ---

func (s *Service) handleAdminOrgs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		orgs, err := s.ListOrganisations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if orgs == nil {
			orgs = []Organisation{}
		}
		writeJSON(w, http.StatusOK, orgs)
	case http.MethodPost:
		var raw struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		org, err := s.CreateOrganisation(r.Context(), raw.Slug, raw.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, org)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleAdminOrgBySlug(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/admin/orgs/")
	parts := strings.SplitN(tail, "/", 2)
	slug := parts[0]
	if slug == "" {
		http.Error(w, "slug required", http.StatusBadRequest)
		return
	}

	if len(parts) == 2 && parts[1] == "members" {
		s.handleAdminMembers(w, r, slug)
		return
	}
	if len(parts) == 2 && strings.HasPrefix(parts[1], "members/") {
		s.handleAdminMemberByID(w, r, slug, strings.TrimPrefix(parts[1], "members/"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		org, err := s.GetOrganisation(r.Context(), slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, org)
	case http.MethodPut:
		var raw struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.UpdateOrganisation(r.Context(), slug, raw.Name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		if err := s.DeleteOrganisation(r.Context(), slug); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- users ---

func (s *Service) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		users, err := s.ListUsers(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if users == nil {
			users = []User{}
		}
		writeJSON(w, http.StatusOK, users)
	case http.MethodPost:
		var raw struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		user, err := s.CreateUser(r.Context(), raw.ID, raw.Kind)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, user)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleAdminUserByID(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		user, err := s.GetUser(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, user)
	case http.MethodPut:
		var raw struct {
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.UpdateUser(r.Context(), id, raw.Kind); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		if err := s.DeleteUser(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- members ---

func (s *Service) handleAdminMembers(w http.ResponseWriter, r *http.Request, org string) {
	switch r.Method {
	case http.MethodGet:
		members, err := s.ListOrgMembers(r.Context(), org)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if members == nil {
			members = []OrgMember{}
		}
		writeJSON(w, http.StatusOK, members)
	case http.MethodPost:
		var raw struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.AddOrgMember(r.Context(), org, raw.UserID, raw.Role); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		var raw struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.RemoveOrgMember(r.Context(), org, raw.UserID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleAdminMemberByID(w http.ResponseWriter, r *http.Request, org, userID string) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var raw struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.AddOrgMember(r.Context(), org, userID, raw.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
