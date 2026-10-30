package database

import (
	"context"
	"errors"
	"forma/api/migrations"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

const migrationLockID = 104209

// ErrFreshInProduction stops Fresh from wiping a production database.
var ErrFreshInProduction = errors.New("migrate: fresh is disabled in production")

// Migrate applies every pending migration as one group. Each migration runs in its own
// transaction and is recorded only after it succeeds.
func Migrate(ctx context.Context, db *bun.DB) (*migrate.MigrationGroup, error) {
	var group *migrate.MigrationGroup
	err := withMigrator(ctx, db, func(m *migrate.Migrator) (err error) {
		group, err = m.Migrate(ctx)
		return err
	})
	return group, err
}

// Rollback runs the down migrations of the most recently applied group. Down SQL is destructive.
func Rollback(ctx context.Context, db *bun.DB) (*migrate.MigrationGroup, error) {
	var group *migrate.MigrationGroup
	err := withMigrator(ctx, db, func(m *migrate.Migrator) (err error) {
		group, err = m.Rollback(ctx)
		return err
	})
	return group, err
}

// Status lists every known migration with its applied group, if any.
func Status(ctx context.Context, db *bun.DB) (migrate.MigrationSlice, error) {
	var status migrate.MigrationSlice
	err := withMigrator(ctx, db, func(m *migrate.Migrator) (err error) {
		status, err = m.MigrationsWithStatus(ctx)
		return err
	})
	return status, err
}

// TableRows is a table Fresh would drop and how many rows it holds.
type TableRows struct {
	Name string
	Rows int64
}

// FreshPreview lists the tables in the current schema, which Fresh would drop.
func FreshPreview(ctx context.Context, db *bun.DB) ([]TableRows, error) {
	var names []string
	err := db.NewSelect().TableExpr("information_schema.tables").Column("table_name").
		Where("table_schema = current_schema()").
		Where("table_type = 'BASE TABLE'").
		Order("table_name").
		Scan(ctx, &names)
	if err != nil {
		return nil, err
	}
	tables := make([]TableRows, len(names))
	for i, name := range names {
		count, err := db.NewSelect().TableExpr("?", bun.Ident(name)).Count(ctx)
		if err != nil {
			return nil, err
		}
		tables[i] = TableRows{Name: name, Rows: int64(count)}
	}
	return tables, nil
}

// Fresh drops the current schema with every table and row in it, recreates it
// empty and applies every migration. It is for the local development database
// while migrations are squashed, and refuses to run in production.
func Fresh(ctx context.Context, db *bun.DB, production bool) (*migrate.MigrationGroup, error) {
	if production {
		return nil, ErrFreshInProduction
	}
	err := withLock(ctx, db, func() error {
		var schema string
		if err := db.NewSelect().ColumnExpr("current_schema()").Scan(ctx, &schema); err != nil {
			return err
		}
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.ExecContext(ctx, "DROP SCHEMA ? CASCADE", bun.Ident(schema)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "CREATE SCHEMA ?", bun.Ident(schema))
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return Migrate(ctx, db)
}

// CreateMigration writes an empty transactional up/down pair to the migrations directory.
func CreateMigration(ctx context.Context, name string) ([]*migrate.MigrationFile, error) {
	return migrate.NewMigrator(nil, migrations.Migrations).CreateTxSQLMigrations(ctx, name)
}

// withMigrator runs a migrator command under the migration lock.
func withMigrator(ctx context.Context, db *bun.DB, run func(*migrate.Migrator) error) error {
	return withLock(ctx, db, func() error {
		m := migrate.NewMigrator(db, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
		if err := m.Init(ctx); err != nil {
			return err
		}
		return run(m)
	})
}

// withLock serializes migration commands across processes with a PostgreSQL advisory lock,
// which the server releases even if this process dies.
func withLock(ctx context.Context, db *bun.DB, run func() error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "SELECT pg_advisory_lock(?)", migrationLockID); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(?)", migrationLockID)
	return run()
}
