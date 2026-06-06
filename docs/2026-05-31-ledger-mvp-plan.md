# ledger MVP — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand ledger up as a CWB product — a herald-identified HTTP issue-tracker behind interchange-gateway exposing the daily-driver agent verbs — the issues leg of the CWB agent loop. Mostly expose + gate + wire what exists; the only new code is atomic claim.

**Architecture:** A thin `cmd/ledger` server wraps the existing flat-package `ledger` library (untouched). A gateway-identity middleware reads the mTLS-trusted X-CWB-* the gateway injects (actor=Subject, tenant=Org, perm=Scopes); the existing HS256 path stays for the in-nexus embedded mode. Daily-driver verbs (my/ready) wire over existing library functions; atomic claim is one new transactional method. Deploys to the cwb k3s namespace. HTTP/REST, not the WS-bus.

**Tech Stack:** Go 1.26, the existing ledger library (`github.com/ncruces/go-sqlite3` SQLite driver, FTS5), no new deps for the core. herald (live) + interchange-gateway (live) for identity; mTLS mesh (platform).

---

## Orientation — read before starting

This plan implements the build sequence from `docs/2026-05-31-ledger-mvp-spec.md` §8. Read that spec first; this plan assumes it.

**The library already exists and works.** `github.com/CarriedWorldUniverse/ledger` is a flat-package Go library at the repo root with full issue CRUD, transitions, comments, links, FTS search, multi-tenancy, events, and a polling-updates endpoint — all tested. The MVP **wraps** it; it does not rebuild it. Every existing `.go` file at the repo root stays behaviourally unchanged. The new/changed surface is exactly:

- `cmd/ledger/` — the standalone server (new directory, new `package main`).
- `cmd/ledger/middleware.go` — gateway-identity + dual-auth middleware (new, `package main`).
- `auth.go` — **one additive exported function** `ContextWithAuth` so the middleware can inject `*AuthClaims` into the request context that `tenancy.go`/`GetIssue` already read. Nothing existing changes.
- `claim.go` — **the one genuinely-new library method** `Service.ClaimIssue` (atomic assign+transition+event in a single tx). Needs `s.db`, which is unexported, so it must live in `package ledger`.
- `verbs.go` — REST handlers for `my` / `ready` / `claim` wired over existing `ListMy` / `ListReady` / the new `ClaimIssue`. (New file in `package ledger` so the handlers can be mounted by `Service.Handler()` alongside the existing mux — see Task 1 for why they live here, not in `cmd/`.)
- `rest.go` — three new `mux.HandleFunc` lines mounting the verb routes. Existing handlers untouched.
- Actor-tagging — the middleware threads `X-CWB-Subject` into the `actor` parameter the existing handlers already accept (Task 5); no library mutation-method signatures change.
- `cmd/ledger/Containerfile` + `deploy/k3s/` (new manifests) — Task 6.
- `/Users/jacinta/Source/cwb-conformance/...` — Task 7, cross-repo.

**Why some "wrapping" code lands in `package ledger` not `cmd/ledger`:** the library keeps its DB handle (`s.db`), its mux builder (`Service.Handler`), and its auth-context key (`authClaimsKey`) unexported. Atomic claim needs `s.db`; the verb routes are cleanest mounted inside `Service.Handler`; the context injector needs `authClaimsKey`. These are **additive** files/functions in the library package — they don't touch existing behaviour. The deployment-mode middleware (which is policy, not storage) lives in `cmd/ledger`. This split keeps "library untouched" true in the sense that matters: no existing file's behaviour changes, no existing signature changes, every existing test still passes.

**Ground truth the steps below depend on (verified against the real code):**

- `Service.New(ctx, Config{DBPath, JWTSecret, Notifier})` opens the DB + applies schema. `Service.Handler() http.Handler` returns the mux. `Service.Close()`.
- `AuthClaims{Sub, Org, Role, Jti, Iat, Exp}`; `AuthFromContext(ctx) *AuthClaims`; `authMiddleware(next, secret)`; `resolveAuth(r)`. The context key `authClaimsKey` is unexported.
- Tenancy is driven entirely by `AuthFromContext(ctx)`: `GetIssue`, `AssignIssue`, `UpdateIssue`, `callerCanAccessProject/Issue`, `ListProjects` all read `claims.Org` and apply the hide-existence pattern. **An empty/absent claims context = trusted in-process caller (no tenancy filtering).** So to make the gateway path tenancy-correct, the middleware MUST inject an `*AuthClaims` with `Org` set.
- Mutation methods take an explicit `actor string`: `CreateIssue(ctx, IssueDraft{...Reporter})`, `TransitionIssue(ctx, key, toStatus, actor)`, `AssignIssue(ctx, key, aspect, team, actor)`, `CommentIssue(ctx, key, actor, body)`, `UpdateIssue(ctx, key, patch, actor)`. Today the REST handlers read `actor` from the JSON body. Task 5 makes the gateway path override it from `X-CWB-Subject`.
- `ListMy(ctx, aspect, teams []string)` and `ListReady(ctx, aspect, teams []string)` already exist (search.go). There is **no** public "teams-for-aspect" lookup; existing tests pass `teams = nil`. MVP wires `teams = nil` (aspect-direct assignment is the path; team-membership resolution is out of scope and noted).
- Workflow (workflow.go): story-like types (`Story`/`Task`/`Bug`/`Subtask`) go `To Do → {Ready to Start, In Progress, Cancelled}` and `Ready to Start → {In Progress, ...}`. **The claimed/in-progress target state is `"In Progress"`.** Epics use a different machine (`Brief → Sketch/Refined → In Development`) and are not claimable in the agent sense — claim targets story-like types and transitions to `"In Progress"`.
- `writeEvent(ctx, tx, issueKey, kind, actor, payload)` appends an event inside a caller-supplied tx (events.go). Atomic claim reuses this.
- Actor/author/event columns are opaque `TEXT` (schema.sql: `reporter TEXT`, `events.actor TEXT`, `assignee_aspect TEXT`). They accept herald agent ids without schema change — confirmed.
- `X-CWB-Scopes` is **space-joined** (gateway.go `strings.Join(id.Scopes, " ")`). The middleware splits on whitespace.
- The gateway strips the `/ledger` prefix before proxying, so ledger sees clean `/api/...` paths (gateway.go `match` + `r.URL.Path = rest`). No prefix handling needed in ledger.

**Scope vocabulary (pinned, spec §9):** ledger's herald scope strings are `issue:read`, `issue:write`, `issue:claim`, `issue:admin`. Mapping to actions:
- `issue:read` → GET/search/my/ready/get/updates/projects (read surface).
- `issue:write` → create, patch, transition, assign, comment, watch.
- `issue:claim` → the atomic claim verb specifically.
- `issue:admin` → `/api/admin/*`.
A caller may hold several. The gateway path enforces these from `X-CWB-Scopes`; the embedded HS256 path keeps its existing role gate (`owner`/`admin`/etc) untouched.

**Trust + dual-auth (pinned, spec §3, §9):** MVP trusts `X-CWB-*` because the gateway↔ledger hop is mTLS and ledger is reachable only over it. We do **not** also run heraldauth in-ledger for the MVP (lean: defense-in-depth is a fast-follow). The deployment mode is selected by env var `LEDGER_AUTH_MODE` (`gateway` | `embedded`). In `gateway` mode the X-CWB-* middleware runs and absent identity headers on a gated path → 401. In `embedded` mode the existing HS256 `authMiddleware` runs unchanged.

---

## Conventions for every task

- **TDD where it applies:** failing test → run-expect-fail → minimal compile-ready Go → run-expect-pass → commit. The steps give exact commands and expected-output prefixes.
- **Working directory:** ledger repo root is `/Users/jacinta/Source/ledger` for Tasks 1–6; `/Users/jacinta/Source/cwb-conformance` for Task 7. All paths in commands are absolute.
- **Build/test commands run from the repo root** with the module path, e.g. `go test ./...` from `/Users/jacinta/Source/ledger`.
- **Commits:** real `git commit -m`. Use a `nex-` / `ledger-mvp:` prefix. The spec relates NEX-137 (native tracker), NEX-379 (tracker-port), NEX-382 (herald migration); these tasks don't all have dedicated keys, so a `ledger-mvp:` prefix is fine where no key fits. **Do not push or open PRs** — the operator handles git.
- **No placeholders.** Every step lands complete, compiling Go. No `// TODO`, no "add error handling later", no "similar to Task N".
- **Go version:** the module is `go 1.25.0` today; the Containerfile builds on `golang:1.26` (matching herald). Leave `go.mod` at its current directive unless a step explicitly bumps it. No bump is required by this plan.

---

## Task 1 — `cmd/ledger` standalone server

