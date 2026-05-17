# ledger

Aspect-first issue tracker for the nexus stack. Append-only timeline, immutable comments, per-type workflow validator (DoD-gated transitions), markdown-as-canonical artifact. Designed for AI aspects to consume + emit natively; operator review via a markdown-first dashboard.

**Status:** scaffold — design landed 2026-05-17, see `docs/spec.md` (mirrors `nexus/docs/2026-05-17-native-issue-tracker-spec.md`). Phase 0 implementation in progress against the foundation plan.

## Repo role

`ledger` is a Go module imported by `nexus.exe` and run in-process under the nexus supervisor. The DB (`ledger.db`) lives parallel to `broker.db` in the nexus data directory. Other consumers (cairn-as-repo-host, the contributor-facing UI) reach ledger via its REST surface.

Sibling repos in the stack: [`nexus`](https://github.com/CarriedWorldUniverse/nexus), [`interchange`](https://github.com/CarriedWorldUniverse/interchange), [`agora`](https://github.com/CarriedWorldUniverse/agora), [`bridle`](https://github.com/CarriedWorldUniverse/bridle), [`casket-go`](https://github.com/CarriedWorldUniverse/casket-go), [`cairn`](https://github.com/CarriedWorldUniverse/cairn).

## Design

- **Hybrid storage**: relational core (SQLite, own DB) for queries and backlog; markdown documents as the aspect-facing artifact materialised from row + timeline.
- **Per-issue-type workflows**: Epic (Brief → Sketch/Refined → In Development → Delivered); Story/Task/Bug/Subtask (To Do → In Progress → Blocked → In Review → Done → Cancelled).
- **Required DoD** on every issue type — markdown checklist, all items ticked before transition to Done/Delivered. No escape hatch on the validator; bypass is via aspect-mediated prerequisite undo, all audited.
- **Immutable comments**, append-only events table as the timeline source of truth.
- **Soft-guard permissions with full audit**: close-someone-else requires rationale; reporter immutable; no deletes (only Cancelled); operator-as-aspect impersonation explicitly supported with audit fields.
- **Push notifications to assignees + mentions; operator activity stream for everything else.**
- **Per-project monotonic sequences** (`NEX-1`, `WAKE-1`, etc.) with cross-project move + key aliases for forever-stable lookups.
- **External-source ingress**: GitHub issues filed via the [interchange](https://github.com/CarriedWorldUniverse/interchange) gateway, validated by operator-aspects (shadow / keel), final-accepted by operator before becoming a first-class ticket.

Full design: `docs/spec.md`. Implementation plan (Phases 0–2): `docs/plan-foundation.md`.

## Build

Requires Go 1.25+.

```sh
make build
make test
```

## License

Apache 2.0. See [`LICENSE`](./LICENSE).
