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
  config/          kind: add    -> typed YAML configuration, requires base
    capability.json
    files/         internal/config (typed structs, injectable loader, Secret),
                   config.yml (local, git-ignored), config.example.yml, snippets
  http/            kind: add    -> HTTP lifecycle + composable handler builder
    capability.json
    files/         internal/httpserver (server, injectable handler builder),
                   internal/app/serve.go (the one serve command),
                   config.yml / config.example.yml http section (snippet)
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
  redis/           kind: add    -> opt-in Redis client, requires base + config
    capability.json
    files/         internal/redisclient (lazy go-redis client, caller owns the
                   lifecycle), internal/config/redis.go (typed connection
                   settings), snippets that extend the shared config, README
  loom/            kind: add    -> Loom DI graph, requires http, opt-in
    capability.json
    files/         internal/di/di.go.tmpl (capability-aware graph source),
                   internal/app/serve_loom.go, tools/loom (nested module pinning
                   the Loom generator), stable provider seams, README
  cron/            kind: add    -> in-process scheduler, requires base + config
    capability.json
    files/         internal/cron (registry + stable register.go job seam),
                   internal/app/cron.go (the serve runtime),
                   internal/config/cron.go, snippets, README
  mail/            kind: add    -> opt-in SMTP sender, requires base + config
    capability.json
    files/         internal/mailsender (per-Send SMTP client),
                   internal/config/mail.go,
                   internal/di/mail_provider.go (stable Loom seam), README
  storage/         kind: add    -> opt-in S3 client, requires base + config
    capability.json
    files/         internal/objectstore (streaming S3-compatible client),
                   internal/config/storage.go,
                   internal/di/storage_provider.go (stable Loom seam), README

modules/           per-name    -> `weld add module <name>` payload (not a capability)
  module.json      files: internal/modules/<name> (typed DTOs, Service seam,
                   Go handler, httptest test, README), the non-Loom route seam
                   internal/httpserver/<name>_route.go, and the weld:routes
                   snippet; rendered once per module name

commands/          per-name    -> `weld add command <name>` / `--command` payload
  command.json     generic: an independent internal/commands/<name> group plus
                   its internal/app/<name>_command.go registration; module: the
                   same target paths backed by the module's Service. Both
                   variants declare one target set, rendered once per name
