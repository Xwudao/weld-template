// Package weldtemplate ships the file payloads consumed by the weld CLI.
//
// Payloads are embedded so a weld binary carries its scaffold and capabilities
// with it: no network access or template checkout is needed at run time. Each
// capability lives in capabilities/<name>/ with a capability.json descriptor
// and a files/ payload directory.
//
// This module is the only contract between the weld CLI and the template
// payloads. The CLI imports FS() and reads descriptors from it.
package weldtemplate

import (
	"embed"
	"io/fs"
)

// Version is the template payload version recorded in generated projects.
const Version = "0.1.0"

//go:embed all:capabilities
var assets embed.FS

// FS returns the embedded template assets rooted at the module root.
func FS() fs.FS {
	return assets
}
