package main

import "os"

// serverConfig is the cmd/ledger server's env-driven runtime config.
// It is distinct from ledger.Config (which configures the library/DB);
// this struct also carries the deployment-mode auth selector.
type serverConfig struct {
	Addr      string // listen address, e.g. ":8081"
	DBPath    string // sqlite path
	AuthMode  string // "gateway" | "embedded"
	JWTSecret string // HS256 secret, used only in embedded mode
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadConfig reads the server config from the environment, applying
// CWB-product defaults: gateway-trust auth, the standard nexus data
// path, and the ledger service port (:8081 — herald owns :8099).
func loadConfig() serverConfig {
	return serverConfig{
		Addr:      env("LEDGER_ADDR", ":8081"),
		DBPath:    env("LEDGER_DB", "/var/lib/nexus/ledger.db"),
		AuthMode:  env("LEDGER_AUTH_MODE", "gateway"),
		JWTSecret: os.Getenv("LEDGER_JWT_SECRET"),
	}
}
