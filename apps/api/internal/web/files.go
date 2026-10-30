// Package web serves the built React app (apps/web) from the API process:
// prerendered pages, the single-page-app fallback for every other page, and
// the hashed assets, with caching and security headers.
package web

import (
	"embed"
	"io/fs"
)

// dist holds apps/web/build/client, copied in by `make build` before the Go
// build. Only .gitkeep is committed, so a checkout without a front-end build
// still compiles; New then reports ErrNotBuilt.
//
//go:embed all:dist
var dist embed.FS

// Files is the embedded web build.
func Files() fs.FS {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a fixed, valid path
	}
	return files
}
