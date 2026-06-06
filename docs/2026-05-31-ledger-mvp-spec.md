# ledger — CWB MVP spec

**Status:** draft for approval · 2026-05-31
**Goal:** stand ledger up as a CWB product — a herald-identified HTTP issue-tracker behind interchange-gateway, exposing the daily-driver agent verbs — so aspects (and the conformance suite) track issues over HTTP with a herald token instead of Jira.
**Why now:** ledger is the **issues** leg of the CWB MVP agent loop (auth+git+issues+knowledge — see `cwb-conformance/docs/2026-05-31-cwb-mvp-definition.md`). It's the team's Jira replacement for the nexus network's own use. The work is mostly *expose + herald-gate + wire what already exists*, not greenfield: ledger today is a flat-package Go library embedded in nexus, with issue CRUD, transitions, comments, links, FTS search, multi-tenancy (`tenancy.go`), events, and a polling updates endpoint already built and tested (~Phase 2 of the native-tracker plan, NEX-137).

This is the **agent-loop MVP** cut. Human UI, the cutover-from-Jira migration tooling, and the SSE/webhook push capability are out of scope here (§7), each their own track.

---

## 1. The one-paragraph architecture

Ledger gets a thin standalone server (`cmd/ledger`) wrapping the existing library, deployed as a CWB product in the `cwb` k3s namespace behind interchange-gateway (route `/ledger`). It does **not** join the nexus WebSocket bus — it's an HTTP/REST service reached with a herald token, like every CWB pillar (`project_cwb_http_native`). Auth follows the gateway model: interchange verifies the herald EdDSA token and injects `X-CWB-{Subject,Org,Kind,Scopes}`; ledger trusts that identity because the gateway→ledger hop is **mTLS** (the gateway is cryptographically the caller — `project_cwb_tls_everywhere`), and ledger is reachable only over that path. The herald `sub` becomes the issue **actor** (every mutation tagged to the calling agent), `org` the tenant scope (ledger is already multi-tenant), `scope` the permission. The existing HS256 self-auth stays for the in-nexus embedded path until nexus migrates (NEX-382). The agent daily-driver verbs (`my` / `ready` / atomic `claim`) are wired to REST on top of library functions that mostly already exist; ledger's events feed the fast-follow SSE/webhook push capability.

```
  aspect ──herald token──► interchange-gateway ──mTLS, X-CWB-*──► cmd/ledger ──► ledger lib + SQLite
   (HTTP, no WS-bus)         (verifies + injects)                 (actor=sub, tenant=org)
```

---

## 2. Scope — what's IN

