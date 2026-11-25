// Package web embeds the compiled frontend static assets for single-binary distribution.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFiles embed.FS

// DistFS returns the embedded frontend build directory rooted at dist.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFiles, "dist")
}
