# Loom integration seams for the storage capability

`weld add storage` already ships `internal/di/storage_provider.go` guarded by
`when: ["loom"]`, so adding storage to a project that already has loom writes
the provider seam. The remaining wiring is small but touches files the storage
capability does not own, so it is **not** included in `capabilities/storage/**`.
This note is the exact change set for the template repo (or a follow-up
capability) that makes the integration complete and install-order independent.

The storage capability follows the redis pattern: both the storage and loom
capabilities carry the same provider template, each guarded by the other
capability, so whichever is installed second writes `internal/di/storage_provider.go`
exactly once.

## 1. `capabilities/loom/capability.json`

Add the mirror entry to `files` so a loom-first install still writes the seam:

```json
{ "path": "internal/di/storage_provider.go", "source": "files/storage_provider.go.tmpl", "when": ["storage"] }
```

## 2. `capabilities/loom/files/storage_provider.go.tmpl`

Add a byte-for-byte copy of `capabilities/storage/files/storage_provider.go.tmpl`.
The two templates must stay identical; the redis pair is guarded by an equality
test (see step 4).

## 3. `capabilities/loom/files/di.go.tmpl`

Add a guarded provider declaration next to the `redis` block, before
`loom.Provide(NewServer)`:

```go
{{- if .Caps.Has "storage"}}
	// The object store is a declared binding, not part of the default
	// composition: nothing in this graph depends on *objectstore.Store, so Loom
	// prunes it and an unrelated serve never constructs a store and never needs a
	// storage setting. It is defined in storage_provider.go, the stable wiring
	// seam; edit a provider this graph consumes to depend on *objectstore.Store
	// to switch it on.
	loom.Provide(NewObjectStore),
{{- end}}
```

`NewObjectStore` is only referenced when the storage capability is installed, so
a loom-only project never compiles a reference to a missing provider.

## 4. `assets_test.go`

Add the storage contract test mirroring `TestRedisCapabilityIsConfigOnly` and
the equality test mirroring `TestRedisProviderSeamTemplatesMatch`:

- the storage descriptor `requires` is exactly `[base config]`;
- `internal/di/storage_provider.go` is guarded by `when: ["loom"]`;
- the storage `go.mod` patch targets the `deps` marker, and the `config.yml` /
  `config.example.yml` patches target the `config` marker with the
  `config.example.yml` bootstrap;
- the config patches target `configfields`, `configenv` and `configdefaults`;
- `capabilities/storage/files/storage_provider.go.tmpl` equals
  `capabilities/loom/files/storage_provider.go.tmpl`;
- `capabilities/loom/files/di.go.tmpl` contains
  `{{- if .Caps.Has "storage"}}` and `loom.Provide(NewObjectStore)`, and does
  **not** define `func NewObjectStore`.

## 5. `capabilities/loom/files/README.md`

Add a row to the provider table:

| provider | installed when | bound to |
| --- | --- | --- |
| `*objectstore.Store` | `storage` | `objectstore.New` through `NewObjectStore`, defined in the stable `storage_provider.go` seam; **declared but not part of the default composition**. Loom prunes it while nothing depends on `*objectstore.Store`, so a plain serve builds no store and needs no storage setting. When a consumer asks for it, Loom validates the `storage` section, builds the client (it is **not connected** at build, test or startup) and registers a cleanup that releases the store's idle connections exactly once |

## Order independence

With steps 1-3 done, `weld add storage` then `weld add loom` and `weld add loom`
then `weld add storage` both yield the same `internal/di/storage_provider.go`,
because the file is written by whichever capability is installed second and the
two templates are identical. The `goversion` patch is safe in either order: the
storage snippet already writes Loom's `go 1.25.0` floor (the AWS SDK requires
Go 1.24), so a replace-mode patch can never lower the directive.
