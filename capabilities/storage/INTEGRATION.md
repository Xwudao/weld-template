# Loom integration for the storage capability

The Loom wiring for storage is complete and install-order independent. Two
pieces make it so:

1. `internal/di/storage_provider.go` is written by whichever of `storage` and
   `loom` is installed second. Both capabilities carry the same template, each
   guarded by the other capability:

   - `capabilities/storage/capability.json` declares it with `when: ["loom"]`;
   - `capabilities/loom/capability.json` declares it with `when: ["storage"]`.

   The two templates are byte-identical, so the install order cannot change the
   generated file.

2. `capabilities/loom/files/di.go.tmpl` declares `loom.Provide(NewObjectStore)`
   inside a `{{- if .Caps.Has "storage"}}` block. Nothing in the default graph
   depends on `*objectstore.Store`, so Loom prunes the provider: an ordinary
   serve constructs no store and needs no storage setting. To use it, make a
   provider the graph already consumes depend on `*objectstore.Store`.

Because the graph is regenerated on every capability change, no manual edit to
`internal/di/di.go` is ever needed, and `storage_provider.go` is never
regenerated, so user wiring there survives later adds.
