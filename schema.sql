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

-- -------------------------------------------------------------------
-- Watchers
-- -------------------------------------------------------------------
-- One row per (issue, aspect) pair. Aspects watching an issue receive
-- notifications on blocker transitions (see NEX-160 broker-backed
-- notifier). Idempotent: INSERT OR IGNORE prevents duplicates.
CREATE TABLE IF NOT EXISTS watchers (
  issue_key  TEXT NOT NULL REFERENCES issues(key) ON DELETE CASCADE,
  aspect     TEXT NOT NULL,
  since      TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (issue_key, aspect)
);

INSERT OR IGNORE INTO schema_versions(version) VALUES (6);

-- -------------------------------------------------------------------
-- Organisations + users (multi-tenancy, v7)
-- -------------------------------------------------------------------
-- Orgs own projects. Every project belongs to exactly one org.
-- The default "nexus" org wraps all existing projects.
CREATE TABLE IF NOT EXISTS organisations (
  slug       TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Users are identities that can authenticate to the ledger.
-- kind: human (operator, contributor) or ai (aspect).
CREATE TABLE IF NOT EXISTS users (
  id         TEXT PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('human', 'ai')),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Per-org role for each user.
CREATE TABLE IF NOT EXISTS org_members (
  org        TEXT NOT NULL REFERENCES organisations(slug) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'member', 'viewer')),
  joined_at  TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (org, user_id)
);

-- Backfill default org.
INSERT OR IGNORE INTO organisations(slug, name) VALUES ('nexus', 'Nexus');

-- Backfill known aspects + operator as users.
INSERT OR IGNORE INTO users(id, kind) VALUES ('jacinta', 'human');
INSERT OR IGNORE INTO users(id, kind) VALUES ('shadow',  'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('keel',    'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('anvil',   'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('plumb',   'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('forge',   'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('harrow',  'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('maren',   'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('verity',  'ai');
INSERT OR IGNORE INTO users(id, kind) VALUES ('wren',    'ai');

-- Backfill org memberships.
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'jacinta', 'owner');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'shadow',  'admin');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'keel',    'member');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'anvil',   'member');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'plumb',   'member');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'forge',   'member');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'harrow',  'member');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'maren',   'member');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'verity',  'member');
INSERT OR IGNORE INTO org_members(org, user_id, role) VALUES ('nexus', 'wren',    'member');

INSERT OR IGNORE INTO schema_versions(version) VALUES (7);

-- Add organisation column to projects. FK integrity is enforced at the
-- application layer; SQLite ALTER TABLE ADD COLUMN does not enforce
-- foreign-key constraints on the new column.
ALTER TABLE projects ADD COLUMN organisation TEXT NOT NULL DEFAULT 'nexus';

INSERT OR IGNORE INTO schema_versions(version) VALUES (8);

