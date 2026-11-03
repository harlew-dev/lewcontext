// Package web serves the built React app (apps/web) from the API process:
// prerendered pages, the single-page-app fallback for every other page, and
// the hashed assets, with caching and security headers. The admin dashboard
// is a build of its own, served only behind ADMIN_ROUTE_KEY.
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

// admin holds the admin build (apps/web/build/admin/client), copied in by
// `make build` beside dist and never part of it: only ServeAdmin and
// ServeAdminFile read it. Only .gitkeep is committed, like dist.
//
//go:embed all:admin
var admin embed.FS

// Files is the embedded web build.
func Files() fs.FS {
	return sub(dist, "dist")
}

// AdminFiles is the embedded admin build.
func AdminFiles() fs.FS {
	return sub(admin, "admin")
}

func sub(files embed.FS, dir string) fs.FS {
	tree, err := fs.Sub(files, dir)
	if err != nil {
		panic(err) // dir is a fixed, valid path
	}
	return tree
}