**Outcome:** a `cmd/ledger` binary that opens the ledger DB from env config and serves the existing library mux over HTTP. No auth wiring yet (that's Task 2); no new routes yet (Tasks 3–4). Build-green + a smoke test.

**Files:** `cmd/ledger/main.go` (new), `cmd/ledger/config.go` (new), `cmd/ledger/main_test.go` (new).

### Steps

- [ ] **1.1 — Write the config struct + env loader (failing test first).**
  Create `/Users/jacinta/Source/ledger/cmd/ledger/config_test.go`:
  ```go
  package main

  import "testing"

  func TestLoadConfig_Defaults(t *testing.T) {
  	t.Setenv("LEDGER_ADDR", "")
  	t.Setenv("LEDGER_DB", "")
  	t.Setenv("LEDGER_AUTH_MODE", "")
  	t.Setenv("LEDGER_JWT_SECRET", "")
  	cfg := loadConfig()
  	if cfg.Addr != ":8081" {
  		t.Errorf("Addr default = %q, want :8081", cfg.Addr)
  	}
  	if cfg.DBPath != "/var/lib/nexus/ledger.db" {
  		t.Errorf("DBPath default = %q", cfg.DBPath)
  	}
  	if cfg.AuthMode != "gateway" {
  		t.Errorf("AuthMode default = %q, want gateway", cfg.AuthMode)
  	}
  }

  func TestLoadConfig_FromEnv(t *testing.T) {
  	t.Setenv("LEDGER_ADDR", ":9090")
  	t.Setenv("LEDGER_DB", "/tmp/x.db")
  	t.Setenv("LEDGER_AUTH_MODE", "embedded")
  	t.Setenv("LEDGER_JWT_SECRET", "s3cr3t")
  	cfg := loadConfig()
  	if cfg.Addr != ":9090" || cfg.DBPath != "/tmp/x.db" || cfg.AuthMode != "embedded" || cfg.JWTSecret != "s3cr3t" {
  		t.Errorf("FromEnv = %+v", cfg)
  	}
  }
  ```
  Run: `go test ./cmd/ledger/ 2>&1 | head`
  Expect FAIL prefix: `# github.com/CarriedWorldUniverse/ledger/cmd/ledger` … `undefined: loadConfig`.

- [ ] **1.2 — Write `config.go` to pass.**
  Create `/Users/jacinta/Source/ledger/cmd/ledger/config.go`:
  ```go
  package main

  import "os"

  // serverConfig is the cmd/ledger server's env-driven runtime config.
  // It is distinct from ledger.Config (which configures the library/DB);
  // this struct also carries the deployment-mode auth selector.
  type serverConfig struct {
  	Addr      string // listen address, e.g. ":8081"
  	DBPath    string // sqlite path
  	AuthMode  string // "gateway" | "embedded"
  	JWTSecret string // HS256 secret, used only in embedded mode
  }

  func env(key, def string) string {
  	if v := os.Getenv(key); v != "" {
  		return v
  	}
  	return def
  }

  // loadConfig reads the server config from the environment, applying
  // CWB-product defaults: gateway-trust auth, the standard nexus data
  // path, and the ledger service port (:8081 — herald owns :8099).
  func loadConfig() serverConfig {
  	return serverConfig{
  		Addr:      env("LEDGER_ADDR", ":8081"),
  		DBPath:    env("LEDGER_DB", "/var/lib/nexus/ledger.db"),
  		AuthMode:  env("LEDGER_AUTH_MODE", "gateway"),
  		JWTSecret: os.Getenv("LEDGER_JWT_SECRET"),
  	}
  }
  ```
  Run: `go test ./cmd/ledger/ 2>&1 | tail -3`
  Expect PASS prefix: `ok  	github.com/CarriedWorldUniverse/ledger/cmd/ledger`.

- [ ] **1.3 — Write `main.go` (server wiring, no auth yet).**
  Create `/Users/jacinta/Source/ledger/cmd/ledger/main.go`:
  ```go
  // Command ledger is the standalone CWB issue-tracker server: a thin
  // HTTP wrapper around the ledger library, deployed behind the
  // interchange-gateway in the cwb k3s namespace.
  //
  // Config (env):
  //
  //	LEDGER_ADDR        listen address (default :8081)
  //	LEDGER_DB          sqlite path (default /var/lib/nexus/ledger.db)
  //	LEDGER_AUTH_MODE   "gateway" (trust X-CWB-* from the mTLS gateway,
  //	                   default) or "embedded" (HS256 self-auth for the
  //	                   in-nexus path)
  //	LEDGER_JWT_SECRET  HS256 secret; used only in embedded mode
  package main

  import (
  	"context"
  	"log"
  	"net/http"

  	"github.com/CarriedWorldUniverse/ledger"
  )

  func main() {
  	cfg := loadConfig()

  	svc, err := ledger.New(context.Background(), ledger.Config{
  		DBPath:    cfg.DBPath,
  		JWTSecret: cfg.JWTSecret,
  	})
  	if err != nil {
  		log.Fatalf("ledger: open %q: %v", cfg.DBPath, err)
  	}
  	defer svc.Close()

  	handler := buildHandler(svc, cfg)

  	log.Printf("ledger listening on %s (db=%s, auth_mode=%s)", cfg.Addr, cfg.DBPath, cfg.AuthMode)
  	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
  		log.Fatalf("ledger: %v", err)
  	}
  }

  // buildHandler assembles the served handler: the library mux wrapped by
  // the deployment-mode auth middleware. In Task 1 the middleware is a
  // pass-through; Task 2 replaces buildAuthMiddleware with the real
  // gateway-identity / HS256 selector.
  func buildHandler(svc *ledger.Service, cfg serverConfig) http.Handler {
  	return buildAuthMiddleware(cfg)(svc.Handler())
  }
  ```
  Note: `buildAuthMiddleware` is defined in the next step so this compiles.

- [ ] **1.4 — Add a pass-through middleware seam so Task 1 compiles + serves.**
  Create `/Users/jacinta/Source/ledger/cmd/ledger/middleware.go`:
  ```go
  package main

  import "net/http"

  // middleware is a standard http.Handler decorator.
  type middleware func(http.Handler) http.Handler

  // buildAuthMiddleware returns the auth decorator for the configured
  // deployment mode. Task 2 fills in the gateway-identity and HS256
  // branches; for now it is an identity pass-through so the server wires
  // end-to-end and the library mux is reachable.
  func buildAuthMiddleware(cfg serverConfig) middleware {
  	return func(next http.Handler) http.Handler {
  		return next
  	}
  }
  ```
  Run: `go build ./cmd/ledger/ 2>&1 | head`
  Expect: no output (clean build).

- [ ] **1.5 — Smoke test: server boots, healthz + create round-trip.**
  Create `/Users/jacinta/Source/ledger/cmd/ledger/main_test.go`:
  ```go
  package main

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"path/filepath"
  	"testing"

  	"github.com/CarriedWorldUniverse/ledger"
  )

  func TestBuildHandler_HealthzAndMux(t *testing.T) {
  	dir := t.TempDir()
  	svc, err := ledger.New(context.Background(), ledger.Config{DBPath: filepath.Join(dir, "ledger.db")})
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer svc.Close()

  	h := buildHandler(svc, serverConfig{AuthMode: "gateway"})
  	srv := httptest.NewServer(h)
  	defer srv.Close()

  	resp, err := http.Get(srv.URL + "/healthz/issues")
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer resp.Body.Close()
  	if resp.StatusCode != http.StatusOK {
  		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
  	}
  }
  ```
  Run: `go test ./cmd/ledger/ 2>&1 | tail -3`
  Expect PASS prefix: `ok  	github.com/CarriedWorldUniverse/ledger/cmd/ledger`.

- [ ] **1.6 — Full build + existing-library regression.**
  Run: `go build ./... && go test ./... 2>&1 | tail -5`
  Expect: clean build; existing root-package tests still `ok` (the library is untouched).

- [ ] **1.7 — Commit.**
  Run:
  ```
  git add cmd/ledger
  git commit -m "ledger-mvp: cmd/ledger standalone server wrapping the library

  Env-driven config (addr, db path, auth mode); buildHandler mounts the
  existing Service.Handler() mux behind a pass-through auth seam that
  Task 2 fills in.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

---

## Task 2 — Gateway-identity middleware (+ dual-auth selector)

**Outcome:** in `gateway` mode the server reads `X-CWB-{Subject,Org,Kind,Scopes}`, maps them to a ledger `*AuthClaims` injected into the request context (so the library's existing tenancy gates fire), enforces scope per route, and 401s when the identity headers are absent on a gated path. In `embedded` mode the existing HS256 `authMiddleware` runs unchanged. The library gains exactly one additive export: `ContextWithAuth`.

**Files:** `auth.go` (one additive exported func + a tiny test), `cmd/ledger/middleware.go` (replace the pass-through), `cmd/ledger/middleware_test.go` (new).

### Steps

- [ ] **2.1 — Library seam: exported context injector (failing test first).**
  The middleware lives in `package main` (cmd/ledger) and cannot set the unexported `authClaimsKey`. Add one additive exported function to the library. First the test — append to `/Users/jacinta/Source/ledger/auth_test.go`:
  ```go
  func TestContextWithAuth_RoundTrips(t *testing.T) {
  	want := &AuthClaims{Sub: "agent.anvil", Org: "carried-world", Role: "member"}
  	ctx := ContextWithAuth(context.Background(), want)
  	got := AuthFromContext(ctx)
  	if got == nil || got.Sub != want.Sub || got.Org != want.Org {
  		t.Fatalf("round-trip = %+v, want %+v", got, want)
  	}
  }
  ```
  Ensure `auth_test.go` imports `"context"` (add to its import block if absent).
  Run: `go test . -run TestContextWithAuth 2>&1 | head`
  Expect FAIL prefix: `undefined: ContextWithAuth`.

- [ ] **2.2 — Add `ContextWithAuth` to `auth.go` (additive only).**
  In `/Users/jacinta/Source/ledger/auth.go`, immediately after `AuthFromContext` (around line 102), add:
  ```go
  // ContextWithAuth returns ctx carrying the given auth claims, retrievable
  // via AuthFromContext. It is the exported seam the standalone cmd/ledger
  // server uses to inject gateway-derived identity (X-CWB-*) so the
  // library's existing tenancy/authorization gates fire the same way they
  // do for the embedded HS256 path. The embedded path continues to use the
  // internal authMiddleware unchanged.
  func ContextWithAuth(ctx context.Context, claims *AuthClaims) context.Context {
  	return context.WithValue(ctx, authClaimsKey, claims)
  }
  ```
  Run: `go test . -run TestContextWithAuth 2>&1 | tail -3`
  Expect PASS: `ok  	github.com/CarriedWorldUniverse/ledger`.
  Run the full library suite to prove nothing else moved: `go test . 2>&1 | tail -3` → `ok`.

- [ ] **2.3 — Commit the library seam on its own.**
  Run:
  ```
  git add auth.go auth_test.go
  git commit -m "ledger-mvp: export ContextWithAuth for gateway-identity injection

  Additive only — the existing HS256 authMiddleware and AuthFromContext are
  unchanged. Lets cmd/ledger inject X-CWB-derived claims into the context the
  tenancy gates already read.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

- [ ] **2.4 — Middleware behaviour test (failing first).**
  Create `/Users/jacinta/Source/ledger/cmd/ledger/middleware_test.go`:
  ```go
  package main

  import (
  	"net/http"
  	"net/http/httptest"
  	"testing"

  	"github.com/CarriedWorldUniverse/ledger"
  )

  // echoIdentity is a tiny terminal handler that reports the claims the
  // middleware injected, so tests can assert the mapping without going
  // through the DB.
  func echoIdentity(w http.ResponseWriter, r *http.Request) {
  	c := ledger.AuthFromContext(r.Context())
  	if c == nil {
  		w.WriteHeader(http.StatusOK)
  		_, _ = w.Write([]byte("nil"))
  		return
  	}
  	w.WriteHeader(http.StatusOK)
  	_, _ = w.Write([]byte(c.Sub + "|" + c.Org))
  }

  func TestGatewayMiddleware_InjectsIdentity(t *testing.T) {
  	mw := buildAuthMiddleware(serverConfig{AuthMode: "gateway"})
  	srv := httptest.NewServer(mw(http.HandlerFunc(echoIdentity)))
  	defer srv.Close()

  	req, _ := http.NewRequest("GET", srv.URL+"/api/issues/my", nil)
  	req.Header.Set("X-CWB-Subject", "agent.anvil")
  	req.Header.Set("X-CWB-Org", "carried-world")
  	req.Header.Set("X-CWB-Scopes", "issue:read issue:write")
  	resp, err := http.DefaultClient.Do(req)
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer resp.Body.Close()
  	if resp.StatusCode != http.StatusOK {
  		t.Fatalf("status = %d", resp.StatusCode)
  	}
  	buf := make([]byte, 64)
  	n, _ := resp.Body.Read(buf)
  	if got := string(buf[:n]); got != "agent.anvil|carried-world" {
  		t.Errorf("identity = %q", got)
  	}
  }

  func TestGatewayMiddleware_RejectsMissingIdentity(t *testing.T) {
  	mw := buildAuthMiddleware(serverConfig{AuthMode: "gateway"})
  	srv := httptest.NewServer(mw(http.HandlerFunc(echoIdentity)))
  	defer srv.Close()

  	// No X-CWB-* headers on a gated path → 401.
  	resp, err := http.Get(srv.URL + "/api/issues/my")
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer resp.Body.Close()
  	if resp.StatusCode != http.StatusUnauthorized {
  		t.Fatalf("missing-identity status = %d, want 401", resp.StatusCode)
  	}
  }

  func TestGatewayMiddleware_HealthzIsPublic(t *testing.T) {
  	mw := buildAuthMiddleware(serverConfig{AuthMode: "gateway"})
  	srv := httptest.NewServer(mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.WriteHeader(http.StatusOK)
  	})))
  	defer srv.Close()

  	// /healthz/issues must not require identity (k8s probes are tokenless).
  	resp, err := http.Get(srv.URL + "/healthz/issues")
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer resp.Body.Close()
  	if resp.StatusCode != http.StatusOK {
  		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
  	}
  }

  func TestScopeForMethodPath(t *testing.T) {
  	cases := []struct {
  		method, path, want string
  	}{
  		{"GET", "/api/issues/my", "issue:read"},
  		{"GET", "/api/issues/ready", "issue:read"},
  		{"GET", "/api/issues/NEX-1", "issue:read"},
  		{"POST", "/api/issues/search", "issue:read"},
  		{"POST", "/api/issues", "issue:write"},
  		{"PATCH", "/api/issues/NEX-1", "issue:write"},
  		{"POST", "/api/issues/NEX-1/transition", "issue:write"},
  		{"POST", "/api/issues/NEX-1/comments", "issue:write"},
  		{"POST", "/api/issues/NEX-1/claim", "issue:claim"},
  		{"GET", "/api/admin/orgs", "issue:admin"},
  	}
  	for _, c := range cases {
  		if got := scopeForMethodPath(c.method, c.path); got != c.want {
  			t.Errorf("%s %s → %q, want %q", c.method, c.path, got, c.want)
  		}
  	}
  }
  ```
  Run: `go test ./cmd/ledger/ -run 'Middleware|Scope' 2>&1 | head`
  Expect FAIL: `undefined: scopeForMethodPath` (and the gateway-mode middleware behaviour assertions failing because it's still pass-through).

- [ ] **2.5 — Implement the real middleware.**
  Replace the entire contents of `/Users/jacinta/Source/ledger/cmd/ledger/middleware.go` with:
  ```go
  package main

  import (
  	"net/http"
  	"strings"

  	"github.com/CarriedWorldUniverse/ledger"
  )

  // middleware is a standard http.Handler decorator.
  type middleware func(http.Handler) http.Handler

  // publicPaths are served without identity in every mode: liveness/
  // readiness probes are tokenless. (The gateway never reaches these from
  // outside in production — they're on the ClusterIP for kubelet — but the
  // middleware must let them through regardless of mode.)
  func isPublicPath(path string) bool {
  	return path == "/healthz/issues"
  }

  // scopeForMethodPath maps an inbound request to the herald scope that
  // gates it (spec §3 / §9 vocabulary). Read surface → issue:read; the
  // mutating verbs → issue:write; the atomic claim verb → issue:claim;
  // /api/admin/* → issue:admin. An unknown path falls back to the most
  // restrictive sensible gate (issue:write) so a new mutating route is
  // never accidentally world-open.
  func scopeForMethodPath(method, path string) string {
  	switch {
  	case strings.HasPrefix(path, "/api/admin/"):
  		return "issue:admin"
  	case strings.HasSuffix(path, "/claim") && method == http.MethodPost:
  		return "issue:claim"
  	case method == http.MethodGet:
  		return "issue:read"
  	case method == http.MethodPost && (path == "/api/issues/search" || path == "/api/issues/search/text"):
  		return "issue:read"
  	default:
  		// POST create, PATCH, transition, assign, comments, watchers.
  		return "issue:write"
  	}
  }

  // buildAuthMiddleware returns the auth decorator for the configured
  // deployment mode.
  //
  //   - "embedded": the existing in-nexus HS256 path. We do NOT re-wrap
  //     here; the library's own admin mux already runs authMiddleware for
  //     /api/admin/*, and the embedded broker mounts the rest behind its
  //     own listener. In standalone embedded mode we simply pass through —
  //     identity is asserted by the HS256 token the library resolves per
  //     request (resolveAuth). This keeps the embedded path byte-for-byte
  //     the library's existing behaviour.
  //   - "gateway" (default): trust X-CWB-* injected by the mTLS gateway.
  func buildAuthMiddleware(cfg serverConfig) middleware {
  	if cfg.AuthMode == "embedded" {
  		return func(next http.Handler) http.Handler { return next }
  	}
  	return gatewayIdentity
  }

  // gatewayIdentity reads the gateway-injected X-CWB-* headers, maps them
  // to a ledger.AuthClaims, enforces the per-route scope, and injects the
  // claims into the request context so the library's tenancy gates fire.
  // Absent identity headers on a gated path → 401 (the gateway only omits
  // them when it did not authenticate — which must never reach a gated
  // route).
  func gatewayIdentity(next http.Handler) http.Handler {
  	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		if isPublicPath(r.URL.Path) {
  			next.ServeHTTP(w, r)
  			return
  		}

  		sub := r.Header.Get("X-CWB-Subject")
  		org := r.Header.Get("X-CWB-Org")
  		if sub == "" || org == "" {
  			http.Error(w, `{"error":"identity_required"}`, http.StatusUnauthorized)
  			return
  		}

  		scopes := parseScopes(r.Header.Get("X-CWB-Scopes"))
  		need := scopeForMethodPath(r.Method, r.URL.Path)
  		if !hasScope(scopes, need) {
  			http.Error(w, `{"error":"insufficient_scope"}`, http.StatusForbidden)
  			return
  		}

  		// Map herald identity → ledger claims. Org drives the tenancy
  		// gate; Sub is the actor (Task 5 threads it into the mutation
  		// handlers). Role is left empty: gateway-path authorization is
  		// scope-based, not the embedded role ladder. The library's
  		// role checks only run on the embedded HS256 path.
  		claims := &ledger.AuthClaims{Sub: sub, Org: org}
  		ctx := ledger.ContextWithAuth(r.Context(), claims)
  		next.ServeHTTP(w, r.WithContext(ctx))
  	})
  }

  func parseScopes(raw string) []string {
  	return strings.Fields(raw) // space- or whitespace-separated; gateway space-joins
  }

  func hasScope(have []string, need string) bool {
  	for _, s := range have {
  		if s == need || s == "issue:admin" {
  			return true // issue:admin is a superset
  		}
  	}
  	return false
  }
  ```
  Run: `go test ./cmd/ledger/ 2>&1 | tail -3`
  Expect PASS: `ok  	github.com/CarriedWorldUniverse/ledger/cmd/ledger`.

- [ ] **2.6 — Full regression.**
  Run: `go build ./... && go test ./... 2>&1 | tail -5`
  Expect: clean build; library tests `ok`; cmd/ledger tests `ok`.

- [ ] **2.7 — Commit.**
  Run:
  ```
  git add cmd/ledger/middleware.go cmd/ledger/middleware_test.go
  git commit -m "ledger-mvp: gateway-identity middleware with dual-auth selector

  gateway mode reads X-CWB-{Subject,Org,Scopes}, maps Subject->actor /
  Org->tenant / Scopes->permission, enforces per-route scope, injects
  claims via ContextWithAuth so the tenancy gates fire, and 401s when the
  identity headers are absent on a gated path. embedded mode preserves the
  existing HS256 path. /healthz/issues stays public.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

---

## Task 3 — Wire `my` + `ready` REST routes

**Outcome:** `GET /api/issues/my` and `GET /api/issues/ready` are served over the existing `ListMy` / `ListReady` library functions, **scoped to the caller** (the aspect = `X-CWB-Subject`, threaded via the injected claims). New handlers live in the library package so they mount in `Service.Handler()`; the routes are added to `rest.go`.

**Why the caller scoping comes from claims, not a query param:** the verbs are "my" / "ready *for me*". The handler reads `AuthFromContext(ctx).Sub` as the aspect. On the embedded path (no claims) it falls back to an `?aspect=` query param so the existing in-process/test callers keep working.

**Files:** `verbs.go` (new, `package ledger`), `verbs_test.go` (new), `rest.go` (two new mount lines).

### Steps

- [ ] **3.1 — Handler test (failing first).**
  Create `/Users/jacinta/Source/ledger/verbs_test.go`:
  ```go
  package ledger

  import (
  	"context"
  	"encoding/json"
  	"net/http"
  	"net/http/httptest"
  	"path/filepath"
  	"testing"
  )

  func verbsTestService(t *testing.T) *Service {
  	t.Helper()
  	dir := t.TempDir()
  	svc, err := New(context.Background(), Config{DBPath: filepath.Join(dir, "ledger.db")})
  	if err != nil {
  		t.Fatal(err)
  	}
  	t.Cleanup(func() { _ = svc.Close() })
  	return svc
  }

  func TestHandleMy_ScopedToCallerViaQuery(t *testing.T) {
  	ctx := context.Background()
  	svc := verbsTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "mine",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})
  	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "theirs",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "keel"})

  	srv := httptest.NewServer(svc.Handler())
  	defer srv.Close()

  	resp, err := http.Get(srv.URL + "/api/issues/my?aspect=anvil")
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer resp.Body.Close()
  	if resp.StatusCode != http.StatusOK {
  		t.Fatalf("status = %d", resp.StatusCode)
  	}
  	var refs []IssueRef
  	_ = json.NewDecoder(resp.Body).Decode(&refs)
  	if len(refs) != 1 || refs[0].AssigneeAspect != "anvil" {
  		t.Fatalf("my = %+v", refs)
  	}
  }

  func TestHandleReady_ScopedToCallerViaQuery(t *testing.T) {
  	ctx := context.Background()
  	svc := verbsTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "ready",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

  	srv := httptest.NewServer(svc.Handler())
  	defer srv.Close()

  	resp, err := http.Get(srv.URL + "/api/issues/ready?aspect=anvil")
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer resp.Body.Close()
  	if resp.StatusCode != http.StatusOK {
  		t.Fatalf("status = %d", resp.StatusCode)
  	}
  	var refs []IssueRef
  	_ = json.NewDecoder(resp.Body).Decode(&refs)
  	if len(refs) != 1 {
  		t.Fatalf("ready = %+v", refs)
  	}
  }

  func TestHandleMy_PrefersClaimsSubjectOverQuery(t *testing.T) {
  	ctx := context.Background()
  	svc := verbsTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	_, _ = svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "mine",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow", AssigneeAspect: "anvil"})

  	// Inject claims for anvil; pass a contradictory ?aspect=keel. Claims win.
  	req := httptest.NewRequest("GET", "/api/issues/my?aspect=keel", nil)
  	req = req.WithContext(ContextWithAuth(req.Context(), &AuthClaims{Sub: "anvil"}))
  	rec := httptest.NewRecorder()
  	svc.Handler().ServeHTTP(rec, req)

  	if rec.Code != http.StatusOK {
  		t.Fatalf("status = %d", rec.Code)
  	}
  	var refs []IssueRef
  	_ = json.NewDecoder(rec.Body).Decode(&refs)
  	if len(refs) != 1 || refs[0].AssigneeAspect != "anvil" {
  		t.Fatalf("claims-scoped my = %+v", refs)
  	}
  }
  ```
  Note: the contradictory-aspect test passes claims with no `Org`, so the tenancy gate is a no-op and the assertion isolates the claims-vs-query precedence. Run: `go test . -run 'HandleMy|HandleReady' 2>&1 | head`
  Expect FAIL: `404` responses (routes not mounted) / `undefined: handleListMy`.

- [ ] **3.2 — Implement the verb handlers.**
  Create `/Users/jacinta/Source/ledger/verbs.go`:
  ```go
  package ledger

  import (
  	"encoding/json"
  	"net/http"
  )

  // callerAspect resolves the aspect a "my"/"ready" query is scoped to.
  // The herald-gated path injects AuthClaims whose Sub is the calling
  // agent — that wins. The embedded / in-process path (no claims) falls
  // back to the ?aspect= query param so existing callers keep working.
  func callerAspect(r *http.Request) string {
  	if c := AuthFromContext(r.Context()); c != nil && c.Sub != "" {
  		return c.Sub
  	}
  	return r.URL.Query().Get("aspect")
  }

  // handleListMy powers GET /api/issues/my — the caller's open assigned
  // issues (ListMy). Scoped to callerAspect; team membership is not
  // resolved at the MVP (teams=nil), so this returns directly-assigned
  // work, which is the agent daily-driver case.
  func (s *Service) handleListMy(w http.ResponseWriter, r *http.Request) {
  	if r.Method != http.MethodGet {
  		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
  		return
  	}
  	aspect := callerAspect(r)
  	if aspect == "" {
  		http.Error(w, "aspect required", http.StatusBadRequest)
  		return
  	}
  	refs, err := s.ListMy(r.Context(), aspect, nil)
  	if err != nil {
  		http.Error(w, err.Error(), http.StatusInternalServerError)
  		return
  	}
  	if refs == nil {
  		refs = []IssueRef{}
  	}
  	w.Header().Set("Content-Type", "application/json")
  	_ = json.NewEncoder(w).Encode(refs)
  }

  // handleListReady powers GET /api/issues/ready — the top of the caller's
  // ready pool (ListReady): assigned issues in a startable state, ordered
  // priority-then-age. Scoped to callerAspect; teams=nil at the MVP.
  func (s *Service) handleListReady(w http.ResponseWriter, r *http.Request) {
  	if r.Method != http.MethodGet {
  		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
  		return
  	}
  	aspect := callerAspect(r)
  	if aspect == "" {
  		http.Error(w, "aspect required", http.StatusBadRequest)
  		return
  	}
  	refs, err := s.ListReady(r.Context(), aspect, nil)
  	if err != nil {
  		http.Error(w, err.Error(), http.StatusInternalServerError)
  		return
  	}
  	if refs == nil {
  		refs = []IssueRef{}
  	}
  	w.Header().Set("Content-Type", "application/json")
  	_ = json.NewEncoder(w).Encode(refs)
  }
  ```

- [ ] **3.3 — Mount the routes in `rest.go`.**
  In `/Users/jacinta/Source/ledger/rest.go`, inside `Handler()`, add the two routes **before** the catch-all `/api/issues/` mount so the exact paths win. The existing block is:
  ```go
  	mux.HandleFunc("/api/issues", s.handleCreate)
  	mux.HandleFunc("/api/issues/", s.handleIssueByKey)
  	mux.HandleFunc("/api/issues/search", s.handleSearch)
  ```
  Change it to:
  ```go
  	mux.HandleFunc("/api/issues", s.handleCreate)
  	mux.HandleFunc("/api/issues/my", s.handleListMy)
  	mux.HandleFunc("/api/issues/ready", s.handleListReady)
  	mux.HandleFunc("/api/issues/", s.handleIssueByKey)
  	mux.HandleFunc("/api/issues/search", s.handleSearch)
  ```
  Note: Go's `http.ServeMux` matches the longest registered pattern, so the exact `/api/issues/my` and `/api/issues/ready` take precedence over the `/api/issues/` subtree regardless of registration order — the ordering above is for readability. (The atomic-claim route in Task 4 is `/api/issues/{key}/claim` and is handled inside `handleIssueByKey`'s action switch, not as a separate mux entry, because the key is variable.)
  Run: `go test . -run 'HandleMy|HandleReady' 2>&1 | tail -3`
  Expect PASS: `ok  	github.com/CarriedWorldUniverse/ledger`.

- [ ] **3.4 — Full regression.**
  Run: `go build ./... && go test ./... 2>&1 | tail -5`
  Expect: clean build; all tests `ok`.

- [ ] **3.5 — Commit.**
  Run:
  ```
  git add verbs.go verbs_test.go rest.go
  git commit -m "ledger-mvp: wire GET /api/issues/my and /ready over ListMy/ListReady

  Caller-scoped via the injected herald Subject (claims win), with an
  ?aspect= fallback for the embedded/in-process path. teams=nil at MVP
  (directly-assigned work is the agent daily-driver case).

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

