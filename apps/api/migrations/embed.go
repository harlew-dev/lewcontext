package migrations

import (
	"embed"
	"github.com/uptrace/bun/migrate"
)

// Files contains versioned SQL. While in development the migrations are
// squashed: change a table by editing its create migration, then rebuild the
// local database with `make migrate-fresh CONFIRM=1`.
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
