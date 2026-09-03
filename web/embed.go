// Package web embeds the built frontend (web/dist, produced by `npm run build`
// in the web/ directory) into the Go binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend build rooted at dist/, ready to be
// served directly (no "dist/" prefix in paths).
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
