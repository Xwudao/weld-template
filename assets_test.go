package weldtemplate

import (
	"encoding/json"
	"go/format"
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
		Path       string   `json:"path"`
		Source     string   `json:"source"`
		When       []string `json:"when"`
		WhenAbsent []string `json:"whenAbsent"`
	} `json:"files"`
	Patches []struct {
		Path       string   `json:"path"`
		Marker     string   `json:"marker"`
		Source     string   `json:"source"`
		Mode       string   `json:"mode"`
		Bootstrap  string   `json:"bootstrap"`
		When       []string `json:"when"`
		WhenAbsent []string `json:"whenAbsent"`
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
	if strings.Join(d.Requires, ",") != "base,config" {
		t.Fatalf("http requires = %v, want [base config]", d.Requires)
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
	// http appends its section to the config capability's local and example
	// YAML, so a db-only project never carries a gratuitous http section, and it
	// declares the tracked example to restore the git-ignored local file from.
	var configLocal, configExample bool
	for _, patch := range d.Patches {
		switch {
		case patch.Path == "config.yml" && patch.Marker == "config":
			configLocal = true
			if patch.Bootstrap != "config.example.yml" {
				t.Errorf("http config.yml patch bootstrap = %q, want config.example.yml", patch.Bootstrap)
			}
		case patch.Path == "config.example.yml" && patch.Marker == "config":
			configExample = true
		}
	}
	if !configLocal || !configExample {
		t.Errorf("http does not append its section to the config files: %+v", d.Patches)
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
		"internal/di/api_provider.go",
	} {
		if !paths[want] {
			t.Errorf("api capability does not ship %s", want)
		}
	}
	// The API provider file is created only once loom is also installed: when
	// api is added after loom. It carries the loom guard so it is never written
	// when loom is absent.
	var providerSeam bool
	for _, file := range d.Files {
		if file.Path == "internal/di/api_provider.go" {
			providerSeam = len(file.When) == 1 && file.When[0] == "loom" && len(file.WhenAbsent) == 0
		}
	}
	if !providerSeam {
		t.Errorf("api api_provider.go is not guarded by when: [loom]: %+v", d.Files)
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
	if strings.Join(d.Requires, ",") != "base,config" {
		t.Fatalf("db requires = %v, want [base config]", d.Requires)
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
	var deps, makefile, configLocal, configExample bool
	for _, patch := range d.Patches {
		switch {
		case patch.Path == "go.mod" && patch.Marker == "deps":
			deps = true
		case patch.Path == "Makefile" && patch.Marker == "db":
			makefile = true
		case patch.Path == "config.yml" && patch.Marker == "config":
			configLocal = true
			if patch.Bootstrap != "config.example.yml" {
				t.Errorf("db config.yml patch bootstrap = %q, want config.example.yml", patch.Bootstrap)
			}
		case patch.Path == "config.example.yml" && patch.Marker == "config":
			configExample = true
		}
	}
	if !deps {
		t.Error("db does not patch the go.mod dependency region")
	}
	if !makefile {
		t.Error("db does not patch the Makefile db extension point")
	}
	if !configLocal || !configExample {
		t.Error("db does not merge its section into the local config and the committed example")
	}
}

// TestConfigCapabilityContract guards the config capability: it needs only base,
// ships the shared loader plus the local and example YAML, and patches the
// git-ignore and go.mod dependency regions.
func TestConfigCapabilityContract(t *testing.T) {
	d := readDescriptor(t, "config")
	if d.Kind != "add" {
		t.Fatalf("config kind = %q, want add", d.Kind)
	}
	if len(d.Requires) != 1 || d.Requires[0] != "base" {
		t.Fatalf("config requires = %v, want [base]", d.Requires)
	}
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{"internal/config/config.go", "internal/config/config_test.go", "config.example.yml", "config.yml"} {
		if !paths[want] {
			t.Errorf("config capability does not ship %s", want)
		}
	}
	var gitignore, deps bool
	for _, patch := range d.Patches {
		switch {
		case patch.Path == ".gitignore" && patch.Marker == "config":
			gitignore = true
		case patch.Path == "go.mod" && patch.Marker == "deps":
			deps = true
		}
	}
	if !gitignore {
		t.Error("config does not patch the git-ignore region")
	}
	if !deps {
		t.Error("config does not patch the go.mod dependency region")
	}

	// The loader keeps DB validation separate and redacts secrets, so a local
	// password never reaches a log and an http-only project needs no DB setting.
	loader, err := fs.ReadFile(FS(), "capabilities/config/files/config.go.tmpl")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	loaderText := string(loader)
	for _, want := range []string{
		"type Secret string",
		"func (Secret) LogValue() slog.Value",
		"func (Secret) MarshalJSON()",
		"func (c *Config) ValidateDatabase() error",
		"func NewLoader(readFile ReadFileFunc, lookupEnv LookupEnvFunc) *Loader",
		"gopkg.in/yaml.v3",
	} {
		if !strings.Contains(loaderText, want) {
			t.Errorf("config.go is missing %q", want)
		}
	}
	if strings.Contains(loaderText, "os.Getenv") {
		t.Error("config.go reads os.Getenv directly; the loader must take an injectable lookup")
	}

	// The example documents the database section and the local file is ignored.
	gitignoreSnippet, err := fs.ReadFile(FS(), "capabilities/config/files/gitignore.snippet")
	if err != nil {
		t.Fatalf("read gitignore snippet: %v", err)
	}
	if !strings.Contains(string(gitignoreSnippet), "config.yml") {
		t.Errorf("git-ignore snippet does not ignore config.yml:\n%s", gitignoreSnippet)
	}
	depsSnippet, err := fs.ReadFile(FS(), "capabilities/config/files/go.mod.snippet")
	if err != nil {
		t.Fatalf("read go.mod snippet: %v", err)
	}
	if !strings.Contains(string(depsSnippet), "gopkg.in/yaml.v3") {
		t.Errorf("config go.mod snippet does not require yaml.v3:\n%s", depsSnippet)
	}

	// The base scaffold owns the git-ignore extension point config patches.
	baseGitignore, err := fs.ReadFile(FS(), "capabilities/base/files/gitignore.txt")
	if err != nil {
		t.Fatalf("read base gitignore: %v", err)
	}
	if !strings.Contains(string(baseGitignore), "# weld:config:begin") {
		t.Error("base .gitignore is missing the weld:config extension point")
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

// TestBaseCapabilityShipsCobraCommandSeams guards the base contract after the
// Cobra migration: the base app builds a Cobra command tree and exposes the
// three contribution seams a capability or business module uses, go.mod pins
// Cobra (with its own requirements) and go.sum ships with the scaffold so a
// fresh project builds with no network access.
func TestBaseCapabilityShipsCobraCommandSeams(t *testing.T) {
	appGo, err := fs.ReadFile(FS(), "capabilities/base/files/app.go.tmpl")
	if err != nil {
		t.Fatalf("read base app.go: %v", err)
	}
	appText := string(appGo)
	for _, want := range []string{
		"github.com/spf13/cobra",
		"type CommandFactory func() *cobra.Command",
		"func RegisterCommand(",
		"func ConfigureRoot(",
		"func SetDefaultRun(",
		"func NewRootCommand() *cobra.Command",
	} {
		if !strings.Contains(appText, want) {
			t.Errorf("base app.go is missing the command seam %q", want)
		}
	}

	mod, err := fs.ReadFile(FS(), "capabilities/base/files/go.mod.tmpl")
	if err != nil {
		t.Fatalf("read base go.mod: %v", err)
	}
	modText := string(mod)
	for _, want := range []string{"require github.com/spf13/cobra v1.9.1", "github.com/spf13/pflag", "github.com/inconshreveable/mousetrap"} {
		if !strings.Contains(modText, want) {
			t.Errorf("base go.mod does not pin %q", want)
		}
	}
	if !strings.Contains(modText, "weld:deps:begin") {
		t.Error("base go.mod lost the weld:deps extension point")
	}
	if _, err := fs.ReadFile(FS(), "capabilities/base/files/go.sum"); err != nil {
		t.Errorf("base go.sum is missing: %v", err)
	}
}

// TestLoomServeSnippetRebuildsTheCobraServe guards the Loom replacement of the
// serve registration: it must re-register the Loom serve command, keep the
// shared root flags and keep a bare invocation running the graph, so the Cobra
// migration did not silently drop --config/--addr on a Loom project.
func TestLoomServeSnippetRebuildsTheCobraServe(t *testing.T) {
	snippet, err := fs.ReadFile(FS(), "capabilities/loom/files/serve.snippet")
	if err != nil {
		t.Fatalf("read loom serve snippet: %v", err)
	}
	snippetText := string(snippet)
	for _, want := range []string{
		"RegisterCommand(newServeLoomCommand)",
		"ConfigureRoot(registerServeFlags)",
		"SetDefaultRun(runServeLoom)",
	} {
		if !strings.Contains(snippetText, want) {
			t.Errorf("loom serve snippet is missing %q:\n%s", want, snippetText)
		}
	}
}

// TestHTTPCapabilityInjectsLogger guards the http logging wiring: the serve
// command builds the injected logger from the base factory and records the
// listening and shutdown lines through it, while server.go never writes to the
// process streams directly.
func TestHTTPCapabilityInjectsLogger(t *testing.T) {
	serveGo, err := fs.ReadFile(FS(), "capabilities/http/files/serve.go.tmpl")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	serveText := string(serveGo)
	for _, want := range []string{"logging.New", "logger.Info", "listening"} {
		if !strings.Contains(serveText, want) {
			t.Errorf("serve.go is missing %q:\n%s", want, serveText)
		}
	}

	serverGo, err := fs.ReadFile(FS(), "capabilities/http/files/server.go.tmpl")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	serverText := string(serverGo)
	if strings.Contains(serverText, "os.Stderr") || strings.Contains(serverText, "fmt.Fprintf") {
		t.Errorf("server.go still writes to the process streams directly:\n%s", serverText)
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
	for _, want := range []string{`{{if .Caps.Has "db"}}`, `{{if .Caps.Has "api"}}`, "NewPool", "loom.Provide(NewAPIService)"} {
		if !strings.Contains(string(graph), want) {
			t.Errorf("di template is missing %q", want)
		}
	}
	// The provider is defined in the stable api_provider.go seam, not in the
	// regenerated graph: a provider in di.go would be erased by the next
	// capability install, taking the user's persistence wiring with it.
	if strings.Contains(string(graph), "func NewAPIService") {
		t.Error("di template defines NewAPIService; the provider must live in the stable api_provider.go")
	}
	if strings.Contains(string(graph), "repositoryService") {
		t.Error("di template still auto-wires the repository into the API service")
	}
	if strings.Contains(string(graph), "os.Getenv") {
		t.Error("di template reads os.Getenv directly; configuration must use the injected EnvLookup")
	}
}

// TestLoomProviderSeamIsStableAndOrderIndependent guards the durable api+db
// wiring seam: the graph references a provider defined in
// internal/di/api_provider.go, a file weld writes once (when api and loom are
// both installed, whichever arrives second) and never regenerates. loom
// declares it guarded by api; the api capability declares the identical
// template guarded by loom, so exactly one of them writes the file in either
// install order.
func TestLoomProviderSeamIsStableAndOrderIndependent(t *testing.T) {
	loomTemplate, err := fs.ReadFile(FS(), "capabilities/loom/files/api_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read loom api_provider template: %v", err)
	}
	apiTemplate, err := fs.ReadFile(FS(), "capabilities/api/files/api_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read api api_provider template: %v", err)
	}
	if string(loomTemplate) != string(apiTemplate) {
		t.Error("the api and loom api_provider.go templates differ; the install order would change the generated file")
	}
	for _, want := range []string{
		"func NewAPIService() api.Service",
		"return api.NewService()",
		"__module__/internal/api",
	} {
		if !strings.Contains(string(loomTemplate), want) {
			t.Errorf("api_provider.go template is missing %q:\n%s", want, loomTemplate)
		}
	}

	d := readDescriptor(t, "loom")
	var providerSeam bool
	for _, file := range d.Files {
		if file.Path != "internal/di/api_provider.go" {
			continue
		}
		providerSeam = file.Source == "files/api_provider.go.tmpl" &&
			len(file.When) == 1 && file.When[0] == "api" && len(file.WhenAbsent) == 0
	}
	if !providerSeam {
		t.Errorf("loom api_provider.go is not declared with source files/api_provider.go.tmpl guarded by when: [api]: %+v", d.Files)
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

// TestRedisCapabilityIsConfigOnly guards the redis capability's contract: it
// needs only base and config (never http, db, api or loom), ships the typed
// config extension and the lazy client package, and declares its Loom provider
// seam guarded on loom.
func TestRedisCapabilityIsConfigOnly(t *testing.T) {
	d := readDescriptor(t, "redis")
	if d.Kind != "add" {
		t.Fatalf("redis kind = %q, want add", d.Kind)
	}
	if strings.Join(d.Requires, ",") != "base,config" {
		t.Fatalf("redis requires = %v, want [base config]", d.Requires)
	}
	for _, forbidden := range []string{"http", "web", "api", "db", "loom"} {
		for _, required := range d.Requires {
			if required == forbidden {
				t.Errorf("redis requires %q", forbidden)
			}
		}
	}
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{
		"internal/config/redis.go",
		"internal/config/redis_test.go",
		"internal/redisclient/redisclient.go",
		"internal/redisclient/redisclient_test.go",
		"internal/redisclient/README.md",
		"internal/di/redis_provider.go",
	} {
		if !paths[want] {
			t.Errorf("redis capability does not ship %s", want)
		}
	}
	var providerSeam bool
	for _, file := range d.Files {
		if file.Path == "internal/di/redis_provider.go" {
			providerSeam = len(file.When) == 1 && file.When[0] == "loom" && len(file.WhenAbsent) == 0
		}
	}
	if !providerSeam {
		t.Errorf("redis redis_provider.go is not guarded by when: [loom]: %+v", d.Files)
	}
	var deps, configLocal, configExample bool
	markers := map[string]bool{}
	for _, patch := range d.Patches {
		switch {
		case patch.Path == "go.mod" && patch.Marker == "deps":
			deps = true
		case patch.Path == "config.yml" && patch.Marker == "config":
			configLocal = true
			if patch.Bootstrap != "config.example.yml" {
				t.Errorf("redis config.yml patch bootstrap = %q, want config.example.yml", patch.Bootstrap)
			}
		case patch.Path == "config.example.yml" && patch.Marker == "config":
			configExample = true
		case patch.Path == "internal/config/config.go":
			markers[patch.Marker] = true
		}
	}
	if !deps {
		t.Error("redis does not patch the go.mod dependency region")
	}
	if !configLocal || !configExample {
		t.Error("redis does not merge its section into the local config and the committed example")
	}
	for _, want := range []string{"configfields", "configenv", "configdefaults"} {
		if !markers[want] {
			t.Errorf("redis does not patch the config.go %s extension point: %+v", want, d.Patches)
		}
	}

	// The client is lazy and free of the HTTP, JSON and data surfaces, and the
	// config file keeps the connection settings behind the redacting Secret.
	client, err := fs.ReadFile(FS(), "capabilities/redis/files/redisclient.go.tmpl")
	if err != nil {
		t.Fatalf("read redisclient template: %v", err)
	}
	clientText := string(client)
	if !strings.Contains(clientText, "github.com/redis/go-redis/v9") {
		t.Error("redisclient does not build a go-redis client")
	}
	for _, forbidden := range []string{"net/http", "encoding/json", "internal/data"} {
		if strings.Contains(clientText, forbidden) {
			t.Errorf("redisclient payload references %s", forbidden)
		}
	}
	config, err := fs.ReadFile(FS(), "capabilities/redis/files/redis_config.go.tmpl")
	if err != nil {
		t.Fatalf("read redis config template: %v", err)
	}
	for _, want := range []string{"type Redis struct", "Username Secret", "Password Secret", "func (c *Config) ValidateRedis() error"} {
		if !strings.Contains(string(config), want) {
			t.Errorf("redis config is missing %q", want)
		}
	}

	// Loom's graph is capability aware for redis too.
	graph, err := fs.ReadFile(FS(), "capabilities/loom/files/di.go.tmpl")
	if err != nil {
		t.Fatalf("read di template: %v", err)
	}
	for _, want := range []string{`{{- if .Caps.Has "redis"}}`, "loom.Provide(NewRedisClient)"} {
		if !strings.Contains(string(graph), want) {
			t.Errorf("loom di template is missing %q", want)
		}
	}
	if strings.Contains(string(graph), "func NewRedisClient") {
		t.Error("di template defines NewRedisClient; the provider must live in the stable redis_provider.go")
	}
}

// TestRedisProviderSeamTemplatesMatch proves the redis and loom copies of the
// provider seam are byte-identical, so the install order cannot change the
// generated file.
func TestRedisProviderSeamTemplatesMatch(t *testing.T) {
	redisTemplate, err := fs.ReadFile(FS(), "capabilities/redis/files/redis_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read redis redis_provider template: %v", err)
	}
	loomTemplate, err := fs.ReadFile(FS(), "capabilities/loom/files/redis_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read loom redis_provider template: %v", err)
	}
	if string(redisTemplate) != string(loomTemplate) {
		t.Error("the redis and loom redis_provider.go templates differ; the install order would change the generated file")
	}
	for _, want := range []string{"func NewRedisClient(cfg *config.Config) (*redis.Client, loom.Cleanup, error)", "__module__/internal/redisclient"} {
		if !strings.Contains(string(redisTemplate), want) {
			t.Errorf("redis_provider.go template is missing %q:\n%s", want, redisTemplate)
		}
	}
}

// TestCronCapabilityIsConfigOnly guards the cron capability's contract: it needs
// only base and config, ships the scheduler library plus a stable registration
// file and the serve runtime, and declares its Loom provider seam guarded on
// loom.
func TestCronCapabilityIsConfigOnly(t *testing.T) {
	d := readDescriptor(t, "cron")
	if d.Kind != "add" {
		t.Fatalf("cron kind = %q, want add", d.Kind)
	}
	if strings.Join(d.Requires, ",") != "base,config" {
		t.Fatalf("cron requires = %v, want [base config]", d.Requires)
	}
	for _, forbidden := range []string{"http", "web", "api", "db", "loom"} {
		for _, required := range d.Requires {
			if required == forbidden {
				t.Errorf("cron requires %q", forbidden)
			}
		}
	}
	paths := map[string]string{}
	for _, file := range d.Files {
		paths[file.Path] = file.Source
	}
	for _, want := range []string{
		"internal/cron/cron.go",
		"internal/cron/register.go",
		"internal/cron/README.md",
		"internal/config/cron.go",
		"internal/config/cron_test.go",
		"internal/app/cron.go",
		"internal/app/cron_test.go",
		"internal/di/cron_provider.go",
	} {
		if _, ok := paths[want]; !ok {
			t.Errorf("cron does not ship %s", want)
		}
	}
	var providerSeam bool
	for _, file := range d.Files {
		if file.Path == "internal/di/cron_provider.go" {
			providerSeam = len(file.When) == 1 && file.When[0] == "loom" && len(file.WhenAbsent) == 0
		}
	}
	if !providerSeam {
		t.Errorf("cron cron_provider.go is not guarded by when: [loom]: %+v", d.Files)
	}
	markers := map[string]bool{}
	for _, patch := range d.Patches {
		switch {
		case patch.Path == "go.mod" && patch.Marker == "deps":
			markers["go.mod|deps"] = true
		case patch.Path == "config.yml" && patch.Marker == "config":
			if patch.Bootstrap != "config.example.yml" {
				t.Errorf("cron config.yml patch bootstrap = %q, want config.example.yml", patch.Bootstrap)
			}
			markers["config.yml|config"] = true
		case patch.Path == "config.example.yml" && patch.Marker == "config":
			markers["config.example.yml|config"] = true
		case patch.Path == "internal/config/config.go":
			markers["config.go|"+patch.Marker] = true
		}
	}
	for _, want := range []string{
		"go.mod|deps",
		"config.yml|config",
		"config.example.yml|config",
		"config.go|configfields",
		"config.go|configenv",
		"config.go|configdefaults",
	} {
		if !markers[want] {
			t.Errorf("cron does not patch %s: %+v", want, d.Patches)
		}
	}

	// The scheduler follows serve through the shared config runtime seam, and it
	// does not schedule anything by itself.
	appRuntime, err := fs.ReadFile(FS(), "capabilities/cron/files/app_runtime.go.tmpl")
	if err != nil {
		t.Fatalf("read cron app runtime: %v", err)
	}
	appText := string(appRuntime)
	for _, want := range []string{"config.RegisterRuntime", "cron.New", "cron.Register", "scheduler.Start", "scheduler.Stop"} {
		if !strings.Contains(appText, want) {
			t.Errorf("cron app runtime is missing %q:\n%s", want, appText)
		}
	}
	register, err := fs.ReadFile(FS(), "capabilities/cron/files/register.go.tmpl")
	if err != nil {
		t.Fatalf("read cron register: %v", err)
	}
	if !strings.Contains(string(register), "func Register(s *Scheduler) error") {
		t.Errorf("cron register file does not expose Register:\n%s", register)
	}
	if !strings.Contains(string(register), "return nil") {
		t.Errorf("cron register file registers jobs by default:\n%s", register)
	}

	// Loom declares the provider and the graph root consumes the scheduler, so it
	// is not pruned.
	graph, err := fs.ReadFile(FS(), "capabilities/loom/files/di.go.tmpl")
	if err != nil {
		t.Fatalf("read loom di template: %v", err)
	}
	for _, want := range []string{`{{- if .Caps.Has "cron"}}`, "loom.Provide(NewScheduler)", "Scheduler *cron.Scheduler"} {
		if !strings.Contains(string(graph), want) {
			t.Errorf("loom di template is missing %q", want)
		}
	}
	if strings.Contains(string(graph), "func NewScheduler") {
		t.Error("loom di template defines NewScheduler; the provider must live in cron_provider.go")
	}
}

// TestCronProviderSeamTemplatesMatch proves the cron and loom copies of the
// provider seam are byte-identical, so the install order cannot change the
// generated file.
func TestCronProviderSeamTemplatesMatch(t *testing.T) {
	cronTemplate, err := fs.ReadFile(FS(), "capabilities/cron/files/cron_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read cron provider template: %v", err)
	}
	loomTemplate, err := fs.ReadFile(FS(), "capabilities/loom/files/cron_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read loom cron provider template: %v", err)
	}
	if string(cronTemplate) != string(loomTemplate) {
		t.Error("the cron and loom cron_provider.go templates differ; the install order would change the generated file")
	}
	for _, want := range []string{
		"func NewScheduler(lc *loom.Lifecycle, cfg *config.Config, logger *slog.Logger, server *Server) (*cron.Scheduler, error)",
		"__module__/internal/cron",
		"loom.Hook",
	} {
		if !strings.Contains(string(cronTemplate), want) {
			t.Errorf("cron_provider.go template is missing %q:\n%s", want, cronTemplate)
		}
	}
}

// TestMailCapabilityIsConfigOnly guards the mail capability's contract: it needs
// only base and config, ships the typed config extension and the sender package,
// and declares its Loom provider seam guarded on loom.
func TestMailCapabilityIsConfigOnly(t *testing.T) {
	d := readDescriptor(t, "mail")
	if d.Kind != "add" {
		t.Fatalf("mail kind = %q, want add", d.Kind)
	}
	if strings.Join(d.Requires, ",") != "base,config" {
		t.Fatalf("mail requires = %v, want [base config]", d.Requires)
	}
	for _, forbidden := range []string{"http", "web", "api", "db", "loom"} {
		for _, required := range d.Requires {
			if required == forbidden {
				t.Errorf("mail requires %q", forbidden)
			}
		}
	}
	paths := map[string]bool{}
	for _, file := range d.Files {
		paths[file.Path] = true
	}
	for _, want := range []string{
		"internal/config/mail.go",
		"internal/config/mail_test.go",
		"internal/mailsender/mailsender.go",
		"internal/mailsender/mailsender_test.go",
		"internal/mailsender/README.md",
		"internal/di/mail_provider.go",
	} {
		if !paths[want] {
			t.Errorf("mail capability does not ship %s", want)
		}
	}
	var providerSeam bool
	for _, file := range d.Files {
		if file.Path == "internal/di/mail_provider.go" {
			providerSeam = len(file.When) == 1 && file.When[0] == "loom" && len(file.WhenAbsent) == 0
		}
	}
	if !providerSeam {
		t.Errorf("mail mail_provider.go is not guarded by when: [loom]: %+v", d.Files)
	}
	markers := map[string]bool{}
	for _, patch := range d.Patches {
		if patch.Path == "config.yml" && patch.Marker == "config" && patch.Bootstrap != "config.example.yml" {
			t.Errorf("mail config.yml patch bootstrap = %q, want config.example.yml", patch.Bootstrap)
		}
		if patch.Path == "internal/config/config.go" {
			markers[patch.Marker] = true
		}
	}
	for _, want := range []string{"configfields", "configenv", "configdefaults"} {
		if !markers[want] {
			t.Errorf("mail does not patch the config.go %s extension point: %+v", want, d.Patches)
		}
	}

	// Loom declares the provider; the graph prunes it until a consumer asks.
	graph, err := fs.ReadFile(FS(), "capabilities/loom/files/di.go.tmpl")
	if err != nil {
		t.Fatalf("read loom di template: %v", err)
	}
	if !strings.Contains(string(graph), "loom.Provide(NewMailSender)") {
		t.Error("loom di template does not declare NewMailSender")
	}
	if strings.Contains(string(graph), "func NewMailSender") {
		t.Error("loom di template defines NewMailSender; the provider must live in mail_provider.go")
	}
}

// TestMailProviderSeamTemplatesMatch proves the mail and loom copies of the
// provider seam are byte-identical.
func TestMailProviderSeamTemplatesMatch(t *testing.T) {
	mailTemplate, err := fs.ReadFile(FS(), "capabilities/mail/files/mail_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read mail provider template: %v", err)
	}
	loomTemplate, err := fs.ReadFile(FS(), "capabilities/loom/files/mail_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read loom mail provider template: %v", err)
	}
	if string(mailTemplate) != string(loomTemplate) {
		t.Error("the mail and loom mail_provider.go templates differ; the install order would change the generated file")
	}
}

