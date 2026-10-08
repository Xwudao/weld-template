package weldtemplate

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"testing"
)

type descriptor struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Kind     string   `json:"kind"`
	Summary  string   `json:"summary"`
	Requires []string `json:"requires"`
	DI       *struct {
		Dir    string `json:"dir"`
		Source string `json:"source"`
		Test   string `json:"test"`
	} `json:"di"`
	Files []struct {
		Path   string `json:"path"`
		Source string `json:"source"`
	} `json:"files"`
	Patches []struct {
		Path   string `json:"path"`
		Marker string `json:"marker"`
		Source string `json:"source"`
		Mode   string `json:"mode"`
	} `json:"patches"`
}

func readDescriptor(t *testing.T, name string) descriptor {
	t.Helper()
	raw, err := fs.ReadFile(FS(), path.Join("capabilities", name, "capability.json"))
	if err != nil {
		t.Fatalf("%s: read descriptor: %v", name, err)
	}
	var d descriptor
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("%s: invalid descriptor: %v", name, err)
	}
	return d
}

func TestCapabilitiesAreWellFormed(t *testing.T) {
	fsys := FS()
	entries, err := fs.ReadDir(fsys, "capabilities")
	if err != nil {
		t.Fatalf("read capabilities: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no capabilities embedded")
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		dir := path.Join("capabilities", name)
		d := readDescriptor(t, name)
		if d.Name != name {
			t.Errorf("%s: descriptor name is %q", name, d.Name)
		}
		if d.Version == "" {
			t.Errorf("%s: missing version", name)
		}
		switch d.Kind {
		case "base", "add":
		default:
			t.Errorf("%s: unexpected kind %q", name, d.Kind)
		}
		if d.Summary == "" {
			t.Errorf("%s: missing summary", name)
		}
		if len(d.Files) == 0 {
			t.Errorf("%s: no files", name)
		}
		for _, file := range d.Files {
			if file.Path == "" || file.Source == "" {
				t.Errorf("%s: file entry needs path and source", name)
				continue
			}
			if _, err := fs.Stat(fsys, path.Join(dir, file.Source)); err != nil {
				t.Errorf("%s: payload %q missing: %v", name, file.Source, err)
			}
		}
		for _, patch := range d.Patches {
			if patch.Path == "" || patch.Marker == "" || patch.Source == "" {
				t.Errorf("%s: patch entry needs path, marker and source", name)
				continue
			}
			if _, err := fs.Stat(fsys, path.Join(dir, patch.Source)); err != nil {
				t.Errorf("%s: patch payload %q missing: %v", name, patch.Source, err)
			}
		}
	}
}

func TestWebCapabilityRequiresHTTP(t *testing.T) {
	d := readDescriptor(t, "web")
	if len(d.Requires) != 1 || d.Requires[0] != "http" {
		t.Fatalf("web requires = %v, want [http]", d.Requires)
	}
	if len(d.Patches) != 3 {
		t.Fatalf("web patches = %d, want 3", len(d.Patches))
	}
	var routes bool
	for _, file := range d.Files {
		if file.Path == "internal/app/serve.go" {
			t.Error("web still ships a serve command file")
		}
	}
	for _, patch := range d.Patches {
		if patch.Marker == "routes" && patch.Path == "internal/httpserver/http.go" {
			routes = true
		}
	}
	if !routes {
		t.Error("web does not patch the httpserver routes extension point")
	}
}

func TestHTTPCapabilityOwnsTheServeCommand(t *testing.T) {
	d := readDescriptor(t, "http")
	if d.Kind != "add" {
		t.Fatalf("http kind = %q, want add", d.Kind)
	}
	if len(d.Requires) != 1 || d.Requires[0] != "base" {
		t.Fatalf("http requires = %v, want [base]", d.Requires)
	}
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{
		"internal/httpserver/http.go",
		"internal/httpserver/server.go",
		"internal/app/serve.go",
	} {
		if !paths[want] {
			t.Errorf("http capability does not ship %s", want)
		}
	}
	if len(d.Patches) != 0 {
		t.Errorf("http patches = %d, want 0", len(d.Patches))
	}
}

