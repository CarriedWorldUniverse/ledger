package ledger

import (
	"encoding/json"
	"net/http"
)

// HealthzHandler returns an http.Handler that reports service liveness
// plus the currently-applied schema version. Mount on the nexus broker
// listener at /healthz/ledger. Returns 200 + {"status":"ok","schema_version":N}
// on success; 503 + {"status":"error","error":"..."} otherwise.
func (s *Service) HealthzHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var version int
		err := s.db.QueryRowContext(r.Context(),
			`SELECT version FROM schema_versions ORDER BY version DESC LIMIT 1`,
		).Scan(&version)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"schema_version": version,
		})
	})
}
