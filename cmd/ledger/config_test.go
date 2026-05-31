package main

import "testing"

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("LEDGER_ADDR", "")
	t.Setenv("LEDGER_DB", "")
	t.Setenv("LEDGER_AUTH_MODE", "")
	t.Setenv("LEDGER_JWT_SECRET", "")
	cfg := loadConfig()
	if cfg.Addr != ":8081" {
		t.Errorf("Addr default = %q, want :8081", cfg.Addr)
	}
	if cfg.DBPath != "/var/lib/nexus/ledger.db" {
		t.Errorf("DBPath default = %q", cfg.DBPath)
	}
	if cfg.AuthMode != "gateway" {
		t.Errorf("AuthMode default = %q, want gateway", cfg.AuthMode)
	}
}

func TestLoadConfig_FromEnv(t *testing.T) {
	t.Setenv("LEDGER_ADDR", ":9090")
	t.Setenv("LEDGER_DB", "/tmp/x.db")
	t.Setenv("LEDGER_AUTH_MODE", "embedded")
	t.Setenv("LEDGER_JWT_SECRET", "s3cr3t")
	cfg := loadConfig()
	if cfg.Addr != ":9090" || cfg.DBPath != "/tmp/x.db" || cfg.AuthMode != "embedded" || cfg.JWTSecret != "s3cr3t" {
		t.Errorf("FromEnv = %+v", cfg)
	}
}