// TestAPICapabilityRequiresHTTP guards the API capability's contract: it needs
// http, patches the go.mod dependency region and the httpserver routes region,
// and depends on neither web nor any database.
func TestAPICapabilityRequiresHTTP(t *testing.T) {
	d := readDescriptor(t, "api")
	if d.Kind != "add" {
		t.Fatalf("api kind = %q, want add", d.Kind)
	}
	if len(d.Requires) != 1 || d.Requires[0] != "http" {
		t.Fatalf("api requires = %v, want [http]", d.Requires)
	}
	for _, forbidden := range []string{"web", "db", "loom"} {
		for _, required := range d.Requires {
			if required == forbidden {
				t.Errorf("api requires %q", forbidden)
			}
		}
	}
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{
		"internal/api/dto.go",
		"internal/api/service.go",
		"internal/api/handler.go",
		"internal/api/openapi.go",
		"internal/httpserver/api_route.go",
	} {
		if !paths[want] {
			t.Errorf("api capability does not ship %s", want)
		}
	}
	var deps, routes bool
	for _, patch := range d.Patches {
		switch {
		case patch.Path == "go.mod" && patch.Marker == "deps":
			deps = true
		case patch.Path == "internal/httpserver/http.go" && patch.Marker == "routes":
			routes = true
		}
	}
	if !deps {
		t.Error("api does not patch the go.mod dependency region")
	}
	if !routes {
		t.Error("api does not patch the httpserver routes extension point")
	}
}

// TestDBCapabilityIsIndependent guards the db capability's contract: it needs
// only base (never http, web, api or loom), patches the go.mod dependency region
// and the Makefile db region, and ships the SQL sources, the pinned sqlc tool
// module, the generated sqlc package and a repository.
func TestDBCapabilityIsIndependent(t *testing.T) {
	d := readDescriptor(t, "db")
	if d.Kind != "add" {
		t.Fatalf("db kind = %q, want add", d.Kind)
	}
	if len(d.Requires) != 1 || d.Requires[0] != "base" {
		t.Fatalf("db requires = %v, want [base]", d.Requires)
	}
	for _, forbidden := range []string{"http", "web", "api", "loom"} {
		for _, required := range d.Requires {
			if required == forbidden {
				t.Errorf("db requires %q", forbidden)
			}
		}
	}
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{
		"sqlc.yaml",
		"db/migrations/000001_create_items.sql",
		"db/query/items.sql",
		"db/tools/go.mod",
		"db/tools/go.sum",
		"internal/data/data.go",
		"internal/data/sqlc/db.go",
		"internal/data/sqlc/models.go",
		"internal/data/sqlc/items.sql.go",
		"internal/data/data_test.go",
		"internal/data/postgres_integration_test.go",
		"internal/data/README.md",
	} {
		if !paths[want] {
			t.Errorf("db capability does not ship %s", want)
		}
	}
	var deps, makefile bool
	for _, patch := range d.Patches {
		switch {
		case patch.Path == "go.mod" && patch.Marker == "deps":
			deps = true
		case patch.Path == "Makefile" && patch.Marker == "db":
			makefile = true
		}
	}
	if !deps {
		t.Error("db does not patch the go.mod dependency region")
	}
	if !makefile {
		t.Error("db does not patch the Makefile db extension point")
	}
}

