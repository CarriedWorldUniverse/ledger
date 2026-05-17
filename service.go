package ledger

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// Config carries the service's runtime configuration.
type Config struct {
	// DBPath is the on-disk location of ledger.db.
	DBPath string
}

// Service is the in-process issue tracker. One per nexus.exe.
type Service struct {
	cfg Config
	db  *sql.DB
}

// New opens (or creates) ledger.db and returns a ready Service.
// Subsequent phases will run the embedded schema migration here.
func New(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("ledger.New: DBPath required")
	}

	dsn := "file:" + cfg.DBPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("ledger.New: open %s: %w", cfg.DBPath, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ledger.New: ping: %w", err)
	}

	return &Service{cfg: cfg, db: db}, nil
}

// Close releases the DB handle.
func (s *Service) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