-- -------------------------------------------------------------------
-- Issue links (v9) — explicit edges between issues distinct from
-- parent_key (which is the epic-child hierarchy). v1 supports:
--
--   'blocks'     from_key cannot be Done until to_key is terminal.
--                Load-bearing for the orchestration scheduler — the
--                "next unblocked task" computation queries this.
--   'relates-to' editorial cross-reference; no orchestration effect.
--
-- Future types (duplicates, etc.) are validated at the application
-- layer rather than via a CHECK constraint — adding a new value to
-- a SQLite CHECK requires recreating the table, which is friction
-- not worth paying when the validation is one if-statement in code.
--
-- FK ON UPDATE CASCADE so links survive cross-project moves (where
-- the issue's key is rewritten — see move.go).
CREATE TABLE IF NOT EXISTS issue_links (
  from_key   TEXT NOT NULL REFERENCES issues(key) ON DELETE CASCADE ON UPDATE CASCADE,
  to_key     TEXT NOT NULL REFERENCES issues(key) ON DELETE CASCADE ON UPDATE CASCADE,
  type       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  created_by TEXT NOT NULL,
  PRIMARY KEY (from_key, to_key, type)
);

-- "Who blocks me?" / "What links to me?" hot path — IsBlocked + the
-- scheduler's unblock-fanout both filter by to_key + type.
CREATE INDEX IF NOT EXISTS idx_issue_links_to_type ON issue_links(to_key, type);

INSERT OR IGNORE INTO schema_versions(version) VALUES (9);

-- -------------------------------------------------------------------
-- Project-scoped teams (v10)
-- -------------------------------------------------------------------
-- v1 simpler shape: teams gain a `project` column (NOT NULL, every
-- team belongs to exactly one project). teams.name remains the
-- primary key, so global uniqueness of team names is preserved AND
-- existing FKs from issues.assignee_team / team_members continue to
-- work unchanged. The trade-off is that two projects can't both have
-- a team literally called "backend-team" — operator uses scoped
-- naming like "nex-backend" / "oss-backend". A future evolution to
-- composite (project, name) PK is left for a follow-up if the
-- shared-name pain becomes real.
--
-- The orchestration scheduler reads teams.project to verify a team-
-- assigned ticket's assignee_team is in the SAME project as the
-- ticket; cross-project team assignment is rejected at the app
-- layer (see AssignIssue + team-scope checks in teams.go).
--
-- Backfill: for each existing team, pick the project most-referenced
-- by existing issues (via assignee_team) or projects (default_team),
-- falling back to 'nexus' (the bootstrap org's default project) when
-- nothing references it. Operator can rebalance via UpdateTeam later
-- if the auto-pick is wrong.
ALTER TABLE teams ADD COLUMN project TEXT NOT NULL DEFAULT 'nexus';

UPDATE teams SET project = COALESCE(
  (SELECT i.project FROM issues i WHERE i.assignee_team = teams.name LIMIT 1),
  (SELECT p.key FROM projects p WHERE p.default_team = teams.name LIMIT 1),
  'nexus'
);

-- Hot path: scheduler resolves team's project to validate cross-
-- project assignment + to filter team listings to a single project.
CREATE INDEX IF NOT EXISTS idx_teams_project ON teams(project);

INSERT OR IGNORE INTO schema_versions(version) VALUES (10);

-- v11: external_refs holds a JSON array of {tracker, key, url, description?}
-- objects so ledger issues can advertise their provenance back to source
-- trackers (the Jira ticket that drove this work, the GitHub issue, etc).
-- Aspects dispatched on a ledger issue can pull the source ticket directly
-- via the URL field — no name-resolution dance required. JSON-encoded
-- column rather than a separate table because the shape is small +
-- caller-controlled + per-issue, and the existing single-row SELECT
-- carries it without joins.
ALTER TABLE issues ADD COLUMN external_refs TEXT NOT NULL DEFAULT '[]';

INSERT OR IGNORE INTO schema_versions(version) VALUES (11);

-- -------------------------------------------------------------------
-- Full-text search (v12)
-- -------------------------------------------------------------------
-- FTS5 virtual table powering Service.FindByText + the issue.find_by_text
-- MCP tool (NEX-323). Indexes the issue's natural-language text — summary,
-- description, definition_of_done — and each comment body. Aspects ask
-- "what mentions DeepSeek?" instead of having to know which fields hold
-- what.
--
-- One row per searchable item rather than one row per issue:
--   source='issue'   → issue body (summary || description || DoD)
--   source='comment' → a single comment's body
-- This avoids the rebuild-the-aggregate-on-every-comment dance and lets
-- triggers stay one-statement-per-mutation. Search dedupes to issue keys
-- in the application layer.
--
-- issue_key + source + event_id are UNINDEXED — they don't participate
-- in MATCH; they're stored so we can dedupe + look up the parent issue
-- without joining back through rowids.
CREATE VIRTUAL TABLE IF NOT EXISTS issue_search USING fts5(
  text,
  issue_key UNINDEXED,
  source UNINDEXED,
  event_id UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);

-- Keep issue_search current as issues + comments change. Triggers fire
-- after the source row has committed locally so the FTS update is part
-- of the same outer transaction.
CREATE TRIGGER IF NOT EXISTS issue_search_issues_ai
AFTER INSERT ON issues BEGIN
  INSERT INTO issue_search(text, issue_key, source, event_id)
  VALUES (NEW.summary || ' ' || NEW.description || ' ' || NEW.definition_of_done,
          NEW.key, 'issue', NULL);
END;

CREATE TRIGGER IF NOT EXISTS issue_search_issues_au
AFTER UPDATE OF summary, description, definition_of_done ON issues BEGIN
  UPDATE issue_search
  SET text = NEW.summary || ' ' || NEW.description || ' ' || NEW.definition_of_done
  WHERE issue_key = NEW.key AND source = 'issue';
END;

CREATE TRIGGER IF NOT EXISTS issue_search_issues_ad
AFTER DELETE ON issues BEGIN
  DELETE FROM issue_search WHERE issue_key = OLD.key;
END;

-- ON UPDATE CASCADE on issues.key (move.go cross-project moves) walks
-- the FK chain to events.issue_key etc., but FTS rows aren't FKed —
-- mirror the rename explicitly so search keeps resolving to the new key.
CREATE TRIGGER IF NOT EXISTS issue_search_issues_key_update
AFTER UPDATE OF key ON issues BEGIN
  UPDATE issue_search SET issue_key = NEW.key WHERE issue_key = OLD.key;
END;

CREATE TRIGGER IF NOT EXISTS issue_search_events_ai
AFTER INSERT ON events WHEN NEW.kind = 'comment' BEGIN
  INSERT INTO issue_search(text, issue_key, source, event_id)
  VALUES (COALESCE(json_extract(NEW.payload, '$.body'), ''),
          NEW.issue_key, 'comment', NEW.id);
END;

-- Backfill: idempotent because each row has a NOT EXISTS guard against
-- the matching natural-key tuple. On first apply this populates from
-- pre-FTS data; on subsequent applies it's a no-op.
INSERT INTO issue_search(text, issue_key, source, event_id)
SELECT summary || ' ' || description || ' ' || definition_of_done, key, 'issue', NULL
FROM issues
WHERE NOT EXISTS (
  SELECT 1 FROM issue_search WHERE issue_key = issues.key AND source = 'issue'
);

INSERT INTO issue_search(text, issue_key, source, event_id)
SELECT COALESCE(json_extract(payload, '$.body'), ''), issue_key, 'comment', id
FROM events
WHERE kind = 'comment'
  AND NOT EXISTS (
    SELECT 1 FROM issue_search WHERE source = 'comment' AND event_id = events.id
  );

INSERT OR IGNORE INTO schema_versions(version) VALUES (12);

-- -------------------------------------------------------------------
-- Project workflows (v13)
-- -------------------------------------------------------------------
-- One workflow per project, stored as protojson so the cwb-proto
-- Workflow contract remains the storage contract. Projects without a
-- stored workflow use the in-code defaultWorkflow seed.
CREATE TABLE IF NOT EXISTS workflows (
  project       TEXT PRIMARY KEY REFERENCES projects(key) ON DELETE CASCADE,
  workflow_json TEXT NOT NULL,
  updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO schema_versions(version) VALUES (13);

-- -------------------------------------------------------------------
-- Issue skills / capability tags (v14)
-- -------------------------------------------------------------------
-- Generic skill/capability tags on an issue (NEX-666). JSON text array,
-- mirrors external_refs. Workers pull ready issues whose required skills
-- they serve (ListReady skill filter). ALTER ADD COLUMN is idempotent via
-- the tolerate-duplicate-column migration path (see schema.go).
ALTER TABLE issues ADD COLUMN skills TEXT NOT NULL DEFAULT '[]';

INSERT OR IGNORE INTO schema_versions(version) VALUES (14);
