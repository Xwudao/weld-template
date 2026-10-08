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
  base/            kind: base   -> scaffolds a brand new project
    capability.json
    files/         main.go, internal/..., Makefile, README, .gitignore
  web/             kind: add    -> extends an existing project
    capability.json
    files/         web/ (React + TS + Vite), embedded-asset Go file, snippets
```

A capability descriptor is JSON:

```json
{
  "name": "web",
  "version": "0.1.0",
  "kind": "add",
  "summary": "...",
  "requires": ["base"],
  "files":   [ { "path": "web/package.json", "source": "files/package.json" } ],
  "patches": [ { "path": "Makefile", "marker": "web", "source": "files/Makefile.snippet" } ]
}
```

- `files` are new files written into the project. Payloads are plain text; the
  CLI substitutes `__name__`, `__module__` and `__version__` (a literal token
  replace, so JSX/JSON braces stay untouched).
- `patches` insert a snippet at an **owned extension point**: a marker pair such
  as `# weld:web:begin` / `# weld:web:end` that the base capability places in a
  file. `weld` never performs arbitrary text replacement.

## Extension points used by `web`

- `Makefile` — `web-build` recipe body and a marker region. `build` already
  depends on `web-build`, so filling the region wires the frontend into the
  build without editing the target definition.
- `.gitignore` — a marker region for the web-specific ignores.
- `internal/server` — the Go extension point is a registered asset filesystem
  (`RegisterAssets`); `web` adds a purely additive `assets_web.go` that embeds
  the Vite output via `//go:embed all:dist`.

## Roadmap

The next capability is `weld add api` (HTTP contract, `go-validate` rule
metadata, OpenAPI). It is intentionally **not** stubbed here: the first slice
proves the minimal → web path only. No arbitrary struct tags or faked OpenAPI
belong in this repo.

## Tests

```sh
go test ./...
```

The test validates that every embedded descriptor parses, that each declared
payload exists, and that `web` declares its `base` requirement and extension
points.
