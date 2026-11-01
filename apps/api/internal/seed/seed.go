// Package seed fills a development database with demo and fake data. It only
// inserts, never deletes, and is not imported by the API server.
package seed

import (
	"context"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"slices"
)

// MaxCount bounds a seeder's count so a typo can't fill the disk.
const MaxCount = 100_000

// Options configures a run. Zero values use each seeder's default.
type Options struct {
	Count    int    // records to create: projects for volume, users for users
	Projects int    // most projects per user for users
	Email    string // the account volume adds to
	// PasswordHash signs in the accounts demo and users create.
	PasswordHash string
}

// Seeder creates one kind of data in a transaction and describes what it did.
type Seeder struct {
	Name        string
	Description string
	// NeedsPassword seeders create accounts, so the run needs DEMO_PASSWORD.
	NeedsPassword bool
	// Max bounds its count, when it isn't MaxCount.
	Max int
	run func(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error)
}

// Seeders lists every seeder; the first is the default.
var Seeders = []Seeder{
	{Name: "demo", Description: "the test@example.com demo account, its Acme workspace and projects (skipped if it exists)", NeedsPassword: true, run: seedDemo},
	{Name: "volume", Description: "fake projects in one account's newest workspace (-count, default 120; -email, default the demo account)", run: seedVolume},
	{Name: "users", Description: "fake accounts, each with a workspace and projects (-count users, default 10; -projects most each, default 60)", NeedsPassword: true, run: seedUsers},
	{Name: "events", Description: "fake audit trail events, most in the last 7 days, to check the admin Events page at volume (-count, default 10,000, at most 2,000,000)", Max: maxEvents, run: seedEvents},
}

// MaxFor is the most a run of s may create.
func (s Seeder) MaxFor() int {
	if s.Max != 0 {
		return s.Max
	}
	return MaxCount
}

// Find returns the seeder with a name.
func Find(name string) (Seeder, bool) {
	i := slices.IndexFunc(Seeders, func(s Seeder) bool { return s.Name == name })
	if i < 0 {
		return Seeder{}, false
	}
	return Seeders[i], true
}

// Run executes a seeder in one transaction, so a failure leaves no partial data.
func (s Seeder) Run(ctx context.Context, db *bun.DB, f *gofakeit.Faker, o Options) (string, error) {
	var summary string
	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		summary, err = s.run(ctx, tx, f, o)
		return err
	})
	return summary, err
}

// orDefault returns value, or fallback when value is zero.
func orDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
