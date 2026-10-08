# internal/di

The application's Loom dependency graph. `InitApp` is generated from the graph
declared in `di.go`; `loom_gen.go` is `loom` output and must never be edited by
hand.

## What Loom binds, and why

Loom wires the application from the capabilities that are installed. The graph
is not decorative: the `serve` command runs `InitApp`, so the objects it
constructs are the ones the process uses.

| provider | installed when | bound to |
| --- | --- | --- |
| `EnvLookup` | always | `OsEnv` (`os.LookupEnv`) in production; tests inject a fake |
| `*Config` | always | an immutable config built and validated by `NewConfig`: the listen address (`serve --addr`, else `:8080`) for http, and `DATABASE_URL` for db (only that field when db is installed) |
| `*pgxpool.Pool` | `db` | `data.NewPool`; the pool parses the dsn lazily and is **not** connected at build, test or startup, and its cleanup closes it exactly once |
| `data.Repository` | `db` | `data.NewRepository(pool)` |
| `api.Service` | `api` | the SQL-backed service when `db` is installed, otherwise `api.NewService()` |
| `*httpserver.Server` | always | the composed mux plus its `loom.Hook` start/stop lifecycle |
| `*App` | always | the graph root returned by `InitApp` |

`internal/di` is the only package that knows the whole composition. The
capabilities stay independent: `internal/api` does not import `internal/data`
(an adapter here maps between the storage model and the wire DTOs) and neither
`db` nor `api` imports Loom.

Configuration does not scatter `os.Getenv` through providers: `NewConfig`
receives an injected `EnvLookup`, applies defaults and validation, and returns an
immutable `*Config` that exposes only what the installed capabilities need. When
`db` is installed, `DATABASE_URL` is required: `NewConfig` fails rather than
inventing a default dsn, so a database-backed serve cannot silently connect to a
local database, and an error never echoes the dsn. Unit tests inject a fake
`EnvLookup`, so building and testing never require a database or the variable.

`InitApp` binds the listen address synchronously: a port already in use makes
`lifecycle.Start` fail, and a serve failure is reported to the serve command so
it stops instead of waiting forever. The graph returns the pool's cleanup, which
Loom runs exactly once whether construction later fails or the lifecycle stops.

Because the graph references concrete capability packages, installing a
capability regenerates `di.go` and `loom_gen.go` for the new capability set; the
composition is capability aware and order independent.

## Go toolchain floor

The generated initializer imports `github.com/Xwudao/loom`, whose module
requires Go 1.25. Installing `loom` therefore raises the project's Go directive
to `go 1.25.0`. The generator itself is pinned in `tools/loom`, a nested module,
so its `x/tools` dependency never enters this module.

## Regenerating

```sh
make generate
```

`weld add` already regenerates the graph for you when an installed capability
changes the provider set. Do not edit `loom_gen.go`.

## Tests

`di_test.go` exercises the composed graph with no PostgreSQL: the pool is only
parsed, configuration is driven through an injected `EnvLookup`, the API service
is driven through an injected in-memory repository, and the lifecycle's bind,
serve-failure and graceful-stop behavior is exercised on loopback sockets.
