# internal/di

The application's Loom dependency graph. `InitApp` is generated from the graph
declared in `di.go`; `loom_gen.go` is `loom` output and must never be edited by
hand. `api_provider.go` is the opposite: a stable provider seam weld writes once
and never regenerates, so it is the file you edit to wire dependencies.

## What Loom binds, and why

Loom wires the application from the capabilities that are installed. The graph
is not decorative: the `serve` command runs `InitApp`, so the objects it
constructs are the ones the process uses.

| provider | installed when | bound to |
| --- | --- | --- |
| `EnvLookup` | always | `OsEnv` (`os.LookupEnv`) in production; tests inject a fake |
| `*Config` | always | an immutable config built by `NewConfig`: the listen address (`serve --addr`, else `:8080`). The database fields are loaded into `*Config` but validated only by the pool |
| `*slog.Logger` | always | `NewLogger`, which builds the base `log/slog` handler on stderr; injected into the server so startup, a serve failure and a shutdown are logged through one protocol, with no process default logger and no custom logger interface |
| `*pgxpool.Pool` | `db` | `data.NewPool`; **declared but not part of the default composition**. Loom prunes it while nothing depends on `data.Repository`, so a plain serve never opens a pool. When a consumer asks for the repository it validates the database section, parses the dsn lazily (it is **not** connected at build, test or startup) and registers a cleanup that closes it exactly once |
| `data.Repository` | `db` | `data.NewRepository(pool)`; the binding exists so a user's own provider can consume it, not so the default graph constructs it |
| `*redis.Client` | `redis` | `redisclient.New` through `NewRedisClient`, defined in the stable `redis_provider.go` seam; **declared but not part of the default composition**. Loom prunes it while nothing depends on `*redis.Client`, so a plain serve builds no client and needs no Redis setting. When a consumer asks for it, Loom validates the `redis` section, builds the lazy client (it is **not connected** at build, test or startup) and registers a cleanup that closes it exactly once |
| `api.Service` | `api` | defined by `api_provider.go` (the stable wiring seam), defaulting to `api.NewService()`, the in-memory development demo. Installing `db` or `redis` does **not** switch it to PostgreSQL or Redis |
| `*mailsender.Sender` | `mail` | `mailsender.New` through `NewMailSender`, defined in the stable `mail_provider.go` seam; **declared but not part of the default composition**. Loom prunes it while nothing depends on `*mailsender.Sender`, so a plain serve builds no sender and needs no mail setting. When a consumer asks for it, Loom validates the `mail` section and builds the sender (it is **not connected** at build, test or startup) |
| `*objectstore.Store` | `storage` | `objectstore.New` through `NewObjectStore`, defined in the stable `storage_provider.go` seam; **declared but not part of the default composition**. Loom prunes it while nothing depends on `*objectstore.Store`, so a plain serve builds no store and needs no storage setting. When a consumer asks for it, Loom validates the `storage` section, builds the client (it is **not connected** at build, test or startup) and registers a cleanup that releases the store's idle connections exactly once |
| `*cron.Scheduler` | `cron` | `cron.New` through `NewScheduler`, defined in the stable `cron_provider.go` seam. Unlike the bindings above, the graph root consumes it, so Loom always constructs it and its start/stop hooks run with the server. `NewScheduler` takes the server, so the socket is bound before the scheduler starts and the scheduler stops before the server. It starts no jobs until `internal/cron/register.go` registers them |
| `*httpserver.Server` | always | the composed mux plus its `loom.Hook` start/stop lifecycle |
| `*App` | always | the graph root returned by `InitApp` |

`internal/di` is the only package that knows the whole composition. The
capabilities stay independent: `internal/api` does not import `internal/data`
and neither `db` nor `api` imports Loom.

## The database is wired by hand

`weld add db` installs a capability, not a connection. The graph declares the
pool and repository providers above, but nothing in the default composition
depends on `data.Repository`, so Loom prunes them: serving the API needs no
database credential and opens no socket. The API keeps the in-memory
development demo (`api.NewService()`), and items are lost on restart.

When you want persistence, edit `api_provider.go`, the stable provider seam,
rather than `di.go`, which is regenerated:

```go
// internal/di/api_provider.go, after `weld add db`
func NewAPIService(repo data.Repository) api.Service {
	return myService{repo: repo} // your repository-backed api.Service
}
```

weld writes `api_provider.go` once, when both `api` and `loom` are installed,
and never rewrites it while it regenerates `di.go`/`loom_gen.go`, so this edit
survives every later `weld add`. Loom infers the provider's dependencies from its
signature: once `NewAPIService` takes `data.Repository`, Loom constructs
`NewPool` and `NewRepository` too. `NewPool` validates the database settings
through `config.Database.ValidateDatabase`, so the credential is required only
when the pool is actually built, never for an unrelated serve. See
`internal/data/README.md` for the pool, repository and transaction seams.

