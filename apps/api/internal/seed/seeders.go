package seed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"strings"
	"time"
)

// DemoEmail is the documented demo account, and DemoWorkspace its workspace.
const (
	DemoEmail     = "test@example.com"
	DemoWorkspace = "Acme"
)

var demoProjects = []factory.Project{
	{Name: "Website redesign", Description: "A fresh home for everything we’re building.", Status: "In progress", Color: "violet"},
	{Name: "Mobile experience", Description: "Bring a little more delight to the everyday.", Status: "In progress", Color: "blue"},
	{Name: "Brand guidelines", Description: "One shared language. A thousand possibilities.", Status: "Completed", Color: "amber"},
	{Name: "Customer insights", Description: "Turn good conversations into better decisions.", Status: "In progress", Color: "rose"},
	{Name: "Design system", Description: "Thoughtful foundations for what comes next.", Status: "Planned", Color: "emerald"},
	{Name: "Product launch", Description: "Make our next chapter a memorable one.", Status: "Planned", Color: "violet"},
}

// demoMembers are the roles of the fake people the demo seed adds to Acme
// beside its owner, so the members page has someone to show.
var demoMembers = []auth.Role{auth.RoleAdmin, auth.RoleMember, auth.RoleMember, auth.RoleViewer}

// seedDemo creates the demo account and its workspace with fixed names, once,
// with a few fake members who can't sign in.
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
	workspace, err := factory.WorkspaceFor(ctx, tx, f, user.ID, factory.Named(DemoWorkspace))
	if err != nil {
		return "", err
	}
	now := time.Now()
	_, err = factory.ProjectsIn(workspace.ID).Sequence(func(i int, p *factory.Project) {
		demo := demoProjects[i]
		p.Name, p.Description, p.Status, p.Color = demo.Name, demo.Description, demo.Status, demo.Color
		p.CreatedAt = now.AddDate(0, 0, -(14 + i))
		p.UpdatedAt = now.Add(-time.Duration(i*4) * time.Hour)
	}).Create(ctx, tx, f, len(demoProjects))
	if err != nil {
		return "", err
	}
	if err := addDemoMembers(ctx, tx, f, workspace.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created demo account %s with workspace %s, %d projects and %d more members", DemoEmail, DemoWorkspace, len(demoProjects), len(demoMembers)), nil
}

// addDemoMembers adds fake @example.test users to the workspace, joined over
// the past weeks. Their password hash is random text, not a hash, so no
// password signs them in.
func addDemoMembers(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, workspaceID int64) error {
	users, err := factory.Users.State(func(u *factory.User) {
		u.PasswordHash = "unusable:" + f.LetterN(32)
	}).Create(ctx, tx, f, len(demoMembers))
	if err != nil {
		return err
	}
	now := time.Now()
	for i, user := range users {
		_, err := factory.WorkspaceUsers.State(func(m *factory.WorkspaceUser) {
			m.UserID, m.WorkspaceID, m.Role = user.ID, workspaceID, demoMembers[i]
			m.CreatedAt = now.AddDate(0, 0, -(10 - 2*i))
		}).CreateOne(ctx, tx, f)
		if err != nil {
			return err
		}
	}
	return nil
}

// seedVolume adds fake projects to an existing account's newest workspace.
func seedVolume(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error) {
	email := strings.ToLower(strings.TrimSpace(o.Email))
	if email == "" {
		email = DemoEmail
	}
	exists, err := tx.NewSelect().Model((*factory.User)(nil)).Where("email = ?", email).Exists(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("no account uses %s; create it first (make seed, or sign up)", email)
	}
	var workspace factory.Workspace
	err = tx.NewSelect().Model(&workspace).
		Join("JOIN workspace_user AS m ON m.workspace_id = w.id").
		Join("JOIN users AS u ON u.id = m.user_id").
		Where("u.email = ?", email).
		OrderExpr("w.created_at DESC, w.id DESC").
		Limit(1).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%s has no workspace; create one first (sign in and finish onboarding)", email)
	}
	if err != nil {
		return "", err
	}
	count := orDefault(o.Count, 120)
	if _, err := factory.ProjectsIn(workspace.ID).Create(ctx, tx, f, count); err != nil {
		return "", err
	}
	return fmt.Sprintf("Added %d projects to %s’s workspace %s", count, email, workspace.Name), nil
}

// seedUsers creates fake accounts, each owning a workspace with a random
// number of projects. Every third one never verified its email, as if made
// while EMAIL_VERIFICATION was off, so the Not verified badge and
// EMAIL_VERIFICATION_EXISTING=required can be tried.
func seedUsers(ctx context.Context, tx bun.Tx, f *gofakeit.Faker, o Options) (string, error) {
	count, most := orDefault(o.Count, 10), orDefault(o.Projects, 60)
	everyThirdUnverified := func(i int, u *factory.User) {
		if i%3 == 2 {
			factory.Unverified(u)
		}
	}
	users, err := factory.Users.State(factory.WithPasswordHash(o.PasswordHash)).Sequence(everyThirdUnverified).Create(ctx, tx, f, count)
	if err != nil {
		return "", err
	}
	projects := 0
	for _, user := range users {
		workspace, err := factory.WorkspaceFor(ctx, tx, f, user.ID)
		if err != nil {
			return "", err
		}
		n := f.IntRange(0, most)
		if _, err := factory.ProjectsIn(workspace.ID).Create(ctx, tx, f, n); err != nil {
			return "", err
		}
		projects += n
	}
	return fmt.Sprintf("Created %d users (%s, …, %d of them unverified), each with a workspace, and %d projects", count, users[0].Email, count/3, projects), nil
}