// TestDBCapabilityPayloadsAreSqlcOutputAndPinnedTool checks the pieces that make
// the capability reproducible: the generated package really is sqlc output, the
// sqlc tool version is pinned in a nested module, and pgx/v5 is pinned in the
// go.mod snippet.
func TestDBCapabilityPayloadsAreSqlcOutputAndPinnedTool(t *testing.T) {
	for _, file := range []string{
		"capabilities/db/files/sqlc/db.go.tmpl",
		"capabilities/db/files/sqlc/models.go.tmpl",
		"capabilities/db/files/sqlc/items.sql.go.tmpl",
	} {
		raw, err := fs.ReadFile(FS(), file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if !strings.Contains(string(raw), "Code generated by sqlc. DO NOT EDIT.") {
			t.Errorf("%s is not sqlc output", file)
		}
	}
	toolsMod, err := fs.ReadFile(FS(), "capabilities/db/files/tools/go.mod.tmpl")
	if err != nil {
		t.Fatalf("read db/tools go.mod: %v", err)
	}
	// Strip comments so the checks below assert the actual directives, not the
	// explanatory header.
	var toolsModCode []string
	for _, line := range strings.Split(string(toolsMod), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		toolsModCode = append(toolsModCode, line)
	}
	toolsModText := strings.Join(toolsModCode, "\n")
	for _, want := range []string{
		"github.com/sqlc-dev/sqlc/cmd/sqlc",
		"github.com/pressly/goose/v3/cmd/goose",
		"github.com/sqlc-dev/sqlc v1.31.1 // indirect",
		"github.com/pressly/goose/v3 v3.28.0 // indirect",
	} {
		if !strings.Contains(toolsModText, want) {
			t.Errorf("db/tools go.mod is missing %q", want)
		}
	}
	if !strings.Contains(toolsModText, "__module__/db/tools") {
		t.Error("db/tools go.mod does not use the project module placeholder")
	}
	snippet, err := fs.ReadFile(FS(), "capabilities/db/files/go.mod.snippet")
	if err != nil {
		t.Fatalf("read db go.mod snippet: %v", err)
	}
	if !strings.Contains(string(snippet), "require github.com/jackc/pgx/v5 v5.7.6") {
		t.Errorf("db go.mod snippet does not pin pgx/v5:\n%s", snippet)
	}
}

// TestDBCapabilityMigrationAndTestSafety guards the destructive-operation
// guardrails: the generated migration targets must use the pinned goose tool
// with an explicit DATABASE_URL guard and a one-step, confirmed down, and the
// generated integration test must isolate itself in its own schema instead of
// dropping application tables.
func TestDBCapabilityMigrationAndTestSafety(t *testing.T) {
	makefile, err := fs.ReadFile(FS(), "capabilities/db/files/Makefile.snippet")
	if err != nil {
		t.Fatalf("read db Makefile snippet: %v", err)
	}
	makeText := string(makefile)
	if strings.Contains(makeText, "psql") {
		t.Errorf("db Makefile snippet still shells out to psql:\n%s", makeText)
	}
	for _, want := range []string{
		"go tool goose",
		`test -n "$$DATABASE_URL"`,
		"CONFIRM",
		"migrate-status:",
	} {
		if !strings.Contains(makeText, want) {
			t.Errorf("db Makefile snippet is missing %q:\n%s", want, makeText)
		}
	}
	// The down target must revert one migration, never every migration.
	if strings.Contains(makeText, "rolls back ALL") || strings.Contains(makeText, "-all") {
		t.Errorf("db Makefile snippet rolls back more than one migration:\n%s", makeText)
	}

	toolsMod, err := fs.ReadFile(FS(), "capabilities/db/files/tools/go.mod.tmpl")
	if err != nil {
		t.Fatalf("read db/tools go.mod: %v", err)
	}
	if !strings.Contains(string(toolsMod), "tool github.com/pressly/goose/v3/cmd/goose") {
		t.Errorf("the goose migration tool is not pinned in db/tools:\n%s", toolsMod)
	}

	migration, err := fs.ReadFile(FS(), "capabilities/db/files/migrations/000001_create_items.sql")
	if err != nil {
		t.Fatalf("read db migration: %v", err)
	}
	for _, want := range []string{"-- +goose Up", "-- +goose Down"} {
		if !strings.Contains(string(migration), want) {
			t.Errorf("db migration is missing %q:\n%s", want, migration)
		}
	}

	test, err := fs.ReadFile(FS(), "capabilities/db/files/postgres_integration_test.go.tmpl")
	if err != nil {
		t.Fatalf("read db integration test: %v", err)
	}
	testText := string(test)
	if strings.Contains(testText, "DROP TABLE") {
		t.Errorf("db integration test drops tables outside its own schema:\n%s", testText)
	}
	for _, want := range []string{
		"CREATE SCHEMA",
		"search_path",
		"DROP SCHEMA",
		"CASCADE",
		"PG_TEST_DSN",
	} {
		if !strings.Contains(testText, want) {
			t.Errorf("db integration test is missing %q", want)
		}
	}
}

// TestBaseCapabilityIsCLIOnly guards the base scaffold against re-acquiring
// server or frontend payloads, which would break capability independence.
func TestBaseCapabilityIsCLIOnly(t *testing.T) {
	d := readDescriptor(t, "base")
	for _, file := range d.Files {
		switch {
		case file.Path == "internal/app/serve.go" ||
			strings.HasPrefix(file.Path, "internal/httpserver/") ||
			strings.HasPrefix(file.Path, "internal/web/") ||
			strings.HasPrefix(file.Path, "web/"):
			t.Errorf("base capability ships capability-specific file %q", file.Path)
		}
		raw, err := fs.ReadFile(FS(), path.Join("capabilities/base", file.Source))
		if err != nil {
			t.Fatalf("read base payload %q: %v", file.Source, err)
		}
		content := string(raw)
		if strings.Contains(content, "net/http") {
			t.Errorf("base payload %q imports net/http", file.Source)
		}
		if strings.Contains(content, "/api/health") {
			t.Errorf("base payload %q mentions the API health endpoint", file.Source)
		}
	}
}

// TestBaseCapabilityShipsInjectedLogger guards the logging milestone: base ships
// a log/slog factory and main logs the failed command through it, with no
// competing error path and no global logger.
func TestBaseCapabilityShipsInjectedLogger(t *testing.T) {
	d := readDescriptor(t, "base")
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{"internal/logging/logging.go", "internal/logging/logging_test.go"} {
		if !paths[want] {
			t.Errorf("base capability does not ship %s", want)
		}
	}

	loggingGo, err := fs.ReadFile(FS(), "capabilities/base/files/logging.go.tmpl")
	if err != nil {
		t.Fatalf("read logging.go: %v", err)
	}
	for _, want := range []string{"\"log/slog\"", "NewTextHandler", "NewJSONHandler"} {
		if !strings.Contains(string(loggingGo), want) {
			t.Errorf("logging.go is missing %q", want)
		}
	}

	mainGo, err := fs.ReadFile(FS(), "capabilities/base/files/main.go.tmpl")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainText := string(mainGo)
	for _, want := range []string{"logging.New", "logger.Error"} {
		if !strings.Contains(mainText, want) {
			t.Errorf("main.go is missing %q", want)
		}
	}
	for _, forbidden := range []string{"fmt.Fprintln", "log.Fatal", "slog.SetDefault"} {
		if strings.Contains(mainText, forbidden) {
			t.Errorf("main.go still uses a competing error path %q:\n%s", forbidden, mainText)
		}
	}
}

// TestHTTPCapabilityInjectsLogger guards the http logging wiring: Serve takes an
// injected *slog.Logger and no longer writes the listening line to stderr
// directly.
func TestHTTPCapabilityInjectsLogger(t *testing.T) {
	serverGo, err := fs.ReadFile(FS(), "capabilities/http/files/server.go.tmpl")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	serverText := string(serverGo)
	if !strings.Contains(serverText, "logger *slog.Logger") {
		t.Errorf("Serve does not accept an injected logger:\n%s", serverText)
	}
	if !strings.Contains(serverText, "logger.Info") {
		t.Errorf("Serve does not log through the injected logger:\n%s", serverText)
	}
	if strings.Contains(serverText, "os.Stderr") || strings.Contains(serverText, "fmt.Fprintf") {
		t.Errorf("server.go still writes to the process streams directly:\n%s", serverText)
	}

	serveGo, err := fs.ReadFile(FS(), "capabilities/http/files/serve.go.tmpl")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	if !strings.Contains(string(serveGo), "logging.New") {
		t.Errorf("serve.go does not build the logger from the base factory:\n%s", serveGo)
	}
}

// TestLoomCapabilityBindsLogger guards the Loom logging wiring: the graph
// provides a *slog.Logger, the managed server consumes it, and the serve
// command leaves startup/shutdown logging to the lifecycle.
func TestLoomCapabilityBindsLogger(t *testing.T) {
	graph, err := fs.ReadFile(FS(), "capabilities/loom/files/di.go.tmpl")
	if err != nil {
		t.Fatalf("read di template: %v", err)
	}
	graphText := string(graph)
	for _, want := range []string{"loom.Provide(NewLogger)", "func NewLogger() *slog.Logger", "logger *slog.Logger", "logging.New"} {
		if !strings.Contains(graphText, want) {
			t.Errorf("di template is missing %q", want)
		}
	}
	if strings.Contains(graphText, "slog.SetDefault") {
		t.Error("di template calls slog.SetDefault; the logger must be injected")
	}

	serveGo, err := fs.ReadFile(FS(), "capabilities/loom/files/serve_loom.go.tmpl")
	if err != nil {
		t.Fatalf("read serve_loom.go: %v", err)
	}
	if strings.Contains(string(serveGo), "fmt.Fprintf(os.Stderr") {
		t.Errorf("serve_loom.go still prints the listening line directly:\n%s", serveGo)
	}
}

// TestLoomCapabilityRequiresHTTPAndDeclaresGraph guards the loom capability's
// contract: it is additive, needs only http, declares a capability-aware DI
// graph, and replaces (never appends to) the serve registration and go directive
// regions.
func TestLoomCapabilityRequiresHTTPAndDeclaresGraph(t *testing.T) {
	d := readDescriptor(t, "loom")
	if d.Kind != "add" {
		t.Fatalf("loom kind = %q, want add", d.Kind)
	}
	if len(d.Requires) != 1 || d.Requires[0] != "http" {
		t.Fatalf("loom requires = %v, want [http]", d.Requires)
	}
	if d.DI == nil || d.DI.Dir != "internal/di" || d.DI.Source == "" || d.DI.Test == "" {
		t.Fatalf("loom di = %+v, want a dir, source and test", d.DI)
	}
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{
		"internal/app/serve_loom.go",
		"tools/loom/go.mod",
		"tools/loom/go.sum",
	} {
		if !paths[want] {
			t.Errorf("loom capability does not ship %s", want)
		}
	}
	var goversion, serve bool
	for _, patch := range d.Patches {
		switch {
		case patch.Path == "go.mod" && patch.Marker == "goversion":
			goversion = patch.Mode == "replace"
		case patch.Path == "internal/app/serve.go" && patch.Marker == "serve":
			serve = patch.Mode == "replace"
		}
	}
	if !goversion {
		t.Error("loom does not replace the go.mod goversion region")
	}
	if !serve {
		t.Error("loom does not replace the serve registration region")
	}
}

// TestLoomCapabilityPinsGeneratorAndRaisesFloor checks the reproducibility
// pieces: the generator is pinned in the nested tools/loom module, the go
// directive region is raised to Loom's floor, and the go.mod snippet requires
// only the Loom runtime.
func TestLoomCapabilityPinsGeneratorAndRaisesFloor(t *testing.T) {
	fsys := FS()
	toolsMod, err := fs.ReadFile(fsys, "capabilities/loom/files/tools/go.mod.tmpl")
	if err != nil {
		t.Fatalf("read loom tools go.mod: %v", err)
	}
	toolsText := string(toolsMod)
	for _, want := range []string{
		"tool github.com/Xwudao/loom/cmd/loom",
		"github.com/Xwudao/loom v0.3.1",
		"__module__/tools/loom",
	} {
		if !strings.Contains(toolsText, want) {
			t.Errorf("loom tools go.mod is missing %q:\n%s", want, toolsText)
		}
	}
	if _, err := fs.ReadFile(fsys, "capabilities/loom/files/tools/go.sum"); err != nil {
		t.Errorf("loom tools go.sum is missing: %v", err)
	}
	goversion, err := fs.ReadFile(fsys, "capabilities/loom/files/goversion.snippet")
	if err != nil {
		t.Fatalf("read goversion snippet: %v", err)
	}
	if !strings.Contains(string(goversion), "go 1.25.0") {
		t.Errorf("goversion snippet = %q, want go 1.25.0", goversion)
	}
	deps, err := fs.ReadFile(fsys, "capabilities/loom/files/go.mod.snippet")
	if err != nil {
		t.Fatalf("read loom go.mod snippet: %v", err)
	}
	if !strings.Contains(string(deps), "require github.com/Xwudao/loom v0.3.1") {
		t.Errorf("loom go.mod snippet does not pin the runtime:\n%s", deps)
	}
	if strings.Contains(string(deps), "golang.org/x/tools") {
		t.Errorf("loom go.mod snippet leaks the generator dependency:\n%s", deps)
	}
	graph, err := fs.ReadFile(fsys, "capabilities/loom/files/di.go.tmpl")
	if err != nil {
		t.Fatalf("read di template: %v", err)
	}
	for _, want := range []string{`{{if .Caps.Has "db"}}`, `{{if .Caps.Has "api"}}`, "NewPool", "NewAPIService", "repositoryService"} {
		if !strings.Contains(string(graph), want) {
			t.Errorf("di template is missing %q", want)
		}
	}
	if strings.Contains(string(graph), "os.Getenv") {
		t.Error("di template reads os.Getenv directly; configuration must use the injected EnvLookup")
	}
}

// TestNoCapabilityRequiresLoom guards the opt-in contract: loom is never pulled
// in by another capability.
func TestNoCapabilityRequiresLoom(t *testing.T) {
	entries, err := fs.ReadDir(FS(), "capabilities")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		d := readDescriptor(t, entry.Name())
		for _, required := range d.Requires {
			if required == "loom" {
				t.Errorf("capability %s requires loom", entry.Name())
			}
		}
	}
}
