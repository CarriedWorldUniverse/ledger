// Command ledger is the standalone CWB issue-tracker server: a thin
// HTTP wrapper around the ledger library, deployed behind the
// interchange-gateway in the cwb k3s namespace.
//
// Config (env):
//
//	LEDGER_ADDR        listen address (default :8081)
//	LEDGER_DB          sqlite path (default /var/lib/nexus/ledger.db)
//	LEDGER_AUTH_MODE   "gateway" (trust X-CWB-* from the mTLS gateway,
//	                   default) or "embedded" (HS256 self-auth for the
//	                   in-nexus path)
//	LEDGER_JWT_SECRET  HS256 secret; used only in embedded mode
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/CarriedWorldUniverse/ledger"
)

func main() {
	cfg := loadConfig()

	svc, err := ledger.New(context.Background(), ledger.Config{
		DBPath:    cfg.DBPath,
		JWTSecret: cfg.JWTSecret,
	})
	if err != nil {
		log.Fatalf("ledger: open %q: %v", cfg.DBPath, err)
	}
	defer svc.Close()

	handler := buildHandler(svc, cfg)

	log.Printf("ledger listening on %s (db=%s, auth_mode=%s)", cfg.Addr, cfg.DBPath, cfg.AuthMode)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatalf("ledger: %v", err)
	}
}

// buildHandler assembles the served handler: the library mux wrapped by
// the deployment-mode auth middleware. In Task 1 the middleware is a
// pass-through; Task 2 replaces buildAuthMiddleware with the real
// gateway-identity / HS256 selector.
func buildHandler(svc *ledger.Service, cfg serverConfig) http.Handler {
	return buildAuthMiddleware(cfg)(svc.Handler())
}
