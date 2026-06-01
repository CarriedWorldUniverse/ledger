package main

import "testing"

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("LEDGER_GRPC_ADDR", "")
	t.Setenv("LEDGER_DB", "")
	cfg := loadConfig()
	if cfg.GRPCAddr != ":8081" {
		t.Errorf("GRPCAddr default = %q, want :8081", cfg.GRPCAddr)
	}
	if cfg.DBPath != "/var/lib/cwb/ledger.db" {
		t.Errorf("DBPath default = %q", cfg.DBPath)
	}
}

func TestLoadConfig_FromEnv(t *testing.T) {
	t.Setenv("LEDGER_GRPC_ADDR", ":9090")
	t.Setenv("LEDGER_DB", "/tmp/x.db")
	cfg := loadConfig()
	if cfg.GRPCAddr != ":9090" {
		t.Errorf("GRPCAddr = %q, want :9090", cfg.GRPCAddr)
	}
	if cfg.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q, want /tmp/x.db", cfg.DBPath)
	}
}