// TestStorageProviderSeamTemplatesMatch proves the storage and loom copies of the
// provider seam are byte-identical.
func TestStorageProviderSeamTemplatesMatch(t *testing.T) {
	storageTemplate, err := fs.ReadFile(FS(), "capabilities/storage/files/storage_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read storage provider template: %v", err)
	}
	loomTemplate, err := fs.ReadFile(FS(), "capabilities/loom/files/storage_provider.go.tmpl")
	if err != nil {
		t.Fatalf("read loom storage provider template: %v", err)
	}
	if string(storageTemplate) != string(loomTemplate) {
		t.Error("the storage and loom storage_provider.go templates differ; the install order would change the generated file")
	}
}

// TestLoomDeclaresAuxiliaryProviders pins the loom-side declarations that make
// the mail, storage and cron integrations install-order independent: the graph
// declares the pruned bindings and the served scheduler, and the mirror files
// are declared guarded by the capability they mirror.
func TestLoomDeclaresAuxiliaryProviders(t *testing.T) {
	d := readDescriptor(t, "loom")
	want := map[string]string{"mail": "", "storage": "", "cron": ""}
	for _, file := range d.Files {
		switch file.Path {
		case "internal/di/mail_provider.go":
			want["mail"] = file.Source
		case "internal/di/storage_provider.go":
			want["storage"] = file.Source
		case "internal/di/cron_provider.go":
			want["cron"] = file.Source
		}
		for _, name := range []string{"mail", "storage", "cron"} {
			if file.Path == "internal/di/"+name+"_provider.go" {
				if len(file.When) != 1 || file.When[0] != name {
					t.Errorf("loom %s_provider.go is not guarded by when: [%s]: %+v", name, name, file)
				}
			}
		}
	}
	for name, source := range want {
		if source == "" {
			t.Errorf("loom does not declare the %s provider mirror", name)
		}
	}
}

