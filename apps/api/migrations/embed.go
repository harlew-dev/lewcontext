package migrations

import "embed"

// Files contains versioned SQL; applied migrations are immutable.
//
//go:embed *.sql
var Files embed.FS
