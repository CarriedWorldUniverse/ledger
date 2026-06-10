# ledger

**Aspect-first issue tracker for the nexus stack.** Markdown-canonical storage, append-only event timeline, immutable comments, per-issue-type workflow validator with required Definition-of-Done. Designed for AI aspects to consume + emit natively; reviewed and edited by the operator through a markdown-first dashboard.

Built to replace Jira as the canonical tracker for everything nexus hosts — nexus itself, agora, interchange, casket, bridle, cairn, vessel, the OSS stack, and the projects cairn-as-repo-host serves.

**Status:** implemented and under active development. The foundation phases from [`docs/plan-foundation.md`](./docs/plan-foundation.md) are in place — schema runner, materialised-view store, append-only event timeline, immutable comments, watchers, per-issue-type workflow + Definition-of-Done validator, search, links, cross-project moves, multi-tenancy/org scoping, and chat notifications — backed by an extensive test suite. The service ships as a standalone gRPC binary (`cmd/ledger`) behind interchange-gateway over mTLS, with a Containerfile and k3s deployment manifests under `deploy/k3s/`.

---

## Where ledger fits

`ledger` is a **standalone CWB service** (`cmd/ledger`), deployed as its own pod on the k3s cluster. It speaks **gRPC only, over mTLS**, behind [interchange](https://github.com/CarriedWorldUniverse/interchange) (the CWB gateway). Identity is **not** established by ledger: the gateway runs herald verification and injects `cwb-*` gRPC metadata (subject / org / kind / scopes), which ledger trusts because the gateway↔ledger hop is mTLS. The SQLite DB (`/var/lib/cwb/ledger.db`) is the service's own, with its own WAL and backup cadence. (The module can still be imported in-process by nexus via `go.mod`, but the deployed shape is the standalone pod.)

The gRPC surface is four services registered in `cmd/ledger/main.go`:

| Service | Covers |
|---|---|
| `IssueService` | create / get / update / transition / assign / claim / comment / link / watch / search / list |
| `ProjectService` | create / list / update / archive projects |
| `OrgService` | org-scoped administration; self-org purge |
| `AdminService` | platform/org administration |

```
  aspect ──gRPC (cwb-* metadata)──► interchange (CWB gateway, mTLS + herald verify)
                                          │  injects cwb-{subject,org,kind,scopes}
                                          ▼
                                   ledger  (cmd/ledger, gRPC-only over mTLS)
                                          │
                                          ▼
                                   /var/lib/cwb/ledger.db  (SQLite + FTS5)
```

Sibling repos: [`herald`](https://github.com/CarriedWorldUniverse/herald) · [`cairn`](https://github.com/CarriedWorldUniverse/cairn) · [`commonplace`](https://github.com/CarriedWorldUniverse/commonplace) · [`interchange`](https://github.com/CarriedWorldUniverse/interchange) · [`casket`](https://github.com/CarriedWorldUniverse/casket) · [`nexus`](https://github.com/CarriedWorldUniverse/nexus).

---

## Locked design (one-paragraph summary per topic)

### Storage shape

Hybrid: relational core in its own SQLite DB (`ledger.db`) for queries + backlog + cross-project linking; markdown documents as the aspect-facing artifact, materialised on demand from the row + the per-issue event timeline. Aspects never see SQL — they get a markdown document with YAML front-matter and read/write via MCP tools.

### Issue types + workflow

Five types: **Epic**, **Story**, **Task**, **Subtask**, **Bug**. Workflow is **per-issue-type**, hard-coded:

| Type | States |
|---|---|
| **Epic** | Brief → Sketch/Refined → In Development → Delivered (+ Cancelled from any) |
| **Story / Task / Bug / Subtask** | To Do → In Progress → Blocked → In Review → Done (+ Cancelled from any) |

The Epic flow deliberately mirrors how design work itself moves — brainstorm → plan → build — making the tracker reinforce the operating pattern.

### Definition of Done

**Required on every issue type.** Stored as a markdown checklist (`- [ ]` / `- [x]`). Minimum-acceptable DoD is one ticked item (e.g., "PR builds clean"). The workflow validator **rejects transitions to Done / Delivered** if any checklist item is unticked. No `force_transition` mode — bypass goes through aspect-mediated prerequisite undo (uncheck a DoD item with reason, then transition), all audited.

### Comments

**Immutable. Append-only.** A comment, once posted, cannot be edited or deleted. Correction is a new comment. This forces aspects to plan before they speak, removes funnel cache-invalidation logic, and keeps the timeline trustworthy as AI-readable history.

### Priority controls

Anyone can change priority (aspects do their own planning); every change lands in the timeline with reasoning. The operator can mark a ticket `priority-locked` to freeze; an aspect changing priority on a ticket it's actively working on pings the operator (catches self-serving promotion).

### Assignment + routing

Assignee is either a **specific aspect** OR a **named team** — never both. Teams are operator-defined sets (default engineering team `oss-nexus-dev` = {keel, shadow, anvil, plumb}). The "ready pool" per aspect / team is a ranked queue: ordered by priority, then age; blocked tickets fall out until the blocker clears.

### Permissions — soft guards, full audit

No heavy ACL. Soft guards enforce specific failure modes:

- Closing someone else's ticket requires a rationale field
- Reporter is immutable post-create
- No deletes — only `Cancelled` (no row ever leaves the table)
- Modifying DoD mid-flight on your own assigned ticket → operator notification
- Epic archival is operator-only
- **Operator-as-aspect impersonation** is a first-class path; every impersonated action is logged with both `actor: operator` and `acting_as: <aspect>` so the timeline shows what really happened

### Notifications

Two channels, both backed by the broker's canonical `HandleChatSend`:

- **Push (per-recipient)** — DM to assignees on assignment / team-queue arrival; to mentioned aspects on `@name`; to watchers on Blocked transitions
- **Operator activity stream (passive)** — one chat thread the operator can leave open; receives every transition / comment / link / attachment across all issues; no pings, just visible if you look

Run-loops fall back to pull via `ledger_list_my_updates` for catch-up.

### Issue keys + projects

Per-project monotonic sequences (`NEX-1`, `WAKE-1`, future `CAIRN-1`, `BRIDLE-1`, etc.). Cross-project moves supported via `ReassignProject` — allocates a new key in the destination, records the alias in `key_aliases` so lookups by the old key resolve forever.

### External-source ingress (GitHub issues, etc.)

GitHub issues filed against cairn-hosted projects flow inbound via [`interchange`](https://github.com/CarriedWorldUniverse/interchange) (gateway extension, NEX-140) → operator-aspect validators (shadow / keel) produce a replicate / replicate-with-edits / reject recommendation → operator final-accepts → first-class ledger ticket created with the external issue linked. The native ticket cannot close until the linked PR is merged + reviewed.

### Migration from Jira

Dual-write during foundation phases — every `nexus-jira-mcp` write tool mirrors to ledger after the Jira call succeeds (Jira stays authoritative until cutover). Cutover at the dedicated migration phase: freeze Jira writes, run a delta importer, flip MCP aliases, archive Jira read-only. Reverse-importer ready for 24h rollback window.

### Search

Structured-filter primary (typed object: projects, types, statuses, priorities, assignee, team, reporter, parent, order, limit) compiled to safe parameterised SQL. Optional `where: <jql>` escape hatch deferred to a later phase.

### Attachments + file references

No native blob storage in ledger. Attachments are `nexus://` references resolved by a separate [nexus file store](https://github.com/CarriedWorldUniverse/nexus) (NEX-139) — size-driven hybrid: small files stream through nexus, large files use direct-write to a host-local mount with sha256-verified commit.

### Backups

Native `nexus snapshot` command produces a coherent tarball — brief write-pause, atomic SQLite `.backup` per DB (broker.db + ledger.db + file-store metadata), hardlinked blob root, manifest with timestamp + hashes, dropped on Drive. Restore is documented from the manifest.

---

## What runs where

| Component | Where |
|---|---|
| `ledger` service (`cmd/ledger`) | Standalone pod on the k3s cluster |
| `ledger.db` SQLite (+ FTS5) | Service-local volume at `/var/lib/cwb/ledger.db` |
| gRPC API (Issue / Project / Org / Admin) | gRPC-only, served by `cmd/ledger` over mTLS |
| Identity | `cwb-*` gRPC metadata injected by `interchange` after herald verification |
| Edge / auth | `interchange` (the CWB gateway) fronts ledger; clients reach it through there |
| Atomic claim | Single-call `ClaimIssue` (`claim.go`) — replaces the old assign-then-read race |
| Full-text search | FTS5 over issue bodies + comment bodies (`search.go`) |
| Contributor-facing web UI | Future, separate (NEX-164) |
| External-event ingress | Through `interchange` (NEX-140 + NEX-163) |

---

## Agent verbs

The gRPC `IssueService` / `ProjectService` back these aspect-facing verbs (surfaced to aspects through an MCP client; shown in the `ledger.*` form):

| Tool | Purpose |
|---|---|
| `ledger.create` | Create issue. Required: project, type, summary, definition_of_done, reporter. |
| `ledger.get` | Markdown document (aspect-facing). |
| `ledger.get_raw` | Structured JSON (dashboard / sync). |
| `ledger.update` | Patch fields (summary, description, DoD, priority, parent). |
| `ledger.transition` | Move status; workflow + DoD validated. |
| `ledger.assign` | Set assignee aspect OR team. |
| `ledger.comment` | Append immutable comment. |
| `ledger.link` | Add internal link (blocks / relates / parent / duplicates / subtask-of). |
| `ledger.unlink` | Remove an internal link. |
| `ledger.link_external` | Add external link + sync policy. |
| `ledger.link_artifact` | Attach a `nexus://` reference. |
| `ledger.watch` / `ledger.unwatch` | Manage own watcher row. |
| `ledger.search` | Structured filter. |
| `ledger.list_my` | Caller's assignments (direct + team). |
| `ledger.list_ready` | Top of the ready pool. |
| `ledger.list_my_updates` | Since-timestamp diff for run-loops. |
| `ledger.reassign_project` | Cross-project move; allocates new key + alias. |
| `ledger.validate_external_inbound` | Operator-aspect tool: validate an inbound external issue and emit a replicate / edit / reject recommendation. |

---

## Dependencies

The full ledger feature set spans multiple sibling epics in the NEX-137 family:

- **NEX-138** — autonomous run-loop primitive (headless `/goal` equivalent). Ledger is loop-callable from day one; this epic delivers the loop runner.
- **NEX-139** — nexus file store with portable `nexus://` references. Required for attachments (Phase 4).
- **NEX-140** — interchange-hosted webhook intake. Required for external-source ingress (Phase 3).
- **NEX-163** — nexus↔interchange protocol (spec v4, WS transport, auto-subscribe, receiver-typed envelopes). Blocks NEX-140.
- **NEX-141** — unified MCP wiring across funnel / bridle / agora runtimes. Aspects can't reliably call `nexus-ledger-mcp` until this lands consistently.
- **NEX-164** — cairn contributor-facing web UI on the ledger backend.

---

## Build

Requires Go 1.26+.

```sh
make build   # go build ./...
make test    # race-enabled tests
make vet     # go vet ./...
```

The standalone gRPC service binary lives at `cmd/ledger` (built via `go build ./...`); `cmd/ledger/Containerfile` and the manifests under `deploy/k3s/` package it for deployment. The module can also be imported directly by [`nexus`](https://github.com/CarriedWorldUniverse/nexus) via `go.mod` for in-process use.

---

## Implementation plan

The original foundation plan ([`docs/plan-foundation.md`](./docs/plan-foundation.md)) sequenced the store / events / comments / watchers work as in-process Phases 0-2. That core is built, and the service has since been lifted to the standalone gRPC pod described above (issues, projects, teams, links, search, atomic claim, org tenancy). Remaining future work — external sync, attachments, dashboard, Jira cutover — is sequenced against NEX-139 / NEX-140 / NEX-163.

---

## License

Apache 2.0. See [`LICENSE`](./LICENSE).

## Reference

- Design spec: [`docs/spec.md`](./docs/spec.md)
- Foundation plan: [`docs/plan-foundation.md`](./docs/plan-foundation.md)
- Origin epic: [`NEX-137`](https://carriedworlduniverse.atlassian.net/browse/NEX-137)
- Brainstorm + decision log: NEX-137 comment thread, 2026-05-17
