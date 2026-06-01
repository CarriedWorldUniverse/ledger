package main

// serverConfig is the cmd/ledger server's env-driven runtime config.
type serverConfig struct {
	GRPCAddr string // listen address, e.g. ":8081"
	DBPath   string // sqlite path
}

// loadConfig reads the server config from the environment, applying
// CWB-product defaults: the standard CWB data path and the ledger service
// port (:8081).
func loadConfig() serverConfig {
	return serverConfig{
		GRPCAddr: env("LEDGER_GRPC_ADDR", ":8081"),
		DBPath:   env("LEDGER_DB", "/var/lib/cwb/ledger.db"),
	}
}