---

## Task 4 — Atomic `claim` (the one genuinely-new piece)

**Outcome:** `POST /api/issues/{key}/claim` performs, in a single DB transaction: verify the issue is claimable (story-like type, in a startable state, not already claimed by a different agent), set `assignee_aspect = caller`, transition status to `"In Progress"`, append a `claim` event. Returns **200 + the updated issue** on success, **409** if already claimed by a different agent (lost the race), **400** for a non-claimable type/state, **404** for an unknown key. Gated on `issue:claim` (enforced by the Task 2 middleware on the gateway path).

This is the meaty task. The new transactional method lives in the library (`claim.go`) because it needs `s.db`; the route is added to `handleIssueByKey`'s action switch.

**Design — claimability rules (derived from workflow.go + the spec §4):**
- Type must be story-like (`Story`/`Task`/`Bug`/`Subtask`). Epics use a different machine and aren't agent-claimable → `ErrNotClaimable`.
- Current status must allow reaching `"In Progress"`: per `storyLikeTransitions`, that's `"To Do"`, `"Ready to Start"`, `"Blocked"`, or already `"In Progress"`. We treat the legal set as `{To Do, Ready to Start, Blocked}` for a *fresh* claim; `"In Progress"` is handled by the already-claimed check below.
- Already-claimed semantics:
  - `assignee_aspect == ""` (unassigned) → claimable.
  - `assignee_aspect == caller` → idempotent re-claim: ensure status is `"In Progress"` (transition if not already), no 409. (An agent re-running claim on its own ticket succeeds.)
  - `assignee_aspect == someoneElse` → **409** `ErrAlreadyClaimed`.
