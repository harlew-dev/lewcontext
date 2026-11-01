package main

import (
	"context"
	"flag"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/seed"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	useLogger()
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if c.Production {
		log.Fatal("seeding is disabled in production")
	}
	only := flag.String("only", seed.Seeders[0].Name, "seeder to run: "+seederNames())
	count := flag.Int("count", 0, "records to create (0 uses the seeder's default)")
	projects := flag.Int("projects", 0, "most projects per fake user, for -only users (0 uses the default)")
	email := flag.String("email", "", "account to add projects to, for -only volume (default the demo account)")
	flag.Usage = usage
	flag.Parse()
	seeder, ok := seed.Find(*only)
	if !ok {
		log.Fatalf("unknown seeder %q; choose %s", *only, seederNames())
	}
	for _, n := range []int{*count, *projects} {
		if n < 0 || n > seed.MaxCount {
			log.Fatalf("counts must be between 0 and %d", seed.MaxCount)
		}
	}
	options := seed.Options{Count: *count, Projects: *projects, Email: *email}
	if seeder.NeedsPassword {
		if options.PasswordHash, err = passwordHash(); err != nil {
			log.Fatal(err)
		}
	}
	faker, fakerSeed, err := factory.NewFaker()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	summary, err := seeder.Run(ctx, db, faker, options)
	if err != nil {
		log.Fatalf("seed %s: %v", seeder.Name, err)
	}
	log.Printf("%s (fake data seed %d)", summary, fakerSeed)
}

// passwordHash hashes DEMO_PASSWORD once for every account a seeder creates.
// Sign-up's 12-character minimum doesn't apply: the seed never runs in
// production, so a short development password such as "password" is allowed.
func passwordHash() (string, error) {
	password := os.Getenv("DEMO_PASSWORD")
	if password == "" || len(password) > 128 {
		return "", fmt.Errorf("set DEMO_PASSWORD to 1–128 bytes")
	}
	return auth.HashPassword(password)
}
func seederNames() string {
	names := make([]string, len(seed.Seeders))
	for i, s := range seed.Seeders {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}
func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintln(out, "Usage: go run ./cmd/seed [-only seeder] [-count n] [-projects n] [-email address]")
	fmt.Fprintln(out, "\nSeeders:")
	for _, s := range seed.Seeders {
		fmt.Fprintf(out, "  %-7s %s\n", s.Name, s.Description)
	}
	fmt.Fprintln(out, "\nFlags:")
	flag.PrintDefaults()
}

// useLogger installs the server's log handler (LOG_FORMAT, LOG_LEVEL) for
// anything logged through slog, keeping this command's own output plain
// (observe.UseForCommand).
func useLogger() {
	logging, err := config.LoadLogging()
	if err != nil {
		log.Fatal(err)
	}
	observe.UseForCommand(logging.LogFormat, logging.LogLevel)
}
