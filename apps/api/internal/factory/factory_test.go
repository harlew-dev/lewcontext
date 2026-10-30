package factory

import (
	"context"
	"forma/api/internal/database"
	"forma/api/internal/database/dbtest"
	"github.com/brianvoe/gofakeit/v7"
	"net/mail"
	"slices"
	"strings"
	"testing"
)

func TestStatesApplyInOrder(t *testing.T) {
	type item struct{ log []string }
	// A literal rather than Define, so the test type isn't registered as a table.
	f := Factory[item]{define: func(*gofakeit.Faker) item { return item{log: []string{"define"}} }}.
		State(func(v *item) { v.log = append(v.log, "first") }).
		Sequence(func(i int, v *item) { v.log = append(v.log, string(rune('a'+i))) })
	got := f.Make(gofakeit.New(1), 2)
	if !slices.Equal(got[0].log, []string{"define", "first", "a"}) || !slices.Equal(got[1].log, []string{"define", "first", "b"}) {
		t.Fatalf("states out of order: %v", got)
	}
	// Adding a state returns a new factory and leaves the original alone.
	_ = f.State(func(v *item) { v.log = nil })
	if len(f.Make(gofakeit.New(1), 1)[0].log) != 3 {
		t.Fatal("State modified the original factory")
	}
}
func TestSameSeedReplaysValues(t *testing.T) {
	a := Projects.Make(gofakeit.New(42), 5)
	b := Projects.Make(gofakeit.New(42), 5)
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Status != b[i].Status || a[i].Description != b[i].Description {
			t.Fatalf("seed 42 differs at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
	t.Setenv("FAKER_SEED", "42")
	replayed, seed, err := NewFaker()
	if err != nil || seed != 42 || Projects.Make(replayed, 1)[0].Name != a[0].Name {
		t.Fatalf("FAKER_SEED not replayed: seed %d err %v", seed, err)
	}
	t.Setenv("FAKER_SEED", "0")
	if _, _, err := NewFaker(); err == nil {
		t.Fatal("FAKER_SEED=0 accepted")
	}
}
func TestUserEmailsAreUniqueAndLowercase(t *testing.T) {
	users := Users.Make(TestFaker(t), 2000)
	seen := map[string]bool{}
	for _, u := range users {
		address, err := mail.ParseAddress(u.Email)
		if err != nil || address.Address != u.Email || u.Email != strings.ToLower(u.Email) {
			t.Fatalf("invalid email %q", u.Email)
		}
		if seen[u.Email] {
			t.Fatalf("duplicate email %q", u.Email)
		}
		seen[u.Email] = true
	}
}

// Every table in the migrated schema needs a factory, and every factory's
// output has to insert: a migration can't land without updating fake data.
func TestEveryTableHasAFactory(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var tables []string
	err := db.NewSelect().TableExpr("information_schema.tables").Column("table_name").
		Where("table_schema = current_schema()").
		Where("table_name NOT LIKE 'bun_migration%'").
		Scan(ctx, &tables)
	if err != nil {
		t.Fatal(err)
	}
	var covered []string
	for _, model := range Models() {
		covered = append(covered, db.Table(model).Name)
	}
	for _, table := range tables {
		if !slices.Contains(covered, table) {
			t.Errorf("table %q has no factory in internal/factory", table)
		}
	}
	f := TestFaker(t)
	users, err := Users.Create(ctx, db, f, 3)
	if err != nil {
		t.Fatal(err)
	}
	user, other, third := users[0], users[1], users[2]
	if _, err := SessionFor(ctx, db, f, user.ID); err != nil {
		t.Fatal(err)
	}
	workspace, err := WorkspaceFor(ctx, db, f, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WorkspaceUsers.State(func(m *WorkspaceUser) { m.WorkspaceID = workspace.ID }).Sequence(func(i int, m *WorkspaceUser) {
		m.UserID = []int64{other.ID, third.ID}[i]
	}).Create(ctx, db, f, 2); err != nil {
		t.Fatal(err)
	}
	projects, err := ProjectsIn(workspace.ID).Create(ctx, db, f, 3)
	if err != nil {
		t.Fatal(err)
	}
	if user.PublicID == "" || workspace.PublicID == "" || projects[0].PublicID == "" {
		t.Fatal("RETURNING did not fill public IDs")
	}
	if _, err := AuthAttempts.Create(ctx, db, f, 3); err != nil {
		t.Fatal(err)
	}
	event, err := Events.State(func(e *Event) { e.UserID = user.ID }).CreateOne(ctx, db, f)
	if err != nil || event.PublicID == "" {
		t.Fatalf("event: %+v (%v)", event, err)
	}
}
func TestCreateInsertsInBatches(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := TestFaker(t)
	user, err := Users.CreateOne(ctx, db, f)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := WorkspaceFor(ctx, db, f, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := ProjectsIn(workspace.ID).Create(ctx, db, f, batchSize+7)
	if err != nil {
		t.Fatal(err)
	}
	if projects[0].ID == 0 || projects[len(projects)-1].PublicID == "" {
		t.Fatal("RETURNING did not fill IDs across batches")
	}
	count, err := db.NewSelect().Model((*Project)(nil)).Where("workspace_id = ?", workspace.ID).Count(ctx)
	if err != nil || count != batchSize+7 {
		t.Fatalf("want %d projects, got %d (%v)", batchSize+7, count, err)
	}
}
