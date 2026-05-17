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

-- -------------------------------------------------------------------
-- Issues
-- -------------------------------------------------------------------
-- One row per ticket. Either assignee_aspect OR assignee_team is set
-- (not both); NULL on both = unassigned.
CREATE TABLE IF NOT EXISTS issues (
  key                  TEXT PRIMARY KEY,                  -- e.g. "NEX-137"
  project              TEXT NOT NULL REFERENCES projects(key),
  seq                  INTEGER NOT NULL,                  -- denormalised for clarity
  type                 TEXT NOT NULL,                     -- Epic|Story|Task|Subtask|Bug
  status               TEXT NOT NULL,
  summary              TEXT NOT NULL,
  description          TEXT NOT NULL DEFAULT '',
  definition_of_done   TEXT NOT NULL,                     -- required, can be minimal
  priority             TEXT NOT NULL DEFAULT 'Medium',    -- Lowest|Low|Medium|High|Highest
  priority_locked      INTEGER NOT NULL DEFAULT 0,
  assignee_aspect      TEXT,
  assignee_team        TEXT REFERENCES teams(name) ON DELETE SET NULL,
  reporter             TEXT NOT NULL,                     -- immutable post-create
  parent_key           TEXT REFERENCES issues(key) ON DELETE SET NULL,
  created_at           TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at           TEXT NOT NULL DEFAULT (datetime('now')),
  CHECK (assignee_aspect IS NULL OR assignee_team IS NULL)  -- at most one
);

CREATE INDEX IF NOT EXISTS idx_issues_project_status ON issues(project, status);
CREATE INDEX IF NOT EXISTS idx_issues_assignee_aspect ON issues(assignee_aspect) WHERE assignee_aspect IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_issues_assignee_team ON issues(assignee_team) WHERE assignee_team IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_issues_parent ON issues(parent_key) WHERE parent_key IS NOT NULL;

INSERT OR IGNORE INTO schema_versions(version) VALUES (3);

-- key_aliases maps old issue keys to current canonical keys after
-- cross-project moves. Lookups by old key continue to resolve forever.
CREATE TABLE IF NOT EXISTS key_aliases (
  old_key   TEXT PRIMARY KEY,
  new_key   TEXT NOT NULL REFERENCES issues(key) ON DELETE CASCADE,
  moved_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_key_aliases_new ON key_aliases(new_key);

INSERT OR IGNORE INTO schema_versions(version) VALUES (4);

-- -------------------------------------------------------------------
-- Events (timeline)
-- -------------------------------------------------------------------
-- One row per timeline event. `kind` discriminates; `payload` JSON
-- holds kind-specific fields. Append-only — never updated.
CREATE TABLE IF NOT EXISTS events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  issue_key   TEXT NOT NULL REFERENCES issues(key) ON DELETE CASCADE ON UPDATE CASCADE,
  seq         INTEGER NOT NULL,                          -- per-issue ordering
  kind        TEXT NOT NULL,                             -- comment|transition|field_change|...
  actor       TEXT NOT NULL,
  at          TEXT NOT NULL DEFAULT (datetime('now')),
  payload     TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_events_issue ON events(issue_key, seq);
CREATE INDEX IF NOT EXISTS idx_events_at ON events(at);

INSERT OR IGNORE INTO schema_versions(version) VALUES (5);