- The `"In Progress"` transition is validated against the state machine the same way `TransitionIssue` does (reuse `validateTransition`), so a `Done`/`Cancelled` ticket can't be claimed.

**Concurrency:** the whole read-check-write runs in one `BeginTx`. SQLite is opened with `_busy_timeout=5000` + WAL (service.go), and writes serialize. The losing concurrent claimer re-reads inside its own tx and sees the winner's `assignee_aspect`, returning 409. A `SELECT ... ` inside the tx followed by the conditional `UPDATE` is sufficient under SQLite's single-writer model; no explicit row lock is needed.

**Files:** `claim.go` (new, `package ledger`), `claim_test.go` (new), `rest.go` (one new action case), `verbs.go` (the `respondClaim` HTTP handler).

### Steps

- [ ] **4.1 — Sentinel errors + claimability test (failing first).**
  Create `/Users/jacinta/Source/ledger/claim_test.go`:
  ```go
  package ledger

  import (
  	"context"
  	"errors"
  	"sync"
  	"testing"
  )

  func claimTestService(t *testing.T) *Service {
  	t.Helper()
  	svc := verbsTestService(t) // reuse the helper from verbs_test.go
  	return svc
  }

  func TestClaimIssue_FreshClaimSetsAssigneeAndInProgress(t *testing.T) {
  	ctx := context.Background()
  	svc := claimTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "claim me",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	got, err := svc.ClaimIssue(ctx, iss.Key, "anvil")
  	if err != nil {
  		t.Fatal(err)
  	}
  	if got.AssigneeAspect != "anvil" {
  		t.Errorf("assignee = %q, want anvil", got.AssigneeAspect)
  	}
  	if got.Status != "In Progress" {
  		t.Errorf("status = %q, want In Progress", got.Status)
  	}

  	// A claim event was appended.
  	tl, _ := svc.Timeline(ctx, iss.Key)
  	var sawClaim bool
  	for _, e := range tl {
  		if e.Kind == "claim" && e.Actor == "anvil" {
  			sawClaim = true
  		}
  	}
  	if !sawClaim {
  		t.Error("no claim event for anvil")
  	}
  }

  func TestClaimIssue_AlreadyClaimedByOther409(t *testing.T) {
  	ctx := context.Background()
  	svc := claimTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	if _, err := svc.ClaimIssue(ctx, iss.Key, "anvil"); err != nil {
  		t.Fatal(err)
  	}
  	_, err := svc.ClaimIssue(ctx, iss.Key, "keel")
  	if !errors.Is(err, ErrAlreadyClaimed) {
  		t.Fatalf("second claim err = %v, want ErrAlreadyClaimed", err)
  	}
  }

  func TestClaimIssue_IdempotentForSameAgent(t *testing.T) {
  	ctx := context.Background()
  	svc := claimTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	if _, err := svc.ClaimIssue(ctx, iss.Key, "anvil"); err != nil {
  		t.Fatal(err)
  	}
  	got, err := svc.ClaimIssue(ctx, iss.Key, "anvil")
  	if err != nil {
  		t.Fatalf("re-claim by same agent err = %v", err)
  	}
  	if got.AssigneeAspect != "anvil" || got.Status != "In Progress" {
  		t.Fatalf("idempotent re-claim = %+v", got)
  	}
  }

  func TestClaimIssue_EpicNotClaimable(t *testing.T) {
  	ctx := context.Background()
  	svc := claimTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Epic", Summary: "big",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	_, err := svc.ClaimIssue(ctx, iss.Key, "anvil")
  	if !errors.Is(err, ErrNotClaimable) {
  		t.Fatalf("epic claim err = %v, want ErrNotClaimable", err)
  	}
  }

  func TestClaimIssue_UnknownKeyNotFound(t *testing.T) {
  	ctx := context.Background()
  	svc := claimTestService(t)
  	_, err := svc.ClaimIssue(ctx, "NEX-999", "anvil")
  	if !errors.Is(err, ErrIssueNotFound) {
  		t.Fatalf("unknown-key claim err = %v, want ErrIssueNotFound", err)
  	}
  }

  func TestClaimIssue_ConcurrentClaimsExactlyOneWins(t *testing.T) {
  	ctx := context.Background()
  	svc := claimTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "race",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	const n = 8
  	var wg sync.WaitGroup
  	wins := make([]bool, n)
  	for i := 0; i < n; i++ {
  		wg.Add(1)
  		go func(i int) {
  			defer wg.Done()
  			agent := "agent" + string(rune('A'+i))
  			_, err := svc.ClaimIssue(ctx, iss.Key, agent)
  			wins[i] = err == nil
  		}(i)
  	}
  	wg.Wait()

  	winners := 0
  	for _, w := range wins {
  		if w {
  			winners++
  		}
  	}
  	if winners != 1 {
  		t.Fatalf("winners = %d, want exactly 1", winners)
  	}
  }
  ```
  Run: `go test . -run TestClaimIssue 2>&1 | head`
  Expect FAIL: `undefined: ClaimIssue` / `undefined: ErrAlreadyClaimed` / `undefined: ErrNotClaimable`.

- [ ] **4.2 — Implement `ClaimIssue` (the atomic method).**
  Create `/Users/jacinta/Source/ledger/claim.go`:
  ```go
  package ledger

  import (
  	"context"
  	"database/sql"
  	"errors"
  	"fmt"
  )

  // ErrAlreadyClaimed is returned by ClaimIssue when the issue is already
  // assigned to a DIFFERENT agent — the caller lost the race. Surfaced as
  // HTTP 409 by the REST layer.
  var ErrAlreadyClaimed = errors.New("ledger: issue already claimed by another agent")

  // ErrNotClaimable is returned by ClaimIssue when the issue's type or
  // current status can't be claimed (e.g. an Epic, or a Done/Cancelled
  // ticket). Surfaced as HTTP 400.
  var ErrNotClaimable = errors.New("ledger: issue is not in a claimable state")

  // claimTargetStatus is the state a successful claim transitions the
  // issue into — the story-like "in progress" state per workflow.go.
  const claimTargetStatus = "In Progress"

  // ClaimIssue atomically claims an issue for the calling agent: in a
  // single DB transaction it verifies claimability, sets the assignee to
  // the caller, transitions the issue to "In Progress", and appends a
  // claim event. It returns the updated issue.
  //
  // This is the atomic replacement for the old two-call (assign, then
  // transition) flow, which raced when two agents claimed concurrently.
  // Concurrency rests on SQLite's single-writer model: the read-check and
  // the conditional write happen inside one BeginTx, so a losing claimer
  // re-reads the winner's assignee and gets ErrAlreadyClaimed.
  //
  // Semantics:
  //   - unassigned & claimable type/state  → claim, transition, 200
  //   - already assigned to the caller     → idempotent: ensure In Progress, 200
  //   - assigned to a different agent       → ErrAlreadyClaimed (409)
  //   - Epic or terminal/illegal state      → ErrNotClaimable (400)
  //   - unknown key                         → ErrIssueNotFound (404)
  //
  // Tenancy: cross-org callers can't see the issue (same hide-existence
  // pattern as GetIssue), enforced via callerCanAccessIssue before the tx.
  func (s *Service) ClaimIssue(ctx context.Context, key, actor string) (*Issue, error) {
  	if actor == "" {
  		return nil, fmt.Errorf("ClaimIssue: actor required")
  	}
  	// Tenancy gate first (hide-existence on cross-org).
  	if err := s.callerCanAccessIssue(ctx, key); err != nil {
  		return nil, err
  	}

  	tx, err := s.db.BeginTx(ctx, nil)
  	if err != nil {
  		return nil, err
  	}
  	defer tx.Rollback()

  	var issueType, status, dod string
  	var assignee sql.NullString
  	err = tx.QueryRowContext(ctx,
  		`SELECT type, status, definition_of_done, assignee_aspect FROM issues WHERE key = ?`, key,
  	).Scan(&issueType, &status, &dod, &assignee)
  	if errors.Is(err, sql.ErrNoRows) {
  		return nil, ErrIssueNotFound
  	}
  	if err != nil {
  		return nil, fmt.Errorf("ClaimIssue: load %s: %w", key, err)
  	}

  	// Already claimed by someone else → lost the race.
  	if assignee.Valid && assignee.String != "" && assignee.String != actor {
  		return nil, ErrAlreadyClaimed
  	}

  	alreadyMine := assignee.Valid && assignee.String == actor

  	// Determine whether a transition to "In Progress" is needed + legal.
  	needTransition := status != claimTargetStatus
  	if needTransition {
  		if err := validateTransition(issueType, status, claimTargetStatus, dod); err != nil {
  			// Illegal state machine move (e.g. Epic, Done, Cancelled) →
  			// not claimable. validateTransition's error is descriptive but
  			// we normalise to the sentinel so callers can branch to 400.
  			return nil, fmt.Errorf("%w: %v", ErrNotClaimable, err)
  		}
  	}

  	// Assign to the caller (no-op write if already mine — keeps the path
  	// uniform and refreshes updated_at).
  	if _, err := tx.ExecContext(ctx,
  		`UPDATE issues SET assignee_aspect = ?, assignee_team = NULL, updated_at = datetime('now') WHERE key = ?`,
  		actor, key,
  	); err != nil {
  		return nil, fmt.Errorf("ClaimIssue: assign: %w", err)
  	}

  	if needTransition {
  		if _, err := tx.ExecContext(ctx,
  			`UPDATE issues SET status = ?, updated_at = datetime('now') WHERE key = ?`,
  			claimTargetStatus, key,
  		); err != nil {
  			return nil, fmt.Errorf("ClaimIssue: transition: %w", err)
  		}
  	}

  	// One claim event records the whole atomic action.
  	payload := map[string]any{
  		"assignee":    actor,
  		"from_status": status,
  		"to_status":   claimTargetStatus,
  		"reclaim":     alreadyMine,
  	}
  	if err := writeEvent(ctx, tx, key, "claim", actor, payload); err != nil {
  		return nil, fmt.Errorf("ClaimIssue: event: %w", err)
  	}

  	if err := tx.Commit(); err != nil {
  		return nil, err
  	}

  	// Fire-and-forget operator notification, mirroring the other mutators.
  	_ = s.notify.NotifyOperatorStream(ctx, fmt.Sprintf("%s claimed by %s", key, actor))

  	return s.GetIssue(ctx, key)
  }
  ```
  Run: `go test . -run TestClaimIssue 2>&1 | tail -3`
  Expect PASS: `ok  	github.com/CarriedWorldUniverse/ledger`.

