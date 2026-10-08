# weld-template

Declarative payloads for the [`weld`](../weld) progressive Go scaffold.

`weld` does not hard-code project files. It consumes the payloads in this
module through `weldtemplate.FS()`, an embedded `io/fs.FS`. A released `weld`
binary therefore carries the whole scaffold and its capabilities with it — no
checkout, network access, or environment variable is needed at run time. This
module is the single, versioned seam between the CLI and the payloads.

## Staged architecture

Capabilities are one-way additive. A project is created from exactly one
`base` capability and then grows by adding capabilities, each of which records
its version in the project manifest (`weld.json`).

```
capabilities/
  base/            kind: base   -> scaffolds a minimal, dependency-free CLI
    capability.json
    files/         main.go (builds the logger, logs a failed command),
                   internal/app (dispatch + command registry),
                   internal/logging (log/slog factory, injectable writer),
                   Makefile, README, .gitignore
  http/            kind: add    -> HTTP lifecycle + composable handler builder
    capability.json
    files/         internal/httpserver (server, injectable handler builder),
                   internal/app/serve.go (the one serve command)
  web/             kind: add    -> frontend, requires http
    capability.json
    files/         web/ (React + TS + Vite), internal/web (SPA handler),
                   internal/httpserver/web_route.go, snippets
  api/             kind: add    -> JSON API, requires http
    capability.json
    files/         internal/api (DTOs, go-validate Spec, route table,
                   OpenAPI 3.1 document, httptest-driven tests),
                   internal/httpserver/api_route.go, snippets
  db/              kind: add    -> PostgreSQL, requires base only
    capability.json
    files/         db/migrations (goose), db/query, sqlc.yaml, db/tools (nested
                   module pinning sqlc and goose), internal/data (Repository +
                   pool), generated internal/data/sqlc, snippets
  loom/            kind: add    -> Loom DI graph, requires http, opt-in
    capability.json
    files/         internal/di/di.go.tmpl (capability-aware graph source),
                   internal/app/serve_loom.go, tools/loom (nested module pinning
                   the Loom generator), README
```

`base` is pure CLI: it never imports `net/http`. It ships `internal/logging`, a
small factory over `log/slog` (`New(w io.Writer, format Format, options
*slog.HandlerOptions)`), so the process has one logging protocol with an
injectable sink and no custom logger interface. `http` owns the single `serve`
command and the composable mux; its `Serve` takes the injected `*slog.Logger`.
`web` and `api` each require `http` and
contribute only their own handler through explicit composition points. They are
independent: either can be added first, and both are served by the same `serve`
command.

`db` requires only `base`. It can be added to a CLI-only project, and it is
independent of `http`, `web` and `api`: the repository speaks domain types, not
sqlc rows, and the JSON DTOs stay in `internal/api`. It can therefore be added
in any order relative to the HTTP capabilities.

`loom` requires `http` and is **opt-in**: `web`, `api` and `db` never install
it. It renders `internal/di/di.go` against the installed capability set and
generates `internal/di/loom_gen.go` with the real pinned generator, so the graph
follows whatever of `db` and `api` is installed, in either order. It is the only
capability whose payload is capability aware; every other capability stays
Loom-free. The graph also provides the `*slog.Logger` (via `NewLogger`) that the
managed HTTP server uses to record startup, a serve failure and a graceful
shutdown, so the logger is injected rather than reached for through a global.

## Database: SQL source of truth and generated code

`db` keeps the SQL authoritative. `db/migrations` holds the schema (one goose
migration file per change, with `-- +goose Up` / `-- +goose Down` sections),
`db/query` holds the named sqlc queries, and `sqlc.yaml` reads both and
generates `internal/data/sqlc`. The
hand-written `internal/data/data.go` wraps the generated queries behind a small
`Repository` interface, converts generated rows to a domain `Item`, exposes
`NewPool` for the connection pool and `WithTx` for a narrow transaction.

The generated package is shipped, so `weld add db` produces a project that
compiles and tests immediately with no generator installed and no PostgreSQL
running. Regeneration is reproducible: the sqlc version is pinned by a `tool`
directive in the nested `db/tools/go.mod` module
(`tool github.com/sqlc-dev/sqlc/cmd/sqlc`), alongside the goose migration tool
(`tool github.com/pressly/goose/v3/cmd/goose`). That module is separate on
purpose, so neither tool's large dependency graph enters the application
`go.mod` or its `go test ./...`:

```sh
make sqlc
make migrate-up          # idempotent; requires DATABASE_URL
make migrate-status
CONFIRM=1 make migrate-down   # reverts exactly one migration
# or, from the nested module:
cd db/tools && go tool sqlc generate -f ../../sqlc.yaml
```

The migration targets never fall back to a local database: they refuse to run
when `DATABASE_URL` is unset, and `migrate-down` requires `CONFIRM=1` so it can
never roll back the whole schema by accident.

`pgx/v5` is pinned in the `weld:deps` region of the generated `go.mod` at the
newest release that keeps the project's Go toolchain floor. Generated sqlc code
imports `pgx`; it carries no placeholder tokens and no local path, so nothing
machine-specific is committed into a scaffolded project.

A capability descriptor is JSON:

```json
{
  "name": "web",
  "version": "0.1.0",
  "kind": "add",
  "summary": "...",
  "requires": ["http"],
  "files":   [ { "path": "web/package.json", "source": "files/package.json" } ],
  "patches": [ { "path": "Makefile", "marker": "web", "source": "files/Makefile.snippet" } ]
}
```

