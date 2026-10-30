package seed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"forma/api/internal/factory"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"strings"
	"time"
)

// DemoEmail is the documented demo account.
const DemoEmail = "hello@forma.test"

var demoProjects = []factory.Project{
	{Name: "Website redesign", Description: "A fresh home for everything we’re building.", Status: "In progress", Color: "violet"},
	{Name: "Mobile experience", Description: "Bring a little more delight to the everyday.", Status: "In progress", Color: "blue"},
	{Name: "Brand guidelines", Description: "One shared language. A thousand possibilities.", Status: "Completed", Color: "amber"},
	{Name: "Customer insights", Description: "Turn good conversations into better decisions.", Status: "In progress", Color: "rose"},
	{Name: "Design system", Description: "Thoughtful foundations for what comes next.", Status: "Planned", Color: "emerald"},
	{Name: "Product launch", Description: "Make our next chapter a memorable one.", Status: "Planned", Color: "violet"},
}

// seedDemo creates the demo account with fixed names, once.
func seedDemo(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error) {
	exists, err := tx.NewSelect().Model((*factory.User)(nil)).Where("email = ?", DemoEmail).Exists(ctx)
	if err != nil || exists {
		return "Demo account already exists; no data changed", err
	}
	user, err := factory.Users.State(func(u *factory.User) {
		u.Name, u.Email, u.PasswordHash = "Alex Morgan", DemoEmail, o.PasswordHash
	}).CreateOne(ctx, tx, f)
	if err != nil {
		return "", err
	}
	now := time.Now()
	_, err = factory.ProjectsFor(user.ID).Sequence(func(i int, p *factory.Project) {
		demo := demoProjects[i]
		p.Name, p.Description, p.Status, p.Color = demo.Name, demo.Description, demo.Status, demo.Color
		p.CreatedAt = now.AddDate(0, 0, -(14 + i))
		p.UpdatedAt = now.Add(-time.Duration(i*4) * time.Hour)
	}).Create(ctx, tx, f, len(demoProjects))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created demo account %s and %d projects", DemoEmail, len(demoProjects)), nil
}

// seedVolume adds fake projects to an existing account.
func seedVolume(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error) {
	email := strings.ToLower(strings.TrimSpace(o.Email))
	if email == "" {
		email = DemoEmail
	}
	var userID int64
	err := tx.NewSelect().Model((*factory.User)(nil)).Column("id").Where("email = ?", email).Scan(ctx, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("no account uses %s; create it first (make seed, or sign up)", email)
	}
	if err != nil {
		return "", err
	}
	count := orDefault(o.Count, 120)
	if _, err := factory.ProjectsFor(userID).Create(ctx, tx, f, count); err != nil {
		return "", err
	}
	return fmt.Sprintf("Added %d projects to %s", count, email), nil
}

// seedUsers creates fake accounts, each with a random number of projects.
func seedUsers(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error) {
	count, most := orDefault(o.Count, 10), orDefault(o.Projects, 60)
	users, err := factory.Users.State(factory.WithPasswordHash(o.PasswordHash)).Create(ctx, tx, f, count)
	if err != nil {
		return "", err
	}
	projects := 0
	for _, user := range users {
		n := f.IntRange(0, most)
		if _, err := factory.ProjectsFor(user.ID).Create(ctx, tx, f, n); err != nil {
			return "", err
		}
		projects += n
	}
	return fmt.Sprintf("Created %d users (%s, …) with %d projects", count, users[0].Email, projects), nil
}
