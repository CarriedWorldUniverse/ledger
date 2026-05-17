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

-- -------------------------------------------------------------------
-- Projects + sequence allocator
-- -------------------------------------------------------------------
-- One row per project (NEX, WAKE, OSS, ...). Each has its own
-- monotonic key sequence to produce stable PROJECT-N identifiers.
CREATE TABLE IF NOT EXISTS projects (
  key            TEXT PRIMARY KEY,                -- e.g. "NEX", "WAKE"
  name           TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  default_team   TEXT,                            -- FK to teams.name, nullable
  archived       INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Per-project monotonic counter. Updated transactionally on every
-- issue_create. Row exists for every row in projects.
CREATE TABLE IF NOT EXISTS project_sequences (
  project   TEXT PRIMARY KEY REFERENCES projects(key) ON DELETE CASCADE,
  next_seq  INTEGER NOT NULL DEFAULT 1
);

-- Teams of aspects. Named, operator-defined sets used as
-- assignee_team on issues.
CREATE TABLE IF NOT EXISTS teams (
  name           TEXT PRIMARY KEY,                -- e.g. "oss-nexus-dev"
  description    TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS team_members (
  team    TEXT NOT NULL REFERENCES teams(name) ON DELETE CASCADE,
  aspect  TEXT NOT NULL,                          -- aspect name from broker
  PRIMARY KEY (team, aspect)
);

INSERT OR IGNORE INTO schema_versions(version) VALUES (2);
