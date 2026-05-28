// Package frontend embeds the compiled React application.
// The web/dist directory is built by `make build-web` before compilation.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed dist
var files embed.FS

// FS returns a filesystem rooted at the dist directory.
func FS() (fs.FS, error) {
	return fs.Sub(files, "dist")
}
