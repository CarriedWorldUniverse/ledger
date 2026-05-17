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
	// Notifier is optional; defaults to a no-op. Wire a BrokerNotifier
	// (from the nexus side) to enable chat notifications.
	Notifier Notifier
}

// Service is the in-process issue tracker. One per nexus.exe.
type Service struct {
	cfg    Config
	db     *sql.DB
	notify Notifier
}

// New opens (or creates) ledger.db, applies the embedded schema, and
// returns a ready Service. schema.sql is idempotent, so applySchema runs
// on every call.
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
	if err := applySchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	notify := cfg.Notifier
	if notify == nil {
		notify = nopNotifier{}
	}
	return &Service{cfg: cfg, db: db, notify: notify}, nil
}

// Close releases the DB handle.
func (s *Service) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
