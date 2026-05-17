-- ledger schema. Idempotent — safe to run on every startup.
-- The DB lives at $NEXUS_DATA_DIR/ledger.db (parallel to broker.db).
-- See docs/spec.md for the design.
--
-- PRAGMAs (journal_mode=WAL, foreign_keys=ON, busy_timeout=5000) are
-- set via the DSN in service.go.

CREATE TABLE IF NOT EXISTS schema_versions (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO schema_versions(version) VALUES (1);