```

`base` is pure CLI: it never imports `net/http` and ships no configuration. It
ships `internal/logging`, a small factory over `log/slog` (`New(w io.Writer,
format Format, options *slog.HandlerOptions)`), so the process has one logging
protocol with an injectable sink and no custom logger interface.

`config` is the shared configuration capability, installed automatically by the
first of `http` and `db`. It ships `internal/config`: typed Go structs, a loader
with injectable `readFile`/`lookupEnv` dependencies, and a redacting `Secret`
type. Configuration is `config.yml`, read from the working directory by default
(`--config` overrides the path), overridden by the environment and then by a
command-line flag, so the precedence is flag, environment, file, defaults. A
missing or invalid file is an error rather than a silent fallback to defaults.
`config.yml` is generated locally and git-ignored; `config.example.yml` is
committed. The file carries a marker region that each capability appends to, so
adding `db` later merges its `database` section without clobbering the user's
edits — including a local database password. Because `config.yml` is git-ignored,
a fresh clone has no local file; adding `http` or `db` restores it from the
tracked `config.example.yml` (its declared `bootstrap`) before appending, and a
missing or corrupt example is an actionable error rather than a silent default.
See [Configuration](#configuration) below.

`http` owns the single `serve` command and the composable mux; its `Serve` takes
the injected `*slog.Logger` and its `serve` command loads the listen address
through the shared `config` loader. `web` and `api` each require `http` and
contribute only their own handler through explicit composition points. They are
independent: either can be added first, and both are served by the same `serve`
command.

`db` requires `base` and `config`. It can be added to a CLI-only project, and it
is independent of `http`, `web` and `api`: the repository speaks domain types,
not sqlc rows, and the JSON DTOs stay in `internal/api`. It can therefore be
added in any order relative to the HTTP capabilities, and adding it appends its
`database` section to the local `config.yml`.

`redis` is likewise independent: it requires only `base` and `config`, so it can
be added to a CLI-only project and composed with `http`/`db`/`api` in any order.
It installs an opt-in client, never a connection: `internal/redisclient` builds a
lazy `go-redis` client from the typed `redis` configuration section (added to
`config.yml` through the same extension point as `database`), and the caller owns
its lifecycle. Nothing in the generated application imports the client, so an
ordinary build, test or serve needs no Redis server; `weld add redis` never wires
Redis into a service.

`loom` requires `http` and is **opt-in**: `web`, `api`, `db`, `redis`, `mail`,
`storage` and `cron` never install it. It renders `internal/di/di.go` against the
installed capability set and generates `internal/di/loom_gen.go` with the real
pinned generator, so the graph follows whatever of `db`, `api`, `redis`, `mail`,
`storage` and `cron` is installed, in either order. It is the only capability
whose payload is capability aware; every other capability stays Loom-free. The
graph also provides the `*slog.Logger` (via `NewLogger`) that the managed HTTP
server uses to record startup, a serve failure and a graceful shutdown, so the
logger is injected rather than reached for through a global. The `db`, `redis`,
`mail` and `storage` bindings are declared but pruned until a provider depends
on them; `cron` is different — the graph root consumes the scheduler, so the
scheduler is constructed and its lifecycle hooks run with the server (the socket
is bound before it starts and it stops before the server).

`cron` requires `base` and `config` and is opt-in. It ships an in-process
scheduler (`internal/cron`) with a stable, user-editable registration file
(`internal/cron/register.go`) that declares no jobs, so installing the capability
schedules nothing. `internal/app/cron.go` registers a runtime with the shared
`config` runtime seam, and the `serve` command starts that runtime after binding
its socket and stops it on shutdown; `help`, `version` and every short command
never start it. The plain `serve` command serves under a signal-canceled context
and shuts the scheduler and HTTP server down within a bounded timeout; with Loom
installed, the stable `internal/di/cron_provider.go` seam builds the scheduler
and registers its hooks. The scheduler takes no database or Redis lock.

`mail` and `storage` likewise require only `base` and `config`. `mail` ships an
SMTP sender (`internal/mailsender`) with typed, secret-redacted configuration and
an explicit TLS policy; `storage` ships a streaming S3-compatible client
(`internal/objectstore`) that never creates the bucket. Neither is imported by
the generated application, and each declares a stable Loom provider seam
(`internal/di/mail_provider.go`, `internal/di/storage_provider.go`) that the
graph declares but prunes until a provider depends on it. Both capabilities and
`loom` carry the same provider template guarded by the other, so whichever is
installed second writes the seam and the install order does not matter.

## Configuration

`config` is a first-class, shared capability: `http` and `db` both require it, so
the first of them installs it and the user never adds it by hand. `base` stays a
minimal CLI with no configuration and no `config.yml`.

`internal/config` holds typed Go structs (`Config`, `HTTP`, `Database`) with
`yaml` tags for names only — there are no validation tags. `Loader.Load` reads
`config.yml` from the working directory by default (`--config` overrides the
path), then applies environment overrides and finally the explicit code
defaults, so precedence is flag, environment, file, defaults. The loader takes
its `readFile` and `lookupEnv` as dependencies (`NewLoader`), which is what makes
the loader testable without the filesystem or the process environment; the plain
`serve` command and the Loom graph both build the process loader with
`NewOsLoader`, so there is one configuration path rather than one per
composition.

The typed struct is closed but extensible: `config.go` carries three marker
regions (`weld:configfields`, `weld:configenv`, `weld:configdefaults`) where a
later capability appends its own field and registers its environment overrides
and defaults. A capability that needs a new section adds the field, the redacting
type and the accessors in a same-package file — `redis` adds `internal/config/redis.go`
and the `redis` section — so the base loader never changes. A project whose
`config.go` was generated before these regions existed has no such marker, so
`weld add redis` fails with the missing extension point instead of rewriting the
file.

A missing file and an invalid file are distinct errors, and neither is a silent
fallback to defaults: an absent `config.yml`, a malformed one, or an explicitly
set-but-blank `HTTP_ADDR` fails loudly. A parse error never quotes the file
content, so a malformed secret cannot reach a log record.

`Secret` is a string that redacts itself in `fmt`, `log/slog`, JSON and YAML
output; `Value()` is the one accessor that reveals it, and `Config.DSN()` is the
only place the database password is read. The database section is validated only
where the database is actually used (`ValidateDatabase`), so an http-only project
never needs a database setting, and no credential is ever defaulted: host and
port have defaults, but the user, password and name do not.

`internal/config/runtime.go` is the shared long-running-runtime seam. A
capability that must start and stop work with `serve` registers a `RuntimeFactory`
from an init function; the `serve` command builds and starts the factories after
binding its socket and stops them on shutdown, and short commands never do. This
is how `cron` follows `serve` without the serve command knowing cron exists, and
it works in either add order because `config` is installed by every capability
that runs a long-lived process.

`config.yml` is generated locally and listed in `.gitignore`;
`config.example.yml` is committed and documents the shape. The file carries a
`weld:config` marker region, and each capability appends only the section it
needs (`http` appends `http`, `db` appends `database`, `redis` appends `redis`),
so a db-only project does not carry a gratuitous http section. Because the
append is marker-based and sentinel-guarded, adding a capability later preserves
every byte outside the region and every value inside it — including a database
or Redis password edited by the user — and a repeat add is a no-op.

A patch that targets `config.yml` declares `config.example.yml` as its
`bootstrap`. On a fresh clone the git-ignored local file is absent while the
manifest still records it and the committed example remains, so adding `http`,
`db` or `redis` restores `config.yml` from the example before appending its
section, rather than failing on the missing target. The restore only runs when the local file is
absent, so it never overwrites a local value or comment, and the written file is
recorded in the manifest with its hash. If the example is absent or no longer
carries the marker region, the add fails with the exact restore instruction
(for example `git checkout config.example.yml`) instead of falling back to
defaults; the generated runtime still errors on a missing `config.yml` until the
file exists.

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

## Business modules (`modules/`)

`modules/` is not a capability. A capability declares a fixed set of files;
a business module is per-name, so `modules/module.json` declares a *template*
that weld renders once per `weld add module <name>`, substituting the module
name into the paths and the package, the constructor names and the URL segment.

The payload generates `internal/modules/<name>`: a typed request and response,
a `Service` interface and a `NewService` seam, a Go `net/http` handler, an
`httptest`-driven test and a README. Without Loom, weld also writes
`internal/httpserver/<name>_route.go` and appends one line to the `weld:routes`
region; with Loom, the regenerated `internal/di` graph provides
`<name>.NewService` and registers the module on the composed mux. The generated
service is a replaceable in-memory example, not persistence. Unlike generated
capability files, module files are written once and never regenerated.

The module is recorded in `weld.json`'s `modules` list, not the capability list.

## Command groups (`commands/`)

`commands/` is not a capability either. `commands/command.json` declares two
variants that write the *same* target paths for a name, so a name is always one
command group in `internal/commands/<name>` plus one registration file in
`internal/app/<name>_command.go`:

- `generic` — the independent group written by `weld add command <name>`: no
  HTTP, configuration or database dependency, showing help on a bare invocation.
- `module` — the group written by `weld add module <name> --command`: backed by
  the module package's `Service` interface and `NewService` constructor, so the
  command line and the HTTP handler share one business layer.

Both register through the base app's `RegisterCommand` seam, so there is one
root command tree and no per-project global dispatcher. The command files are
written once and never regenerated; the command is recorded in `weld.json`'s
`commands` list, not the capability list.

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
payload exists, that `base` stays CLI-only and ships no configuration, that
`config` needs only `base` and patches the git-ignore and `go.mod` dependency
regions, that `http` owns the serve command and appends its section to the
`config` files, that `web` requires `http` and patches the routes extension
point, that `api` requires `http` (not `web`) and patches both the routes and
the `go.mod` dependency extension points, and that `db` requires `base` and
`config`, ships genuine sqlc output plus a pinned sqlc/goose tool module, and
patches the `go.mod` dependency, Makefile `db` and both `config` extension
points. It also proves the migration
targets are guarded (no `psql`, explicit `DATABASE_URL`, one-step confirmed
down) and the integration test isolates itself in its own schema with no global
`DROP TABLE`. For the logging milestone it proves `base` ships the `log/slog`
factory and `main` logs a failed command through it (no `fmt.Fprintln`, no
process default logger), that the `serve` command records the listening and
shutdown lines through the injected logger while `httpserver.Serve` itself never
writes to the process streams, and that the Loom graph provides and injects one.
