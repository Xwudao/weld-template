# internal/api

The JSON HTTP contract of this project. Request and response DTOs carry JSON
serialization tags only; each DTO's `Spec()` declares its go-validate rules
once, and the same `Spec` produces the runtime validation and the OpenAPI 3.1
constraints. `operations()` is the single route table behind both the mux
registration (`Register`) and the document (`Document`, served at
`GET /api/openapi.json`).

The handler is a plain `http.Handler` built with an injected `Service`, so it is
tested with `net/http/httptest` and a fake service: no listener and no database.

## The go-validate dependency is not published yet

This package needs a `github.com/Xwudao/go-validate` version that exposes the
`Spec`/`Constraint`/`Schema` API. That API currently exists only as a local
working copy; the newest published tag is **v0.1.1**, so `go.mod` pins a
placeholder (`v0.2.0`) that cannot be downloaded yet.

Until the new version is published, build against a local checkout (do **not**
commit the replace):

```sh
go mod edit -replace github.com/Xwudao/go-validate=/path/to/go-validate
go mod tidy
go test ./...
```

After the release, publish and pin the real version and remove the placeholder:

```sh
go get github.com/Xwudao/go-validate@<released-version>
```

`go test ./...` also needs the test-only `github.com/getkin/kin-openapi`
dependency that validates the generated document; `go mod tidy` resolves it.