// TestConfigCapabilityShipsRuntimeSeam guards the shared long-running runtime
// seam the cron scheduler follows serve through: config ships it, so a
// capability can register a runtime without the serve command knowing it.
func TestConfigCapabilityShipsRuntimeSeam(t *testing.T) {
	raw, err := fs.ReadFile(FS(), "capabilities/config/files/runtime.go.tmpl")
	if err != nil {
		t.Fatalf("read config runtime seam: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"type Runtime interface",
		"type RuntimeFactory func",
		"func RegisterRuntime(",
		"func StartRuntimes(",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("config runtime seam is missing %q", want)
		}
	}
}

// TestModuleTemplateIsWellFormed guards the per-name `weld add module` payload:
// every declared source exists, every target carries the __modname__ token so the
// per-module substitution produces a unique path, and every Go payload renders
// to gofmt-clean Go for a sample module name.
func TestModuleTemplateIsWellFormed(t *testing.T) {
	fsys := FS()
	raw, err := fs.ReadFile(fsys, "modules/module.json")
	if err != nil {
		t.Fatalf("read modules/module.json: %v", err)
	}
	var d struct {
		Version string `json:"version"`
		Files   []struct {
			Path   string `json:"path"`
			Source string `json:"source"`
		} `json:"files"`
		Route struct {
			Path   string `json:"path"`
			Source string `json:"source"`
		} `json:"route"`
		RouteSnippet string `json:"routeSnippet"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("invalid modules/module.json: %v", err)
	}
	if d.Version == "" {
		t.Fatal("module.json needs a version")
	}
	if len(d.Files) == 0 {
		t.Fatal("module.json declares no files")
	}

	// sources maps each declared payload to its target path. A target path must
	// carry the module-name token: that substitution is what makes a per-module
	// path unique.
	sources := map[string]string{}
	for _, file := range d.Files {
		if !strings.Contains(file.Path, "__modname__") {
			t.Errorf("module file path %q does not carry __modname__", file.Path)
		}
		sources[file.Source] = file.Path
	}
	if !strings.Contains(d.Route.Path, "__modname__") {
		t.Errorf("module route path %q does not carry __modname__", d.Route.Path)
	}
	sources[d.Route.Source] = d.Route.Path
	for source := range sources {
		if _, err := fs.Stat(fsys, path.Join("modules", source)); err != nil {
			t.Errorf("module template payload %q missing: %v", source, err)
		}
	}

	snippet, err := fs.ReadFile(fsys, path.Join("modules", d.RouteSnippet))
	if err != nil {
		t.Fatalf("read route snippet %q: %v", d.RouteSnippet, err)
	}
	if !strings.Contains(string(snippet), "weld:module:__modname__:installed") {
		t.Errorf("route snippet is missing the module sentinel:\n%s", snippet)
	}

	// A Go payload must be gofmt-clean once the placeholders are substituted, so
	// a generated project builds without a formatting pass.
	replacer := strings.NewReplacer(
		"__name__", "demo",
		"__module__", "example.com/demo",
		"__version__", d.Version,
		"__modname__", "widget",
		"__ModName__", "Widget",
	)
	for source := range sources {
		if !strings.HasSuffix(source, ".go.tmpl") {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join("modules", source))
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		rendered := []byte(replacer.Replace(string(body)))
		if _, err := format.Source(rendered); err != nil {
			t.Errorf("%s does not render to valid Go: %v\n%s", source, err, rendered)
		}
	}
}

// TestCommandTemplateIsWellFormed guards the per-name `weld add command` and
// `weld add module --command` payloads: every declared source exists, every
// target carries the __modname__ token so the per-command substitution produces
// a unique path, the two variants declare the same targets (a name is one
// command group, never two), and every Go payload renders to gofmt-clean Go.
func TestCommandTemplateIsWellFormed(t *testing.T) {
	fsys := FS()
	raw, err := fs.ReadFile(fsys, "commands/command.json")
	if err != nil {
		t.Fatalf("read commands/command.json: %v", err)
	}
	var d struct {
		Version string `json:"version"`
		Generic struct {
			Files []struct {
				Path   string `json:"path"`
				Source string `json:"source"`
			} `json:"files"`
		} `json:"generic"`
		Module struct {
			Files []struct {
				Path   string `json:"path"`
				Source string `json:"source"`
			} `json:"files"`
		} `json:"module"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("invalid commands/command.json: %v", err)
	}
	if d.Version == "" {
		t.Fatal("command.json needs a version")
	}
	if len(d.Generic.Files) == 0 || len(d.Module.Files) == 0 {
		t.Fatal("command.json needs both generic and module files")
	}

	replacer := strings.NewReplacer(
		"__name__", "demo",
		"__module__", "example.com/demo",
		"__version__", d.Version,
		"__modname__", "widget",
		"__ModName__", "Widget",
	)
	targetsByVariant := map[string]map[string]bool{}
	for _, variant := range []struct {
		name  string
		files []struct {
			Path   string `json:"path"`
			Source string `json:"source"`
		}
	}{{"generic", d.Generic.Files}, {"module", d.Module.Files}} {
		targets := map[string]bool{}
		targetsByVariant[variant.name] = targets
		for _, file := range variant.files {
			if !strings.Contains(file.Path, "__modname__") {
				t.Errorf("%s file path %q does not carry __modname__", variant.name, file.Path)
			}
			targets[file.Path] = true
			body, err := fs.ReadFile(fsys, path.Join("commands", file.Source))
			if err != nil {
				t.Errorf("%s payload %q missing: %v", variant.name, file.Source, err)
				continue
			}
			if !strings.HasSuffix(file.Source, ".go.tmpl") {
				continue
			}
			rendered := []byte(replacer.Replace(string(body)))
			if _, err := format.Source(rendered); err != nil {
				t.Errorf("%s payload %s does not render to valid Go: %v\n%s", variant.name, file.Source, err, rendered)
			}
		}
	}
	// The two variants must write the same target set, so a name is a single
	// command group regardless of how it was created.
	for target := range targetsByVariant["generic"] {
		if !targetsByVariant["module"][target] {
			t.Errorf("module variant does not write %q", target)
		}
	}
	for target := range targetsByVariant["module"] {
		if !targetsByVariant["generic"][target] {
			t.Errorf("generic variant does not write %q", target)
		}
	}
}