### Redis is wired by hand too

`weld add redis` installs an available `*redis.Client` binding in the same way:
the provider `NewRedisClient` lives in `internal/di/redis_provider.go`, another
stable seam weld writes once (when both `redis` and `loom` are installed,
whichever arrives second) and never regenerates. The default graph declares it
but does not depend on `*redis.Client`, so Loom prunes it and serving needs no
Redis setting. To use it, make a provider the graph already consumes depend on
`*redis.Client` — for an api+loom project, edit `api_provider.go`:

```go
// internal/di/api_provider.go, after `weld add redis`
func NewAPIService(client *redis.Client) api.Service {
	return myService{client: client} // your Redis-backed api.Service
}
```

`NewRedisClient` never dials and never pings: `go-redis` connects on the first
command, so building the graph, running tests and starting the process need no
Redis server, and the lifecycle closes the client exactly once when it was
constructed. See `internal/redisclient/README.md`.

### Mail and storage are declared but pruned

`weld add mail` and `weld add storage` follow the redis pattern: `NewMailSender`
and `NewObjectStore` live in the stable `mail_provider.go` and
`storage_provider.go` seams (written once, by whichever of the capability and
`loom` is installed second), and the graph declares them but nothing depends on
`*mailsender.Sender` or `*objectstore.Store`, so Loom prunes them. An ordinary
serve builds neither and needs neither setting. To use one, make a provider the
graph already consumes depend on it (again, edit `api_provider.go` for an
api+loom project). Construction never connects and never sends. See
`internal/mailsender/README.md` and `internal/objectstore/README.md`.

### Cron is wired, not pruned

`weld add cron` is different: the graph root consumes `*cron.Scheduler`, so Loom
constructs `NewScheduler` (in the stable `cron_provider.go` seam) and registers
its `loom.Hook` start/stop. `NewScheduler` takes the `*httpserver.Server`, and
Loom constructs providers in dependency order and appends hooks as it
constructs them, so the socket is bound before the scheduler starts and the
scheduler stops before the server — the same guarantee the Loom serve command
relies on. The scheduler starts no jobs until `internal/cron/register.go`
registers them, and it takes no database or Redis lock. See `internal/cron/README.md`.

Never wire providers into `di.go`: it is regenerated from the installed
capability set, and an edit there is erased by the next capability install.

## Configuration

Configuration does not scatter `os.Getenv` through providers: `NewConfig`
receives an injected `EnvLookup`, applies defaults and HTTP validation, and
returns an immutable `*Config`. The database section is read into `*Config` but
validated only by `NewPool`, so installing `db` without using it never requires
`DATABASE_URL`. When the pool is built, a missing credential fails loudly rather
than inventing a default dsn, and an error never echoes the dsn. Unit tests
inject a fake `EnvLookup`, so building and testing never require a database or
the variable.

`InitApp` binds the listen address synchronously: a port already in use makes
`lifecycle.Start` fail, and a serve failure is reported to the serve command so
it stops instead of waiting forever. When the pool is constructed, Loom runs its
cleanup exactly once whether construction later fails or the lifecycle stops.

Because the graph references concrete capability packages, installing a
capability regenerates `di.go` and `loom_gen.go` for the new capability set; the
composition is capability aware and order independent. Installing `db` changes
which bindings are declared, never what the default root constructs.

## Go toolchain floor

The generated initializer imports `github.com/Xwudao/loom`, whose module
requires Go 1.25. Installing `loom` therefore raises the project's Go directive
to `go 1.25.0`. The generator itself is pinned in `tools/loom`, a nested module,
so its `x/tools` dependency never enters this module.

## Regenerating

```sh
make generate
```

`weld add` already regenerates `di.go`, `di_test.go` and `loom_gen.go` for you
when an installed capability changes the provider set. Do not edit them. The
stable provider seams — `api_provider.go`, `redis_provider.go`,
`mail_provider.go`, `storage_provider.go` and `cron_provider.go` — are the
exception: weld writes each once (when the capability and `loom` are both
installed, whichever arrives second) and never regenerates it, so edit those
files to wire dependencies.

## Tests

`di_test.go` exercises the composed graph with no PostgreSQL: configuration is
driven through an injected `EnvLookup`, the API service is served in memory over
a real loopback socket, and the lifecycle's bind, serve-failure and
graceful-stop behavior is checked on loopback sockets. The lifecycle tests inject
a logger backed by an `io.Discard` or in-memory writer, so startup and shutdown
logging is asserted without touching the process streams.
