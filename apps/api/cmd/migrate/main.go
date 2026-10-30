package main

import (
	"context"
	"fmt"
	"forma/api/internal/config"
	"forma/api/internal/database"
	"github.com/uptrace/bun"
	"log"
	"os"
	"time"
)

const usage = `usage: migrate [command]

  up                  apply pending migrations (default)
  status              list migrations and the group that applied each
  rollback [-confirm] undo the last group; without -confirm only shows what would run
  fresh [-confirm]    drop every table in the schema and migrate from scratch (development
                      only); without -confirm only lists what would be dropped
  create <name>       write an empty <timestamp>_<name>.tx.up.sql / .tx.down.sql pair`

func main() {
	command, args := "up", os.Args[1:]
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if command == "create" {
		if len(args) != 1 {
			log.Fatal(usage)
		}
		create(ctx, args[0])
		return
	}
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	switch {
	case command == "up" && len(args) == 0:
		up(ctx, db)
	case command == "status" && len(args) == 0:
		status(ctx, db)
	case command == "rollback" && len(args) == 0:
		previewRollback(ctx, db)
	case command == "rollback" && len(args) == 1 && args[0] == "-confirm":
		rollback(ctx, db)
	case command == "fresh" && len(args) == 0:
		previewFresh(ctx, db, c.Production)
	case command == "fresh" && len(args) == 1 && args[0] == "-confirm":
		fresh(ctx, db, c.Production)
	default:
		log.Fatal(usage)
	}
}
func up(ctx context.Context, db *bun.DB) {
	group, err := database.Migrate(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	if group.IsZero() {
		log.Print("No pending migrations")
		return
	}
	log.Printf("Applied %s", group)
}
func status(ctx context.Context, db *bun.DB) {
	migrations, err := database.Status(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range migrations {
		state := "pending"
		if m.IsApplied() {
			state = fmt.Sprintf("applied in group %d at %s", m.GroupID, m.MigratedAt.Format(time.RFC3339))
		}
		fmt.Printf("%-40s %s\n", m.String(), state)
	}
}
func previewRollback(ctx context.Context, db *bun.DB) {
	migrations, err := database.Status(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	group := migrations.LastGroup()
	if group.IsZero() {
		log.Print("Nothing to roll back")
		return
	}
	log.Printf("Would roll back %s. Down migrations can drop data; back up first, then re-run with -confirm.", group)
}
func rollback(ctx context.Context, db *bun.DB) {
	group, err := database.Rollback(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	if group.IsZero() {
		log.Print("Nothing to roll back")
		return
	}
	log.Printf("Rolled back %s", group)
}
func previewFresh(ctx context.Context, db *bun.DB, production bool) {
	if production {
		log.Fatal(database.ErrFreshInProduction)
	}
	tables, err := database.FreshPreview(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	if len(tables) == 0 {
		log.Print("The schema is empty; re-run with -confirm to migrate it")
		return
	}
	for _, t := range tables {
		fmt.Printf("%-40s %d rows\n", t.Name, t.Rows)
	}
	log.Printf("Would drop these %d tables and all their rows, then migrate from scratch. Re-run with -confirm.", len(tables))
}
func fresh(ctx context.Context, db *bun.DB, production bool) {
	group, err := database.Fresh(ctx, db, production)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Dropped the schema and applied %s", group)
}
func create(ctx context.Context, name string) {
	files, err := database.CreateMigration(ctx, name)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		log.Printf("Created %s", f.Path)
	}
}