- [ ] **4.3 — HTTP handler `respondClaim` (failing test first).**
  Append to `/Users/jacinta/Source/ledger/claim_test.go`:
  ```go
  func TestRESTClaim_SuccessAndConflict(t *testing.T) {
  	ctx := context.Background()
  	svc := claimTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	srv := httptest.NewServer(svc.Handler())
  	defer srv.Close()

  	// First claim — pass actor in the body (no claims context here).
  	body := bytes.NewBufferString(`{"actor":"anvil"}`)
  	resp, err := http.Post(srv.URL+"/api/issues/"+iss.Key+"/claim", "application/json", body)
  	if err != nil {
  		t.Fatal(err)
  	}
  	if resp.StatusCode != http.StatusOK {
  		t.Fatalf("claim status = %d, want 200", resp.StatusCode)
  	}
  	var got Issue
  	_ = json.NewDecoder(resp.Body).Decode(&got)
  	resp.Body.Close()
  	if got.AssigneeAspect != "anvil" || got.Status != "In Progress" {
  		t.Fatalf("claimed issue = %+v", got)
  	}

  	// Second claim by a different agent → 409.
  	body2 := bytes.NewBufferString(`{"actor":"keel"}`)
  	resp2, err := http.Post(srv.URL+"/api/issues/"+iss.Key+"/claim", "application/json", body2)
  	if err != nil {
  		t.Fatal(err)
  	}
  	defer resp2.Body.Close()
  	if resp2.StatusCode != http.StatusConflict {
  		t.Fatalf("conflicting claim status = %d, want 409", resp2.StatusCode)
  	}
  }
  ```
  Add the imports this needs to `claim_test.go`'s import block: `"bytes"`, `"encoding/json"`, `"net/http"`, `"net/http/httptest"`.
  Run: `go test . -run TestRESTClaim 2>&1 | head`
  Expect FAIL: `405`/`method/path not supported` (the `claim` action isn't routed yet).

- [ ] **4.4 — Add the `respondClaim` handler in `verbs.go`.**
  Append to `/Users/jacinta/Source/ledger/verbs.go`:
  ```go
  // respondClaim powers POST /api/issues/{key}/claim — the atomic claim
  // verb. The actor is the calling agent: the herald Subject when present
  // (gateway path), else the JSON body's "actor" (embedded/in-process).
  // Maps the claim sentinels to HTTP: 409 ErrAlreadyClaimed, 400
  // ErrNotClaimable, 404 ErrIssueNotFound.
  func (s *Service) respondClaim(w http.ResponseWriter, r *http.Request, key string) {
  	if r.Method != http.MethodPost {
  		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
  		return
  	}
  	actor := mutationActor(r)
  	if actor == "" {
  		http.Error(w, "actor required", http.StatusBadRequest)
  		return
  	}
  	issue, err := s.ClaimIssue(r.Context(), key, actor)
  	switch {
  	case errors.Is(err, ErrAlreadyClaimed):
  		http.Error(w, `{"error":"already_claimed"}`, http.StatusConflict)
  		return
  	case errors.Is(err, ErrNotClaimable):
  		http.Error(w, `{"error":"not_claimable"}`, http.StatusBadRequest)
  		return
  	case errors.Is(err, ErrIssueNotFound):
  		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
  		return
  	case err != nil:
  		http.Error(w, err.Error(), http.StatusInternalServerError)
  		return
  	}
  	w.Header().Set("Content-Type", "application/json")
  	w.WriteHeader(http.StatusOK)
  	_ = json.NewEncoder(w).Encode(issue)
  }
  ```
  This references `mutationActor`, defined in Task 5 — but `respondClaim` is also routed in this step's next sub-step, and `mutationActor` is needed now to compile. To keep Task 4 self-contained and compiling, **add a minimal `mutationActor` here** that Task 5 will extend to all mutations:
  ```go
  // mutationActor resolves the actor attributed to a mutation. The
  // herald-gated path uses the injected Subject (the individual agent id);
  // the embedded/in-process path falls back to the explicit body actor.
  // Task 5 routes every mutating handler through this so attribution is
  // uniform.
  func mutationActor(r *http.Request) string {
  	if c := AuthFromContext(r.Context()); c != nil && c.Sub != "" {
  		return c.Sub
  	}
  	// Embedded path: the body carries "actor". We peek without consuming
  	// the body destructively by relying on the per-handler decode; here we
  	// only need the value for handlers that have already decoded it, so the
  	// claim handler passes it explicitly via the request — see note below.
  	return bodyActor(r)
  }
  ```
  Because the claim body must be read to get `actor` and `mutationActor` would otherwise consume `r.Body`, implement `bodyActor` to decode the small claim body once and stash it. Simplest correct approach: have `respondClaim` decode the body itself and not rely on `mutationActor` reading the body. Replace the `actor := mutationActor(r)` line in `respondClaim` with an explicit decode:
  ```go
  	actor := AuthSubject(r) // herald path
  	if actor == "" {
  		var raw struct {
  			Actor string `json:"actor"`
  		}
  		_ = json.NewDecoder(r.Body).Decode(&raw)
  		actor = raw.Actor
  	}
  ```
  and add the small exported-from-package helper in `verbs.go`:
  ```go
  // AuthSubject returns the herald Subject from the request's injected
  // claims, or "" if none (embedded/in-process path). Used by the mutation
  // handlers to attribute actions to the individual agent.
  func AuthSubject(r *http.Request) string {
  	if c := AuthFromContext(r.Context()); c != nil {
  		return c.Sub
  	}
  	return ""
  }
  ```
  Then **delete** the placeholder `mutationActor`/`bodyActor` stubs above (they were a thinking-aloud detour; the explicit-decode form is the one to keep). The final `respondClaim` reads: herald Subject first, else decode the body's `actor`. This keeps Task 4 compiling without depending on Task 5.

- [ ] **4.5 — Route `claim` in `handleIssueByKey` (`rest.go`).**
  In `/Users/jacinta/Source/ledger/rest.go`, in the `handleIssueByKey` action switch, add a case alongside the existing `transition`/`assign`/`comments` cases:
  ```go
  	case r.Method == http.MethodPost && action == "claim":
  		s.respondClaim(w, r, key)
  ```
  Place it immediately after the `comments` case.
  Run: `go test . -run TestRESTClaim 2>&1 | tail -3`
  Expect PASS: `ok  	github.com/CarriedWorldUniverse/ledger`.

- [ ] **4.6 — Full regression incl. the concurrency test under the race detector.**
  Run: `go build ./... && go test -race . ./cmd/ledger/ 2>&1 | tail -6`
  Expect: clean build; all tests `ok`; the concurrent-claim test passes with exactly one winner and no data race reported.

- [ ] **4.7 — Commit.**
  Run:
  ```
  git add claim.go claim_test.go verbs.go rest.go
  git commit -m "ledger-mvp: atomic claim verb (assign+transition+event in one tx)

  POST /api/issues/{key}/claim: single-transaction verify-claimable, set
  assignee=caller, transition to In Progress, append a claim event.
  409 on lost race (assigned to another agent), idempotent for the same
  agent, 400 for non-claimable type/state, 404 for unknown key. Replaces
  the old two-call assign-then-transition flow. Race-tested.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

---

## Task 5 — Actor-tagging across mutations

**Outcome:** on the gateway path, every mutation (create/claim/comment/transition/assign/patch) is attributed to the calling agent (`X-CWB-Subject`), not whatever the JSON body happened to carry. The embedded/in-process path keeps body-supplied actors. Actor/author/event columns already accept herald agent ids (opaque `TEXT`, confirmed against schema.sql), so no schema change.

**Mechanism:** the existing handlers (`respondTransition`, `respondAssign`, `respondComment`, `respondPatch`, `handleCreate`) read `actor`/`reporter` from the decoded body. We thread the herald Subject in so it **overrides** the body value when present. `respondClaim` already does this (Task 4). Claim already attributes via `ClaimIssue`'s `actor` param.

**Files:** `rest.go` (override actor in the existing handlers using the `AuthSubject` helper from Task 4), `actor_test.go` (new).

### Steps

- [ ] **5.1 — Attribution test (failing first).**
  Create `/Users/jacinta/Source/ledger/actor_test.go`:
  ```go
  package ledger

  import (
  	"context"
  	"net/http/httptest"
  	"strings"
  	"testing"
  )

  // postWithSubject drives a request through Service.Handler with an
  // injected herald Subject and a body actor that should be OVERRIDDEN.
  func postWithSubject(t *testing.T, svc *Service, method, path, subject, body string) {
  	t.Helper()
  	req := httptest.NewRequest(method, path, strings.NewReader(body))
  	req.Header.Set("Content-Type", "application/json")
  	req = req.WithContext(ContextWithAuth(req.Context(), &AuthClaims{Sub: subject}))
  	rec := httptest.NewRecorder()
  	svc.Handler().ServeHTTP(rec, req)
  	if rec.Code >= 400 {
  		t.Fatalf("%s %s → %d: %s", method, path, rec.Code, rec.Body.String())
  	}
  }

  func TestActorTagging_HeraldSubjectOverridesBodyActor(t *testing.T) {
  	ctx := context.Background()
  	svc := verbsTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	// Comment with a LYING body actor; the herald Subject must win.
  	postWithSubject(t, svc, "POST", "/api/issues/"+iss.Key+"/comments",
  		"agent.anvil", `{"actor":"impersonated","body":"hello"}`)

  	tl, _ := svc.Timeline(ctx, iss.Key)
  	var commentActor string
  	for _, e := range tl {
  		if e.Kind == "comment" {
  			commentActor = e.Actor
  		}
  	}
  	if commentActor != "agent.anvil" {
  		t.Fatalf("comment actor = %q, want agent.anvil (herald Subject wins)", commentActor)
  	}
  }

  func TestActorTagging_EmbeddedPathKeepsBodyActor(t *testing.T) {
  	ctx := context.Background()
  	svc := verbsTestService(t)
  	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
  	iss, _ := svc.CreateIssue(ctx, IssueDraft{Project: "NEX", Type: "Story", Summary: "x",
  		DefinitionOfDone: "- [ ] go", Reporter: "shadow"})

  	// No claims context (embedded): body actor is honoured.
  	req := httptest.NewRequest("POST", "/api/issues/"+iss.Key+"/comments",
  		strings.NewReader(`{"actor":"shadow","body":"hi"}`))
  	req.Header.Set("Content-Type", "application/json")
  	rec := httptest.NewRecorder()
  	svc.Handler().ServeHTTP(rec, req)
  	if rec.Code >= 400 {
  		t.Fatalf("comment → %d: %s", rec.Code, rec.Body.String())
  	}

  	tl, _ := svc.Timeline(ctx, iss.Key)
  	var commentActor string
  	for _, e := range tl {
  		if e.Kind == "comment" {
  			commentActor = e.Actor
  		}
  	}
  	if commentActor != "shadow" {
  		t.Fatalf("embedded comment actor = %q, want shadow", commentActor)
  	}
  }
  ```
  Run: `go test . -run TestActorTagging 2>&1 | head`
  Expect FAIL: the override test fails (`impersonated` wins) because the handlers don't yet prefer the Subject.

- [ ] **5.2 — Override the actor in each mutating handler (`rest.go`).**
  In `/Users/jacinta/Source/ledger/rest.go`, thread the herald Subject in **after** each handler decodes its body, overriding the body actor when a Subject is present. Add a tiny helper at the top of `rest.go` (after the imports):
  ```go
  // effectiveActor returns the herald Subject (gateway path) if present,
  // else the body-supplied actor (embedded/in-process path). This is how a
  // mutation is attributed to the individual agent rather than a flat,
  // client-asserted string.
  func effectiveActor(r *http.Request, bodyActor string) string {
  	if sub := AuthSubject(r); sub != "" {
  		return sub
  	}
  	return bodyActor
  }
  ```
  Then apply it in each handler:

  `respondPatch` — change the `UpdateIssue` call:
  ```go
  	if err := s.UpdateIssue(r.Context(), key, patch, effectiveActor(r, raw.Actor)); err != nil {
  ```
  `respondTransition` — change the `TransitionIssue` call:
  ```go
  	if err := s.TransitionIssue(r.Context(), key, raw.Status, effectiveActor(r, raw.Actor)); err != nil {
  ```
  `respondAssign` — change the `AssignIssue` call:
  ```go
  	if err := s.AssignIssue(r.Context(), key, raw.Aspect, raw.Team, effectiveActor(r, raw.Actor)); err != nil {
  ```
  `respondComment` — change the `CommentIssue` call:
  ```go
  	if err := s.CommentIssue(r.Context(), key, effectiveActor(r, raw.Actor), raw.Body); err != nil {
  ```
  `handleCreate` — the `Reporter` is the creating identity; override it with the Subject when present:
  ```go
  	reporter := raw.Reporter
  	if sub := AuthSubject(r); sub != "" {
  		reporter = sub
  	}
  	d := IssueDraft{
  		Project: raw.Project, Type: raw.Type, Summary: raw.Summary,
  		Description: raw.Description, DefinitionOfDone: raw.DefinitionOfDone,
  		Priority: raw.Priority, Reporter: reporter, ParentKey: raw.ParentKey,
  		AssigneeAspect: raw.AssigneeAspect, AssigneeTeam: raw.AssigneeTeam,
  		ExternalRefs: raw.ExternalRefs,
  	}
  ```
  (Replace the existing `d := IssueDraft{...Reporter: raw.Reporter...}` block with the above.)
  Run: `go test . -run TestActorTagging 2>&1 | tail -3`
  Expect PASS: `ok  	github.com/CarriedWorldUniverse/ledger`.

- [ ] **5.3 — Confirm actor columns are opaque strings (documented assertion).**
  This is a spec open-question (§9). No code change — verify by inspection that nothing constrains the actor value shape. Run:
  ```
  grep -n "actor\|reporter" /Users/jacinta/Source/ledger/schema.sql
  ```
  Expect: `reporter TEXT NOT NULL`, `events.actor TEXT`, and no `CHECK`/`REFERENCES` constraint on either — confirming herald agent ids (e.g. `agent.anvil`) are accepted unchanged. (If any constraint is found, stop and raise it — it would be a real surface gap. None is expected.)

- [ ] **5.4 — Full regression.**
  Run: `go build ./... && go test ./... 2>&1 | tail -5`
  Expect: clean build; all tests `ok`.

- [ ] **5.5 — Commit.**
  Run:
  ```
  git add rest.go actor_test.go
  git commit -m "ledger-mvp: attribute mutations to the herald Subject on the gateway path

  effectiveActor prefers the injected X-CWB-Subject over the body actor for
  create/transition/assign/comment/patch, so issues/comments/events are
  attributed to the individual agent id. Embedded/in-process callers keep
  body-supplied actors. Actor columns are opaque TEXT (verified) — no schema
  change.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

---

## Task 6 — Containerfile + k3s manifests (cwb ns)

**Outcome:** ledger builds to a static-Go-on-scratch image and deploys to the `cwb` namespace as a Deployment + ClusterIP Service with SQLite on a `local-path` PVC, fronted by the gateway route `/ledger → ledger.cwb.svc`. Mirrors the herald pattern. mTLS on the gateway↔ledger hop is the platform mesh decision (noted, applied at deploy via the mesh annotation/injection, not re-specified here).

**Files:** `cmd/ledger/Containerfile` (new), `deploy/k3s/10-pvc.yaml`, `deploy/k3s/20-deployment.yaml`, `deploy/k3s/30-service.yaml` (new), `deploy/k3s/README.md` (new). The `cwb` namespace already exists (herald's `00-namespace.yaml`); ledger reuses it and does not redefine it.

### Steps

- [ ] **6.1 — Containerfile (mirror herald).**
  Create `/Users/jacinta/Source/ledger/cmd/ledger/Containerfile`:
  ```dockerfile
  # ledger container — tiny static Go binary on scratch.
  # Build from the ledger repo root:
  #   podman build -f cmd/ledger/Containerfile -t ledger:dev .
  #   podman save ledger:dev | sudo k3s ctr images import -
  #
  # Runtime config via env (see cmd/ledger/main.go):
  #   LEDGER_ADDR, LEDGER_DB, LEDGER_AUTH_MODE, LEDGER_JWT_SECRET
  FROM docker.io/library/golang:1.26 AS build
  WORKDIR /src
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ledger ./cmd/ledger

  FROM scratch
  COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
  COPY --from=build /out/ledger /ledger
  EXPOSE 8081
  ENTRYPOINT ["/ledger"]
  ```
  Verify the build compiles statically (locally, without podman):
  Run: `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/ledger-bin ./cmd/ledger && echo OK`
  Expect: `OK`. (The `ncruces/go-sqlite3` driver is a pure-Go WASM build — `CGO_ENABLED=0` is correct and required; this is why it works on scratch with no libc.)

- [ ] **6.2 — PVC.**
  Create `/Users/jacinta/Source/ledger/deploy/k3s/10-pvc.yaml`:
  ```yaml
  apiVersion: v1
  kind: PersistentVolumeClaim
  metadata:
    name: ledger-data
    namespace: cwb
  spec:
    accessModes: ["ReadWriteOnce"]
    storageClassName: local-path
    resources:
      requests:
        storage: 1Gi
  ```

- [ ] **6.3 — Deployment.**
  Create `/Users/jacinta/Source/ledger/deploy/k3s/20-deployment.yaml`:
  ```yaml
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: ledger
    namespace: cwb
    labels:
      app: ledger
  spec:
    replicas: 1
    strategy:
      type: Recreate
    selector:
      matchLabels:
        app: ledger
    template:
      metadata:
        labels:
          app: ledger
      spec:
        containers:
          - name: ledger
            image: localhost/ledger:dev
            imagePullPolicy: Never
            ports:
              - name: http
                containerPort: 8081
            env:
              - name: LEDGER_ADDR
                value: ":8081"
              - name: LEDGER_DB
                value: "/var/lib/nexus/ledger.db"
              - name: LEDGER_AUTH_MODE
                # gateway: trust X-CWB-* injected by the mTLS interchange
                # gateway. ledger is reachable only over that hop (ClusterIP,
                # no ingress of its own), which is what makes header-trust safe.
                value: "gateway"
            volumeMounts:
              - name: data
                mountPath: /var/lib/nexus
            readinessProbe:
              httpGet:
                path: /healthz/issues
                port: http
              initialDelaySeconds: 2
              periodSeconds: 5
            livenessProbe:
              httpGet:
                path: /healthz/issues
                port: http
              initialDelaySeconds: 10
              periodSeconds: 15
        volumes:
          - name: data
            persistentVolumeClaim:
              claimName: ledger-data
  ```

- [ ] **6.4 — Service.**
  Create `/Users/jacinta/Source/ledger/deploy/k3s/30-service.yaml`:
  ```yaml
  apiVersion: v1
  kind: Service
  metadata:
    name: ledger
    namespace: cwb
    labels:
      app: ledger
  spec:
    type: ClusterIP
    selector:
      app: ledger
    ports:
      - name: http
        port: 8081
        targetPort: http
        protocol: TCP
  ```

- [ ] **6.5 — Deploy README (build/load/apply + gateway route + mTLS note).**
  Create `/Users/jacinta/Source/ledger/deploy/k3s/README.md`:
  ```markdown
  # ledger — k3s deploy (cwb namespace)

  ledger is a CWB product: a herald-identified HTTP issue-tracker reached
  only through the interchange-gateway over an mTLS hop. It joins the `cwb`
  namespace defined by herald's `00-namespace.yaml` (not redefined here).

  ## Build + load the image

      podman build -f cmd/ledger/Containerfile -t ledger:dev .
      podman save ledger:dev | sudo k3s ctr images import -

  ## Apply

      kubectl apply -f deploy/k3s/10-pvc.yaml
      kubectl apply -f deploy/k3s/20-deployment.yaml
      kubectl apply -f deploy/k3s/30-service.yaml

  Readiness/liveness probe `/healthz/issues` is public (tokenless) in every
  auth mode, so kubelet can reach it directly.

  ## Gateway route

  Add to the interchange-gateway's route config so `/ledger/*` proxies to
  this service (the gateway strips the `/ledger` prefix, so ledger sees
  clean `/api/...` paths):

      /ledger -> http://ledger.cwb.svc.cluster.local:8081

  The gateway verifies the herald token and injects `X-CWB-{Subject,Org,
  Kind,Scopes}`; ledger runs in `LEDGER_AUTH_MODE=gateway` and trusts those
  headers because the gateway->ledger hop is mTLS and ledger has no ingress
  of its own.

  ## mTLS on the gateway<->ledger hop

  Platform-level decision (`project_cwb_tls_everywhere`): the service mesh
  (Linkerd) or cert-manager internal certs secure the hop. Whichever the
  platform pins is applied here at deploy time (mesh sidecar injection via
  namespace/pod annotation, or a cert volume) — shared across all CWB
  pillars, not redefined per service. No plain-HTTP hop in the path: public
  TLS at Cloudflare, Full-strict to the origin gateway, mTLS gateway<->ledger.
  ```

- [ ] **6.6 — Validate manifests parse.**
  Run (if `kubectl` is available, dry-run client-side; otherwise validate YAML well-formedness):
  ```
  kubectl apply --dry-run=client -f /Users/jacinta/Source/ledger/deploy/k3s/ 2>&1 | head
  ```
  Expect: `... (dry run)` lines for pvc/deployment/service, no parse errors. If `kubectl` is absent, run a YAML lint instead: `python3 -c "import yaml,glob;[list(yaml.safe_load_all(open(f))) for f in glob.glob('/Users/jacinta/Source/ledger/deploy/k3s/*.yaml')];print('YAML OK')"` → `YAML OK`.

- [ ] **6.7 — Commit.**
  Run:
  ```
  git add cmd/ledger/Containerfile deploy/k3s
  git commit -m "ledger-mvp: Containerfile + k3s manifests for the cwb namespace

  Static Go on scratch (CGO_ENABLED=0; pure-Go sqlite driver). Deployment +
  ClusterIP on :8081, SQLite on a local-path PVC, /healthz/issues probes.
  README documents image build/load, the gateway /ledger route, and the
  mTLS-hop trust basis. Reuses herald's cwb namespace.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

---

## Task 7 — cwb-conformance ledger layer (CROSS-REPO)

**Repo:** `/Users/jacinta/Source/cwb-conformance` (separate Go module). **All paths absolute.**

**Outcome:** a `conformance/ledger` layer that, through the gateway with a herald token, exercises create → my/ready → atomic claim (incl. concurrent-claim 409) → comment → transition, asserts actor-tagging, and asserts cross-org isolation.

**Live-green precondition (state it loudly):** this layer asserts against a **deployed ledger** behind a live gateway+herald. Until Task 6's ledger is deployed on the target (dMon), the layer's networked tests will not pass — they are *correctness assertions against the real boundary*, by design (cwb-conformance §1: a true external client, no shared code). The layer is therefore written to **skip with a logged reason** when `CWB_GATEWAY_URL` (or the target's ledger route) is unset, so the suite stays green pre-deploy and turns red only when a deployed ledger misbehaves.

**Reality check on the repo:** `cwb-conformance` is currently **docs-only** (no Go module, no `internal/`, no `cmd/`). The design doc (`docs/2026-05-31-cwb-conformance-design.md`) specifies the full layout (§2) and says the ledger layer lands "after ledger tracker-port" (§8 step 4). The herald/gateway layers (§8 step 1) are the canonical pattern but **don't exist yet either**. So this task scaffolds the **minimal harness slice the ledger layer needs** — `go.mod`, a thin `internal/wire` HTTP client, a thin `internal/target`, and `conformance/ledger` — following the design doc's shapes exactly, without over-building the parts other layers own. If, by the time this task runs, the harness already exists (herald/gateway layers landed first), **skip the scaffolding sub-steps (7.1–7.4) and only add `conformance/ledger`** against the existing `internal/{target,wire,fixtures}` — adapt the layer to the real harness signatures rather than the minimal ones below.

### Steps

- [ ] **7.0 — Detect whether the harness already exists.**
  Run:
  ```
  ls /Users/jacinta/Source/cwb-conformance/go.mod /Users/jacinta/Source/cwb-conformance/internal 2>&1
  ```
  - If both exist → the harness is built by an earlier layer; **skip to 7.5**, importing the real `internal/target` + `internal/wire` (read their actual signatures first with `grep -n "func " /Users/jacinta/Source/cwb-conformance/internal/wire/*.go` and adapt).
  - If they're missing (expected today) → continue with 7.1 to scaffold the minimal slice.

- [ ] **7.1 — Module + target spine.**
  Run: `cd is not needed — use module-root commands.` Create the module file `/Users/jacinta/Source/cwb-conformance/go.mod`:
  ```
  module github.com/CarriedWorldUniverse/cwb-conformance

  go 1.26
  ```
  Create `/Users/jacinta/Source/cwb-conformance/internal/target/target.go` (the subset the ledger layer needs, matching design §3):
  ```go
  // Package target carries the env-agnostic config a conformance run needs
  // to reach a CWB deployment. See docs/2026-05-31-cwb-conformance-design.md §3.
  package target

  import "os"

  type Target struct {
  	Name       string
  	GatewayURL string // e.g. http://dmonextreme.tail41686e.ts.net:8080
  	AdminToken string // herald admin token (env/secret, never committed)
  	LedgerPath string // gateway path prefix for ledger (default "/ledger")
  	HeraldPath string // gateway path prefix for herald (default "/herald")
  }

  // FromEnv loads a target from environment variables. Returns ok=false
  // when the gateway URL is unset, so layers can skip-with-reason rather
  // than fail when pointed at nothing (the pre-deploy state).
  func FromEnv() (*Target, bool) {
  	gw := os.Getenv("CWB_GATEWAY_URL")
  	if gw == "" {
  		return nil, false
  	}
  	t := &Target{
  		Name:       env("CWB_TARGET", "dmon"),
  		GatewayURL: gw,
  		AdminToken: os.Getenv("CWB_ADMIN_TOKEN"),
  		LedgerPath: env("CWB_LEDGER_PATH", "/ledger"),
  		HeraldPath: env("CWB_HERALD_PATH", "/herald"),
  	}
  	return t, true
  }

  func env(k, def string) string {
  	if v := os.Getenv(k); v != "" {
  		return v
  	}
  	return def
  }
  ```

- [ ] **7.2 — Wire: raw HTTP through the gateway.**
  Create `/Users/jacinta/Source/cwb-conformance/internal/wire/http.go` (raw HTTP, real wire, no service-internal imports — design §1):
  ```go
  // Package wire confines all network I/O for the conformance suite. It
  // uses raw net/http through the gateway and never imports a service's
  // internal packages. See docs/2026-05-31-cwb-conformance-design.md §1.
  package wire

  import (
  	"bytes"
  	"context"
  	"io"
  	"net/http"
  	"time"
  )

  // Client is a thin gateway HTTP client. Token is sent as a bearer on
  // every request; the gateway verifies it and injects X-CWB-* downstream.
  type Client struct {
  	BaseURL string // gateway base, e.g. http://host:8080
  	Token   string // herald access token (bearer)
  	HTTP    *http.Client
  }

  func New(baseURL, token string) *Client {
  	return &Client{
  		BaseURL: baseURL,
  		Token:   token,
  		HTTP:    &http.Client{Timeout: 15 * time.Second},
  	}
  }

  // Do issues method+path (path is gateway-absolute, e.g. "/ledger/api/issues")
  // with an optional JSON body, returning status + body bytes.
  func (c *Client) Do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
  	var rdr io.Reader
  	if body != nil {
  		rdr = bytes.NewReader(body)
  	}
  	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
  	if err != nil {
  		return 0, nil, err
  	}
  	if body != nil {
  		req.Header.Set("Content-Type", "application/json")
  	}
  	if c.Token != "" {
  		req.Header.Set("Authorization", "Bearer "+c.Token)
  	}
  	resp, err := c.HTTP.Do(req)
  	if err != nil {
  		return 0, nil, err
  	}
  	defer resp.Body.Close()
  	b, _ := io.ReadAll(resp.Body)
  	return resp.StatusCode, b, nil
  }
  ```

- [ ] **7.3 — Module tidy + compile the harness.**
  Run:
  ```
  go -C /Users/jacinta/Source/cwb-conformance build ./... 2>&1 | head
  ```
  Expect: clean build (no output). (`go -C <dir>` runs in that module without changing the shell's cwd, which matters because the agent's cwd resets between calls.)

- [ ] **7.4 — Commit the harness slice.**
  Run:
  ```
  git -C /Users/jacinta/Source/cwb-conformance add go.mod internal
  git -C /Users/jacinta/Source/cwb-conformance commit -m "ledger-mvp: minimal conformance harness slice for the ledger layer

  go.mod + internal/target (env spine) + internal/wire (raw gateway HTTP),
  following the cwb-conformance design doc shapes. Scoped to what the ledger
  layer needs; herald/gateway layers own the rest of the harness when they land.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

- [ ] **7.5 — The ledger layer (the assertions).**
  Create `/Users/jacinta/Source/cwb-conformance/conformance/ledger/ledger_test.go`. This is a Go test package that skips-with-reason when no target is configured, and otherwise drives the full verb sequence through the gateway.

  The token-minting (herald `/token` exchange) and ephemeral-org provisioning are the `fixtures` package's job per the design doc; that package is built by the herald layer. To keep the ledger layer self-contained pre-fixtures, it reads two ready-made tokens + an org id from the environment (`CWB_LEDGER_TOKEN_A`, `CWB_LEDGER_TOKEN_B`, `CWB_LEDGER_ORG_A`, `CWB_LEDGER_ORG_B`, `CWB_LEDGER_PROJECT`) and skips-with-reason if absent. When `fixtures` lands, swap the env reads for `fixtures.ProvisionOrg` (noted inline).

  ```go
  // Package ledger is the cwb-conformance tracker layer: it drives the
  // ledger daily-driver verbs through the live gateway with a herald token
  // and asserts behaviour at the real boundary. No ledger-internal imports.
  //
  // LIVE-GREEN PRECONDITION: requires a deployed ledger behind a live
  // gateway+herald (the cwb stack on dMon). Skips-with-reason when the
  // target/tokens are unconfigured, so the suite is green pre-deploy and
  // red only when a deployed ledger misbehaves.
  package ledger

  import (
  	"context"
  	"encoding/json"
  	"os"
  	"sync"
  	"testing"

  	"github.com/CarriedWorldUniverse/cwb-conformance/internal/target"
  	"github.com/CarriedWorldUniverse/cwb-conformance/internal/wire"
  )

  type env struct {
  	tgt       *target.Target
  	tokenA    string // org-A agent token
  	tokenB    string // org-B agent token (cross-org isolation)
  	orgA      string
  	project   string // a project in org-A the token may create against
  }

  // loadEnv resolves the layer's live config or returns a skip reason.
  // When the fixtures package lands, replace the token/org env reads with
  // fixtures.ProvisionOrg(t, tgt) (design §4) — the assertions below stay
  // identical.
  func loadEnv(t *testing.T) (env, bool) {
  	t.Helper()
  	tgt, ok := target.FromEnv()
  	if !ok {
  		t.Skip("ledger layer: CWB_GATEWAY_URL unset — no deployed target (pre-deploy state)")
  		return env{}, false
  	}
  	e := env{
  		tgt:     tgt,
  		tokenA:  os.Getenv("CWB_LEDGER_TOKEN_A"),
  		tokenB:  os.Getenv("CWB_LEDGER_TOKEN_B"),
  		orgA:    os.Getenv("CWB_LEDGER_ORG_A"),
  		project: os.Getenv("CWB_LEDGER_PROJECT"),
  	}
  	if e.tokenA == "" || e.project == "" {
  		t.Skip("ledger layer: CWB_LEDGER_TOKEN_A / CWB_LEDGER_PROJECT unset — skipping until fixtures provision them")
  		return env{}, false
  	}
  	return e, true
  }

  func clientA(e env) *wire.Client { return wire.New(e.tgt.GatewayURL, e.tokenA) }
  func clientB(e env) *wire.Client { return wire.New(e.tgt.GatewayURL, e.tokenB) }

  func lpath(e env, sub string) string { return e.tgt.LedgerPath + sub }

  // createIssue posts a story and returns its key.
  func createIssue(ctx context.Context, t *testing.T, c *wire.Client, e env, summary string) string {
  	t.Helper()
  	body, _ := json.Marshal(map[string]any{
  		"project":            e.project,
  		"type":               "Story",
  		"summary":            summary,
  		"definition_of_done": "- [ ] done",
  	})
  	st, b, err := c.Do(ctx, "POST", lpath(e, "/api/issues"), body)
  	if err != nil {
  		t.Fatalf("create: %v", err)
  	}
  	if st != 201 {
  		t.Fatalf("create status = %d: %s", st, b)
  	}
  	var created struct{ Key string }
  	_ = json.Unmarshal(b, &created)
  	if created.Key == "" {
  		t.Fatalf("create returned no key: %s", b)
  	}
  	return created.Key
  }

  func TestLedger_CreateMyReadyClaimCommentTransition(t *testing.T) {
  	e, ok := loadEnv(t)
  	if !ok {
  		return
  	}
  	ctx := context.Background()
  	c := clientA(e)

  	key := createIssue(ctx, t, c, e, "conformance: full verb sequence")

  	// my / ready include the new issue (caller-scoped via the token's Subject).
  	for _, verb := range []string{"/api/issues/my", "/api/issues/ready"} {
  		st, b, err := c.Do(ctx, "GET", lpath(e, verb), nil)
  		if err != nil {
  			t.Fatalf("%s: %v", verb, err)
  		}
  		if st != 200 {
  			t.Fatalf("%s status = %d: %s", verb, st, b)
  		}
  	}

  	// Atomic claim → 200, assignee=caller, status In Progress.
  	st, b, err := c.Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/claim"), []byte(`{}`))
  	if err != nil {
  		t.Fatalf("claim: %v", err)
  	}
  	if st != 200 {
  		t.Fatalf("claim status = %d: %s", st, b)
  	}
  	var claimed struct {
  		AssigneeAspect string `json:"AssigneeAspect"`
  		Status         string `json:"Status"`
  	}
  	_ = json.Unmarshal(b, &claimed)
  	if claimed.Status != "In Progress" {
  		t.Errorf("claimed status = %q, want In Progress", claimed.Status)
  	}

  	// Comment + transition through the workflow.
  	if st, b, _ := c.Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/comments"), []byte(`{"body":"working on it"}`)); st != 201 {
  		t.Fatalf("comment status = %d: %s", st, b)
  	}
  	if st, b, _ := c.Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/transition"), []byte(`{"status":"In Review"}`)); st != 200 {
  		t.Fatalf("transition status = %d: %s", st, b)
  	}
  }

  func TestLedger_ConcurrentClaim409(t *testing.T) {
  	e, ok := loadEnv(t)
  	if !ok {
  		return
  	}
  	ctx := context.Background()
  	c := clientA(e)
  	key := createIssue(ctx, t, c, e, "conformance: concurrent claim race")

  	// Two concurrent claims with the SAME token would be idempotent; the
  	// race that must produce a 409 is two DIFFERENT agents. With only one
  	// token available pre-fixtures, assert the deterministic surface:
  	// claim once (200), then a second claim by a different agent (tokenB,
  	// when present) → 409. When tokenB is absent, assert idempotent re-claim
  	// by the same token is 200 (not a spurious 409).
  	st, _, _ := c.Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/claim"), []byte(`{}`))
  	if st != 200 {
  		t.Fatalf("first claim status = %d, want 200", st)
  	}
  	if e.tokenB != "" && e.orgA != "" {
  		// tokenB is a different agent in a DIFFERENT org → it cannot even
  		// see the issue (cross-org hide-existence) so it gets 404, not 409.
  		// The true same-org-different-agent 409 needs fixtures' two agents
  		// in one org; assert it there. Here, assert tokenB can't claim.
  		stB, _, _ := clientB(e).Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/claim"), []byte(`{}`))
  		if stB == 200 {
  			t.Fatalf("cross-org token claimed issue (status %d) — isolation breach", stB)
  		}
  	} else {
  		// Idempotent re-claim by the same agent → 200.
  		st2, _, _ := c.Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/claim"), []byte(`{}`))
  		if st2 != 200 {
  			t.Fatalf("idempotent re-claim status = %d, want 200", st2)
  		}
  	}

  	// Smoke the actual concurrency path: N parallel claims by the same
  	// token must all resolve (200, idempotent) — no 5xx, no deadlock.
  	const n = 6
  	var wg sync.WaitGroup
  	bad := make([]int, n)
  	for i := 0; i < n; i++ {
  		wg.Add(1)
  		go func(i int) {
  			defer wg.Done()
  			st, _, _ := c.Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/claim"), []byte(`{}`))
  			if st != 200 && st != 409 {
  				bad[i] = st
  			}
  		}(i)
  	}
  	wg.Wait()
  	for i, s := range bad {
  		if s != 0 {
  			t.Fatalf("parallel claim %d got unexpected status %d", i, s)
  		}
  	}
  }

  func TestLedger_CrossOrgIsolation(t *testing.T) {
  	e, ok := loadEnv(t)
  	if !ok {
  		return
  	}
  	if e.tokenB == "" {
  		t.Skip("ledger layer: CWB_LEDGER_TOKEN_B unset — cross-org isolation needs a second-org token (fixtures provides it)")
  		return
  	}
  	ctx := context.Background()
  	key := createIssue(ctx, t, clientA(e), e, "conformance: org-A private")

  	// org-B token must NOT see org-A's issue: hide-existence → 404.
  	st, _, err := clientB(e).Do(ctx, "GET", lpath(e, "/api/issues/"+key), nil)
  	if err != nil {
  		t.Fatalf("cross-org get: %v", err)
  	}
  	if st != 404 {
  		t.Fatalf("cross-org get status = %d, want 404 (hide-existence)", st)
  	}
  }

  func TestLedger_ActorTagging(t *testing.T) {
  	e, ok := loadEnv(t)
  	if !ok {
  		return
  	}
  	ctx := context.Background()
  	c := clientA(e)
  	key := createIssue(ctx, t, c, e, "conformance: actor tagging")

  	// Comment with a LYING body actor; the gateway-injected Subject must
  	// win. We can't read events through the public API directly, so assert
  	// via the materialised markdown (GET /api/issues/{key}) which renders
  	// the timeline with the recorded actor. The body actor "impersonated"
  	// must NOT appear as the comment author.
  	if st, b, _ := c.Do(ctx, "POST", lpath(e, "/api/issues/"+key+"/comments"),
  		[]byte(`{"actor":"impersonated","body":"actor-tagging probe"}`)); st != 201 {
  		t.Fatalf("comment status = %d: %s", st, b)
  	}
  	st, b, err := c.Do(ctx, "GET", lpath(e, "/api/issues/"+key), nil)
  	if err != nil {
  		t.Fatalf("get markdown: %v", err)
  	}
  	if st != 200 {
  		t.Fatalf("get status = %d", st)
  	}
  	if contains(b, "impersonated") {
  		t.Errorf("materialised issue attributes the comment to the body actor 'impersonated' — actor-tagging not enforced")
  	}
  }

  func contains(haystack []byte, needle string) bool {
  	return len(needle) > 0 && indexOf(string(haystack), needle) >= 0
  }

  func indexOf(s, sub string) int {
  	for i := 0; i+len(sub) <= len(s); i++ {
  		if s[i:i+len(sub)] == sub {
  			return i
  		}
  	}
  	return -1
  }
  ```
  Note on the actor-tagging assertion: it is *necessary but not sufficient* (absence of `impersonated` doesn't prove the Subject is present). It's the strongest assertion the public read surface affords without an events endpoint; when the `journey` layer or an events read route lands, tighten it to assert the Subject *positively*. This limitation is recorded here deliberately rather than asserting something false.

- [ ] **7.6 — Compile + run (expect skips pre-deploy).**
  Run:
  ```
  go -C /Users/jacinta/Source/cwb-conformance test ./conformance/ledger/ 2>&1 | tail -8
  ```
  Expect, with no `CWB_*` env set: the tests **SKIP** with the logged reasons (`CWB_GATEWAY_URL unset — no deployed target`), and the package reports `ok` (skips are not failures). This proves the layer compiles and is green pre-deploy. To exercise it against a live target later: set `CWB_GATEWAY_URL`, `CWB_LEDGER_TOKEN_A`, `CWB_LEDGER_PROJECT` (and `CWB_LEDGER_TOKEN_B`/`CWB_LEDGER_ORG_*` for the isolation case) and re-run.

- [ ] **7.7 — Commit.**
  Run:
  ```
  git -C /Users/jacinta/Source/cwb-conformance add conformance/ledger
  git -C /Users/jacinta/Source/cwb-conformance commit -m "ledger-mvp: cwb-conformance ledger layer

  Drives create -> my/ready -> atomic claim (incl. concurrent + cross-org
  guard) -> comment -> transition through the gateway with a herald token,
  plus an actor-tagging probe and cross-org isolation. Skips-with-reason
  until a deployed ledger + provisioned tokens exist (live-green
  precondition). fixtures-provisioned tokens swap in when the herald layer
  lands its fixtures package.

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
  ```

---

## Final verification (whole-plan)

- [ ] **V.1 — ledger module: build + full test + race on the new code.**
  Run: `go -C /Users/jacinta/Source/ledger build ./... && go -C /Users/jacinta/Source/ledger test ./... 2>&1 | tail -8`
  Expect: clean build; `ok` for `github.com/CarriedWorldUniverse/ledger` and `.../cmd/ledger`.
  Run: `go -C /Users/jacinta/Source/ledger test -race . ./cmd/ledger/ 2>&1 | tail -4` → `ok`, no race.

- [ ] **V.2 — Static-binary build (deploy gate).**
  Run: `CGO_ENABLED=0 go -C /Users/jacinta/Source/ledger build -trimpath -o /tmp/ledger-final ./cmd/ledger && echo BUILT`
  Expect: `BUILT`.

- [ ] **V.3 — conformance module compiles + skips clean.**
  Run: `go -C /Users/jacinta/Source/cwb-conformance build ./... && go -C /Users/jacinta/Source/cwb-conformance test ./conformance/ledger/ 2>&1 | tail -4`
  Expect: clean build; `ok` with skip reasons logged.

- [ ] **V.4 — Placeholder + spec-coverage scan.**
  Run: `grep -rn "TODO\|FIXME\|placeholder\|similar to Task" /Users/jacinta/Source/ledger/cmd /Users/jacinta/Source/ledger/claim.go /Users/jacinta/Source/ledger/verbs.go /Users/jacinta/Source/cwb-conformance/conformance 2>&1`
  Expect: no matches (the plan's own "thinking-aloud detour" note in Task 4.4 was resolved to a concrete decision; the final code has none).
  Confirm spec coverage: §8 step 2 → Task 1; step 3 → Task 2; step 4 → Task 3; step 5 → Task 4; step 6 → Task 5; step 7 → Task 6; step 8 → Task 7. §6 API delta (`my`/`ready`/`claim` + X-CWB-* identity) → Tasks 3/4/2. §4 atomic claim → Task 4. §5 data model (no schema change, opaque actor) → Task 5.3.

---

## Assumptions + open issues (carried from spec §9, resolved here)

- **Scope strings** pinned: `issue:read` / `issue:write` / `issue:claim` / `issue:admin` (with `issue:admin` a superset). Align cross-pillar if herald's canonical set differs.
- **Gateway-trust vs heraldauth-direct:** MVP trusts `X-CWB-*` over the mTLS hop; no in-ledger heraldauth (defense-in-depth deferred).
- **Claim target state** pinned to `"In Progress"` (the story-like in-progress state in workflow.go). Epics are not agent-claimable.
- **Actor field types:** confirmed opaque `TEXT` (no constraint) — herald agent ids accepted unchanged; no schema change (verified in Task 5.3).
- **Two new library files (not a rewrite):** `claim.go` (atomic method, needs `s.db`) and one additive export `ContextWithAuth` in `auth.go`, plus `verbs.go` (handlers mounted by the existing `Handler()`). Every existing file's behaviour and every existing signature are unchanged; all existing tests pass. This is consistent with the spec's "library untouched / the only new code is atomic claim" — the context-injection seam is the minimal additive surface the gateway middleware requires, and is flagged as such.
- **teams=nil at MVP:** `my`/`ready` return directly-assigned work; team-membership resolution for the caller has no public library lookup today and is out of scope.
- **mTLS mechanism** (mesh vs cert-manager) is a platform decision applied at deploy; the manifests document the requirement without pinning the mechanism.
- **Task 7 live-green precondition:** the ledger layer asserts against a deployed ledger; it skips-with-reason until deploy + provisioned tokens, and uses env-supplied tokens until the conformance `fixtures` package (herald layer) lands.
- **Task 7 same-org-different-agent 409:** the truest concurrent-claim 409 (two agents, one org) needs the `fixtures` two-agent org; pre-fixtures the layer asserts the deterministic surface (idempotent re-claim 200, cross-org can't-claim, parallel-claim no-5xx). Tighten when fixtures lands.