- `files` are new files written into the project. Payloads are plain text; the
  CLI substitutes `__name__`, `__module__` and `__version__` (a literal token
  replace, so JSX/JSON braces stay untouched).
- `patches` insert a snippet at an **owned extension point**: a marker pair such
  as `# weld:web:begin` / `# weld:web:end` that the owning capability places in
  a file. `weld` never performs arbitrary text replacement.
- `requires` lists capabilities that must be installed first. `weld add`
  resolves them automatically, so the order in which a user adds capabilities
  does not matter.

Two optional fields are used only by the `loom` capability, which has to vary
with the installed set:

- a `file` or `patch` may carry `when: [cap]` / `whenAbsent: [cap]`, so it is
  planned only when those capabilities are (or are not) installed; and
- a `patch` may carry `mode: "replace"` to rewrite its region instead of
  appending, which is how `loom` sets the `go` directive and the `serve`
  registration.

A capability may also declare a `di` object (`dir`, `source`, `test`). It is a
`text/template` rendered against the installed capability set and formatted as
Go; `loom` uses it for the capability-aware dependency graph. Everything else
stays a plain, unconditional payload.

## Extension point format

An extension point is a `begin`/`end` marker pair whose payload the owning
capability controls. Markers use the host file's comment syntax, so a Makefile
uses `# weld:<marker>:begin` and a Go source uses `// weld:<marker>:begin`.
`weld` recognizes either prefix, which is what lets a Go composition file host
a region.

A snippet carries a **per-capability** installed sentinel,
`weld:<capability>:installed`. The sentinel — not the marker name — makes a
patch idempotent, so several capabilities can append to one region without one
suppressing another. Two capabilities patching region `routes` therefore both
apply, in any installation order, while a repeat `weld add` is a no-op.

`weld` inserts the snippet between the markers and leaves every byte outside
the region untouched, so user edits in the same file survive. A patch target
must be a file `weld` manages (or one created in the same plan); `weld` refuses
to patch an unmanaged file.

## Go route composition

`internal/httpserver/http.go` owns the mux and exposes two seams:

- `NewHandler(routes ...Route) http.Handler` builds a handler from explicit
  routes, so tests compose a mux without a socket or a capability.
- `Handler()` composes the routes capabilities appended through the
  `weld:routes` extension point.

`web` adds `internal/httpserver/web_route.go` (same package) and appends
`installWebRoute` to that region, so no Go import block has to be patched and
no `init` performs route registration. `api` adds
`internal/httpserver/api_route.go` and appends `installAPIRoute` to the same
region, so both compose in either order.

`internal/web` mounts the SPA on `/` and reserves `/api/`: unknown API paths
return 404 instead of the HTML shell, and only browser navigations fall back to
`index.html`. It keeps that exclusion even though a more specific `/api/` route
wins on the mux, because an uninstalled API must not answer with the SPA.

## API contract and OpenAPI

`api` owns `internal/api`. Request and response DTOs carry JSON serialization
tags only; the validation rules live in a single `Spec()` method per DTO, built
from `go-validate`'s `Spec`/`Constraint` constructors. The very same `Spec`
feeds `Spec.Validate()` at runtime and `Spec.Schema()` into the OpenAPI 3.1
document, so the rules and the document cannot drift. An explicit route table
(method, path, operationId, path/query/body/response/error schemas) is the one
source for both the mux registration and the document; a compact adapter maps
go-validate metadata to the small subset of JSON Schema 2020-12 that OpenAPI
3.1 uses. A `Spec` with a constraint that has no machine-readable form (for
example `Custom` or `Conditional`) makes the document build fail instead of
silently omitting the rule.

Enums show the pattern: `ItemStatus` is a typed string alias with named
constants, and `validate.Enum(itemStatusValues()...)` both rejects an invalid
value and exports the same values as the OpenAPI `enum`. If that boilerplate
repeats across resources, a future `weld add enum` step (or go-enum code
generation) is the right home; the API capability deliberately does not grow
its own enum framework.

## go-validate dependency

`api` requires `github.com/Xwudao/go-validate` at a version that exposes the
`Spec`/`Constraint` API. That API is **not published yet** (the newest tag is
v0.1.1), so the generated `go.mod` pins a placeholder (`v0.2.0`) and a local
build needs a `replace` (or `go.work`) pointing at the go-validate working
copy. Releasing this capability requires publishing go-validate and pinning the
real version; the generated project is not otherwise portable. No local path is
committed into any scaffolded file.

## Tests

The test validates that every embedded descriptor parses, that each declared
payload exists, that `base` stays CLI-only, that `http` owns the serve command,
that `web` requires `http` and patches the routes extension point, that `api`
requires `http` (not `web`) and patches both the routes and the `go.mod`
dependency extension points, and that `db` requires only `base`, ships genuine
sqlc output plus a pinned sqlc/goose tool module, and patches the `go.mod`
dependency and Makefile `db` extension points. It also proves the migration
targets are guarded (no `psql`, explicit `DATABASE_URL`, one-step confirmed
down) and the integration test isolates itself in its own schema with no global
`DROP TABLE`. For the logging milestone it proves `base` ships the `log/slog`
factory and `main` logs a failed command through it (no `fmt.Fprintln`, no
process default logger), that `httpserver.Serve` takes an injected logger, and
that the Loom graph provides and injects one.