1. **Standalone server.** New `cmd/ledger` HTTP server wrapping the existing `ledger` library untouched. Env-driven config (addr, DB path, herald issuer/JWKS for any direct verification, gateway-trust mode).
2. **Deploy as a CWB product.** Containerfile (static Go on scratch, mirroring herald/cairn) + k3s manifests in `cwb` ns: Deployment, ClusterIP Service, SQLite on a `local-path` PVC, gateway route `/ledger → ledger.cwb.svc`. mTLS via the service mesh (or internal certs) on the gateway↔ledger hop.
3. **Herald identity via the gateway.** Read `X-CWB-{Subject,Org,Kind,Scopes}` injected by interchange (which ran herald verification). Map: `Subject`→actor, `Org`→tenant, `Scopes`→permission. Trust rests on the **mTLS-authenticated gateway** + ledger being reachable only over that hop. (Optional defense-in-depth: ledger may also run `heraldauth` directly; not required for MVP.)
4. **Actor-tagging.** Every mutation (create/claim/comment/transition/assign) records the calling agent (`X-CWB-Subject` = herald `sub`) as the actor — issues, comments, and events are attributed to the individual agent identity, not a flat aspect string. (Addresses the NEX-235 "tracker aware of individual identities" concern.)
5. **Daily-driver agent verbs (REST).** Wire the library functions that exist + build the one that doesn't:
   - `GET /api/issues/my` → `ListMy` *(lib exists, search.go)*
   - `GET /api/issues/ready` → `ListReady` *(lib exists, search.go)*
   - `POST /api/issues/{key}/claim` → **atomic assign+transition in one transaction** *(new — today it's two non-atomic calls)*
   - existing + retained: `POST /api/issues` (create), `GET/PATCH /api/issues/{key}`, `POST /api/issues/{key}/{transition|assign|comments}`, `/api/issues/search`, `/api/issues/search/text`, `/api/issues/updates`, `/api/projects`.
6. **Event source for push.** Keep `events.go` + `/api/issues/updates` (the run-loop polling pull — how aspects stay current at MVP). These events are the source the fast-follow SSE/webhook push capability (`project_cwb_live_push`) consumes — design-accommodated, not built here.
7. **TLS everywhere.** No plain-HTTP hop. Public TLS at Cloudflare, Full-strict to the origin, **mTLS gateway↔ledger** (`project_cwb_tls_everywhere`). dMon's current plain-HTTP is a dev-only interim.

---

## 3. Auth + identity model

| Herald claim (via `X-CWB-*`) | Ledger use |
|---|---|
| `Subject` (agent id) | the **actor** on every mutation; attribution in issues/comments/events |
| `Org` | tenant scope — ledger is already multi-tenant (`tenancy.go`); MVP is single-org but the mechanism stands |
| `Scopes` | permission gate — map herald scopes (e.g. `issue:read`/`issue:write`/`issue:claim`) to ledger actions |
| `Kind` | `agent` vs `human` (informational at MVP) |

- **Trust basis:** the gateway→ledger hop is mTLS; ledger accepts `X-CWB-*` only from the authenticated gateway and is not otherwise reachable. This is what makes header-trust safe (a plain ClusterIP would let any in-cluster pod forge the headers).
- **HS256 retained** for the existing in-nexus embedded path (`auth.go`); the two coexist until nexus migrates to herald (NEX-382). A small middleware selects the auth source by deployment mode.
- **Scope vocabulary** (`issue:read`/`write`/`claim`/`admin`) is ledger's slice of the cross-service herald scope set; pin the exact strings in the plan, aligned with the other pillars.

---

## 4. Atomic claim (the one genuinely-new piece)

`POST /api/issues/{key}/claim` performs **assign-to-caller + transition-to-in-progress in a single transaction**, gated on `issue:claim`. Today this is two non-atomic REST calls (assign, then transition) — racy if two agents claim concurrently. The atomic version:
- in one DB transaction: verify the issue is claimable (status allows it, not already claimed by another), set assignee = `X-CWB-Subject`, transition to the configured "claimed/in-progress" state, append a claim event.
- returns 409 if already claimed by a different agent (lost the race); 200 + the updated issue on success.

All other verbs reuse existing library behaviour; only claim needs new transactional code.

---

## 5. Data model

No schema change for the MVP — ledger's existing schema (issues, comments, events, links, projects, teams, watchers, tenancy) already supports everything. Actor-tagging uses the existing actor/author fields, populated from `X-CWB-Subject` instead of a flat aspect string. (If actor fields are currently free-string aspect names, the plan confirms they accommodate herald agent ids — they should, being opaque strings.)

---

## 6. API surface (delta from today)

New:
- `GET /api/issues/my` — caller's assigned/owned issues (`ListMy`).
- `GET /api/issues/ready` — claimable/ready issues for the caller (`ListReady`).
- `POST /api/issues/{key}/claim` — atomic claim (§4).

Changed:
- All endpoints behind the gateway read identity from `X-CWB-*` (was: HS256 token resolution). HS256 path retained for embedded use.

Unchanged: create/get/patch/transition/assign/comments/search/search-text/updates/projects.

---

## 7. Explicitly OUT of scope (each its own track)

- **Ledger human UI** (NEX-223) — operator oversees via shadow/dashboard at MVP.
- **SSE/webhook live-push** (`project_cwb_live_push`; NEX-44 SSE + NEX-206 webhooks) — own capability spec, fast-follow; ledger is the event *source*, the stream layers on later.
- **nexus-issue-mcp shim** (NEX-153) — the aspect-facing MCP tools are a thin separate MCP→REST wrapper over this API; not in the ledger spec.
- **Jira→ledger cutover tooling** — the bulk historical importer + dual-write drift report (cutover-gaps doc, "Bucket B") are *Jira-migration* concerns, separate from the agent-loop MVP.
- **HS256→herald migration of nexus itself** (NEX-382) — nexus keeps HS256 until then; this spec only herald-gates the gateway-facing surface.
- **Labels/components** — ledger lacks a schema for them (cutover-gaps gap); decide add-or-drop later, not MVP.

---

## 8. Build sequence (for the implementation plan)

1. **Spec sign-off** (this doc).
2. **`cmd/ledger` server** — thin HTTP wrapper over the library; env config; Containerfile; build-green.
3. **Gateway-identity middleware** — read + trust `X-CWB-*`; map to actor/org/scope; retain HS256 for embedded mode; reject if the identity headers are absent on the gateway path.
4. **Wire `my` + `ready`** — REST routes over the existing `ListMy`/`ListReady` libs, scoped to the caller.
5. **Atomic `claim`** — transactional assign+transition+event; 409 on lost race (§4).
6. **Actor-tagging** — populate actor/author/event-actor from `X-CWB-Subject` across mutations.
7. **k3s deploy** — manifests in `cwb` ns; ClusterIP; PVC; gateway route; mesh-mTLS on the hop.
8. **cwb-conformance ledger layer** — exercise the verbs + actor-tagging + cross-org isolation through the gateway (the conformance ledger leg of the agent loop).

**DoD:** an aspect with a herald token, through the gateway over mTLS, lists its issues (`my`/`ready`), claims one atomically (concurrent claim → 409), comments, and transitions — every mutation attributed to that agent — and the cwb-conformance ledger layer + journey exercise it green. No plain-HTTP hop anywhere in the path.

---

## 9. Open questions for the plan (small, non-blocking)

- **Exact herald scope strings** for ledger (`issue:read`/`write`/`claim`/`admin`?) — pin cross-pillar in the plan.
- **Gateway-trust vs heraldauth-direct** — MVP trusts `X-CWB-*` over mTLS; whether to *also* run heraldauth in ledger for defense-in-depth is a plan call (lean: not for MVP).
- **Claimed/in-progress target state** for atomic claim — read from the per-type workflow config (ledger has per-type state machines); confirm the canonical "claimed" transition per workflow.
- **Actor field types** — confirm existing actor/author/event columns are opaque strings that accept herald agent ids (expected yes).
- **mTLS mechanism** — service mesh (Linkerd) vs cert-manager internal certs; platform-level decision (`project_cwb_tls_everywhere`), pinned at deploy, shared across pillars.
