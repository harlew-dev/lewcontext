// Package factory builds fake records for every table, for seeders and tests.
//
// Each factory has its own Bun model mapped to its table, separate from the
// store's models: a migration has to be matched here too, and
// TestEveryTableHasAFactory fails until it is. The API server never imports
// this package.
package factory

import (
	"context"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"strconv"
)

// batchSize bounds the rows in one INSERT statement.
const batchSize = 500

// models lists each factory's model type, for the table coverage test.
var models []reflect.Type

// Factory builds T values from fake data. States adjust each value in the
// order they were added; a factory is immutable, so states return a copy.
type Factory[T any] struct {
	define func(f *gofakeit.Faker) T
	states []func(i int, v *T)
}

// Define declares the factory for a model and registers its table.
func Define[T any](define func(f *gofakeit.Faker) T) Factory[T] {
	models = append(models, reflect.TypeFor[T]())
	return Factory[T]{define: define}
}

// State returns a factory that also applies fn to every value.
func (f Factory[T]) State(fn func(v *T)) Factory[T] {
	return f.Sequence(func(_ int, v *T) { fn(v) })
}

// Sequence returns a factory that also applies fn with each value's index,
// for values that differ by position (names, timestamps).
func (f Factory[T]) Sequence(fn func(i int, v *T)) Factory[T] {
	return Factory[T]{define: f.define, states: append(slices.Clone(f.states), fn)}
}

// Make builds n values without saving them.
func (f Factory[T]) Make(faker *gofakeit.Faker, n int) []T {
	values := make([]T, n)
	for i := range values {
		values[i] = f.define(faker)
		for _, state := range f.states {
			state(i, &values[i])
		}
	}
	return values
}

// Create builds n values and inserts them in batches, filling IDs and
// defaults from RETURNING. Pass a bun.Tx to keep a seeder atomic.
func (f Factory[T]) Create(ctx context.Context, db bun.IDB, faker *gofakeit.Faker, n int) ([]T, error) {
	values := f.Make(faker, n)
	for batch := range slices.Chunk(values, batchSize) {
		if _, err := db.NewInsert().Model(&batch).Returning("*").Exec(ctx); err != nil {
			return nil, fmt.Errorf("insert %s: %w", reflect.TypeFor[T]().Name(), err)
		}
	}
	return values, nil
}

// CreateOne builds and inserts a single value.
func (f Factory[T]) CreateOne(ctx context.Context, db bun.IDB, faker *gofakeit.Faker) (T, error) {
	values, err := f.Create(ctx, db, faker, 1)
	if err != nil {
		var zero T
		return zero, err
	}
	return values[0], nil
}

// Models returns every factory's model type.
func Models() []reflect.Type {
	return slices.Clone(models)
}

// NewFaker returns a faker with a random seed, or FAKER_SEED when set to
// replay a run, together with the seed it used.
func NewFaker() (*gofakeit.Faker, uint64, error) {
	seed := rand.Uint64N(1<<63) + 1 // gofakeit treats 0 as "unseeded".
	if raw := os.Getenv("FAKER_SEED"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || parsed == 0 {
			return nil, 0, fmt.Errorf("FAKER_SEED must be a positive integer")
		}
		seed = parsed
	}
	return gofakeit.New(seed), seed, nil
}

// TB is the part of testing.TB that TestFaker uses.
type TB interface {
	Helper()
	Cleanup(func())
	Failed() bool
	Logf(format string, args ...any)
	Fatal(args ...any)
}

// TestFaker returns a random faker for a test and logs its seed if the test
// fails, so the run can be replayed with FAKER_SEED.
func TestFaker(t TB) *gofakeit.Faker {
	t.Helper()
	faker, seed, err := NewFaker()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("fake data seed %d; replay with FAKER_SEED=%d", seed, seed)
		}
	})
	return faker
}
