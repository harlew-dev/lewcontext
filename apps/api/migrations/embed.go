package migrations

import (
	"embed"
	"github.com/uptrace/bun/migrate"
)

// Files contains versioned SQL; applied migrations are immutable.
// Name pairs <version>_<name>.tx.up.sql and .tx.down.sql so each runs in a transaction.
//
//go:embed *.sql
var Files embed.FS

// Migrations registers every up/down pair with Bun's migrator.
var Migrations = migrate.NewMigrations()

func init() {
	if err := Migrations.Discover(Files); err != nil {
		panic(err)
	}
}
