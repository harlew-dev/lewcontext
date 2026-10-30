package database

import (
	"context"
	"fmt"
	"forma/api/migrations"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

const migrationLockID = 104209

// legacyMigrations maps files recorded by the previous runner to the Bun migrations that now
// hold their SQL. 001_initial was split into one migration per table without changing the SQL.
var legacyMigrations = map[string][]string{
	"001_initial.up.sql": {"001", "002", "003", "004"},
}

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

// CreateMigration writes an empty transactional up/down pair to the migrations directory.
func CreateMigration(ctx context.Context, name string) ([]*migrate.MigrationFile, error) {
	return migrate.NewMigrator(nil, migrations.Migrations).CreateTxSQLMigrations(ctx, name)
}

// withMigrator serializes migration commands across processes with a PostgreSQL advisory lock,
// which the server releases even if this process dies.
func withMigrator(ctx context.Context, db *bun.DB, run func(*migrate.Migrator) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "SELECT pg_advisory_lock(?)", migrationLockID); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(?)", migrationLockID)
	m := migrate.NewMigrator(db, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
	if err = m.Init(ctx); err != nil {
		return err
	}
	if err = adoptLegacyHistory(ctx, db); err != nil {
		return err
	}
	return run(m)
}

// adoptLegacyHistory hands over databases migrated by the previous runner, which recorded
// applied filenames in schema_migrations. It marks the matching Bun migrations applied as
// group 1 and renames the old table in the same transaction, so it runs once.
func adoptLegacyHistory(ctx context.Context, db *bun.DB) error {
	var legacy bool
	if err := db.NewRaw("SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(ctx, &legacy); err != nil || !legacy {
		return err
	}
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var versions []string
		if err := tx.NewSelect().Table("schema_migrations").Column("version").Scan(ctx, &versions); err != nil {
			return err
		}
		for _, version := range versions {
			names, ok := legacyMigrations[version]
			if !ok {
				return fmt.Errorf("migrate: unknown legacy migration %s; map it in legacyMigrations", version)
			}
			for _, name := range names {
				applied := migrate.Migration{Name: name, GroupID: 1}
				if _, err := tx.NewInsert().Model(&applied).ModelTableExpr("bun_migrations").Exec(ctx); err != nil {
					return err
				}
			}
		}
		_, err := tx.ExecContext(ctx, "ALTER TABLE schema_migrations RENAME TO schema_migrations_legacy")
		return err
	})
}
