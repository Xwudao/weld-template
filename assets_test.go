package weldtemplate

import (
	"encoding/json"
	"io/fs"
	"path"
	"testing"
)

type descriptor struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	Files   []struct {
		Path   string `json:"path"`
		Source string `json:"source"`
	} `json:"files"`
	Patches []struct {
		Path   string `json:"path"`
		Marker string `json:"marker"`
		Source string `json:"source"`
	} `json:"patches"`
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
		raw, err := fs.ReadFile(fsys, path.Join(dir, "capability.json"))
		if err != nil {
			t.Fatalf("%s: read descriptor: %v", name, err)
		}
		var d descriptor
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("%s: invalid descriptor: %v", name, err)
		}
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

func TestWebCapabilityDeclaresBaseRequirement(t *testing.T) {
	raw, err := fs.ReadFile(FS(), "capabilities/web/capability.json")
	if err != nil {
		t.Fatalf("read web capability: %v", err)
	}
	var d struct {
		Requires []string `json:"requires"`
		Patches  []struct {
			Path   string `json:"path"`
			Marker string `json:"marker"`
		} `json:"patches"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("invalid descriptor: %v", err)
	}
	if len(d.Requires) != 1 || d.Requires[0] != "base" {
		t.Fatalf("web requires = %v, want [base]", d.Requires)
	}
	if len(d.Patches) != 2 {
		t.Fatalf("web patches = %d, want 2", len(d.Patches))
	}
}
