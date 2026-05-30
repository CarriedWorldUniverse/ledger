# Ledger ↔ Jira comparison & cutover gaps

**Date:** 2026-05-29
**Status:** Recovered from dropped Claude Code session `ce653f96` (lost to the thinking-block resume regression — see CC issue #63322). This is the analysis that fed the NEX-341..346 spec tickets.
**Method:** read the ledger spec + two deep-reads, reconciled against live code on branch state at the time.

---

## 1. Ledger: built vs specced

**The README lies.** It says "scaffold / Phase 0 pending." Reality: ~164 tests across 25 files, roughly **Phase 2 complete** of a 7-phase plan. The core is real and tested.

| Domain | State | Notes |
|---|---|---|
| Issue CRUD | ✅ full | all fields except labels/components (see gaps) |
| 5 types + per-type workflow | ✅ full | Epic vs Story/Task/Bug/Subtask state machines |
| Transition validator + DoD gate | ✅ full | rejects →Done with unticked DoD; no force-transition |
| Comments (immutable) | ✅ full | append-only |
| Event timeline | ✅ full | every mutation audited |
| Search: structured + FTS5 | ✅ full | `find_by_text` BM25 over issues **+ comments** |
| Projects + key allocation + cross-project move | ✅ full | `key_aliases` for forever-lookup (move.go) |
| Teams + routing (aspect OR team) | ✅ full | beyond Jira MCP's currentUser-only |
| Watchers | ✅ full | |
| Markdown materialisation | ✅ full | the aspect-facing doc |
| Mentions + notifications | ✅ full | chat ping via broker |
| Multi-tenancy (orgs/users/JWT) | ✅ full | **code ahead of spec** — not even in the spec |
| External refs | ⚠️ JSON-only | stored, but **no sync engine, no sync_policy logic** |
| Priority-locked | ⚠️ column only | no freeze/ping enforcement |
| Link vocabulary | ⚠️ partial | only `blocks` + `relates-to`; spec wants duplicates/subtask-of |
| External-sync engine / attachments / dashboard | ❌ absent | deferred behind NEX-140 / NEX-139 / separate UI |

## 2. Jira (what we actually use) vs ledger

**Key finding: Jira already dual-writes into ledger today.** The `nexus-jira-mcp` mirrors create/transition/assign/link into ledger via `native.go` — so ledger has live data flowing in right now. That's the spec's Phase-1 shim, already partly live.

| Jira tool | Ledger equivalent | Verdict |
|---|---|---|
| `jira.create` | `issue.create` | ✅ parity+ (DoD required) |
| `jira.get` | `issue.get` (md) / `get_raw` | ✅ better (markdown-native) |
| `jira.comment` | `issue.comment` | ✅ parity+ (immutable) |
| `jira.update_status` | `issue.transition` | ✅ parity+ (workflow+DoD validated) |
| `jira.update` | `issue.update` | ⚠️ **no labels/components** |
| `jira.link` / `unlink` / `list_links` | `issue.link` / `unlink` / `list_links` | ⚠️ fewer link types |
| `jira.search` (JQL) | `issue.search` + `find_by_text` | ✅ no raw-JQL, but FTS5 is better for AI |
| `jira.list_my_issues` | — | ❌ **not wired** (lib exists) |
| `jira.list_ready` | — | ❌ **not wired** (lib exists) |
| `jira.claim` (atomic assign+transition) | — | ❌ **no atomic claim** |
| `jira.complete` (transition+comment) | — | ❌ no atomic convenience |
| `jira.delete` | — | ✅ intentional (no deletes, only Cancelled) |
| — | `issue.list_my_updates` (run-loop pull) | ledger-only |
| — | watchers, teams, projects, multi-tenancy | ledger-only |

## 3. Gaps, prioritized for cutover

**A — daily-driver parity (cheap, do first):** things aspects hit every turn that ledger can't yet do as a one-call MCP tool.
- `issue.list_my_issues` + `issue.list_ready` — the "get next ticket" loop. **Lib functions already exist** (search.go ListMy/ListReady) — just need REST + MCP wiring. Biggest bang-for-buck.
- `issue.claim` — atomic assign+transition (today it's two non-atomic calls).
- **Labels/components** — ledger has *no schema for them at all*. We use labels. Decision needed: add them, or drop the concept.

**B — the actual cutover blockers (none built):**
- **Bulk Jira→ledger historical importer** — dual-write only captures *new* actions; the back-catalogue of existing NEX-* tickets needs a one-time import. This is the migration-day gap.
- **Divergence/drift report** — a cutover-gate requirement; nothing measures dual-write fidelity.
- **Operator dashboard** — the cutover gate is "operator triages a day without Jira." No review UI exists (separate repo, NEX-164).
- Dual-write holes: `jira.unlink` doesn't mirror; cross-project creates + Duplicate/Cloners links skip.

**C — deferred by dependency (not cutover blockers):**
- External GitHub sync engine → NEX-140 / interchange
- Attachments (`nexus://`) → NEX-139
- Autonomous run-loop → NEX-138

## 4. Migration-timing read

The weekend job is moving the broker *host* Windows→Linux. Jira is external cloud — it survives the host move untouched. Ledger is in-process, so it *moves with* `nexus.exe`. So the host migration **doesn't force** the Jira→ledger cutover either way — they're independent. The one thing worth doing before the move is confirming `ledger.db` is in the snapshot/backup path so it travels with the host.

**Effort summary:** Bucket A is ~a day of wiring (the lib is already there). Bucket B is the real project — importer + dashboard + drift report — and that's what stands between "ledger has live data" and "Jira is gone."
