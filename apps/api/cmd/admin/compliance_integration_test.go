package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
)

func testPseudonym(t *testing.T) func(string) string {
	t.Helper()
	key := make([]byte, auth.AppKeySize)
	must(rand.Read(key))(t)
	return must(auth.NewKeys(key))(t).EmailPseudonym
}

// eventNames counts the events named name.
func eventCount(t *testing.T, repository *store.Store, name string) int {
	t.Helper()
	return int(must(repository.DB.NewSelect().Table("events").Where("name = ?", name).Count(context.Background()))(t))
}

// erase previews without -confirm; refuses an admin, and the only owner of
// a workspace with other members (naming it); otherwise deletes the
// account with the workspaces only it belonged to, erases its email from
// other events, and records auth.account_deleted and admin.user.erased,
// with no actor.
func TestEraseAccount(t *testing.T) {
	repository := migrated(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	pseudonym := testPseudonym(t)
	users := must(factory.Users.Create(ctx, repository.DB, f, 3))(t)
	person, member, admin := users[0], users[1], users[2]
	solo := must(factory.WorkspaceFor(ctx, repository.DB, f, person.ID))(t)
	must(factory.ProjectsIn(solo.ID).Create(ctx, repository.DB, f, 2))(t)
	shared := must(factory.WorkspaceFor(ctx, repository.DB, f, person.ID, factory.Named("Shared")))(t)
	must(factory.MemberOf(ctx, repository.DB, f, member.ID, shared.ID, auth.RoleMember))(t)
	must(factory.Events.State(func(e *factory.Event) {
		e.Name, e.Payload = "auth.login_attempted", map[string]any{"email": person.Email, "result": "wrong_password"}
	}).CreateOne(ctx, repository.DB, f))(t)
	must(factory.Admins.State(factory.AdminFor(admin.ID)).CreateOne(ctx, repository.DB, f))(t)
	var out bytes.Buffer

	if err := eraseAccount(ctx, repository, pseudonym, person.Email, false, &out); err != nil || !strings.Contains(out.String(), "Would erase") || eventCount(t, repository, "auth.account_deleted") != 0 {
		t.Fatalf("preview: %v %s", err, out.String())
	}
	if err := eraseAccount(ctx, repository, pseudonym, admin.Email, true, &out); err == nil || !strings.Contains(err.Error(), "revoke admin first") {
		t.Fatalf("an admin: %v", err)
	}
	err := eraseAccount(ctx, repository, pseudonym, person.Email, true, &out)
	if err == nil || !strings.Contains(err.Error(), "Shared ("+shared.PublicID+")") || strings.Contains(err.Error(), solo.PublicID) {
		t.Fatalf("the only owner of a workspace with members: %v", err)
	}
	if n := int(must(repository.DB.NewSelect().Table("workspaces").Count(ctx))(t)); n != 2 || eventCount(t, repository, "workspace.deleted") != 0 {
		t.Fatal("a refused erasure deleted a workspace")
	}
	must(repository.DB.NewUpdate().Table("workspace_user").Set("role = 'owner'").Where("user_id = ?", member.ID).Exec(ctx))(t)

	out.Reset()
	if err := eraseAccount(ctx, repository, pseudonym, strings.ToUpper(person.Email), true, &out); err != nil || !strings.Contains(out.String(), "with 1 workspaces only it belonged to") {
		t.Fatalf("erase: %v %s", err, out.String())
	}
	if n := int(must(repository.DB.NewSelect().Table("users").Where("id = ?", person.ID).Count(ctx))(t)); n != 0 {
		t.Fatal("the account is still there")
	}
	if n := int(must(repository.DB.NewSelect().Table("workspaces").Count(ctx))(t)); n != 1 || int(must(repository.DB.NewSelect().Table("projects").Count(ctx))(t)) != 0 {
		t.Fatal("the solo workspace (and its projects) or the shared one")
	}
	var erased, deleted string
	must(0, repository.DB.NewSelect().Table("events").ColumnExpr("payload::text").Where("name = 'admin.user.erased' AND actor_id IS NULL AND user_id IS NULL").Scan(ctx, &erased))(t)
	must(0, repository.DB.NewSelect().Table("events").ColumnExpr("payload::text").Where("name = 'auth.account_deleted' AND actor_id IS NULL").Scan(ctx, &deleted))(t)
	if erased != `{"user": "`+person.PublicID+`", "workspaces": 1}` || deleted != `{"user": "`+person.PublicID+`"}` || eventCount(t, repository, "workspace.deleted") != 1 {
		t.Fatalf("events: %s %s", erased, deleted)
	}
	if n := int(must(repository.DB.NewSelect().Table("events").Where("lower(payload::text) LIKE ?", "%"+strings.ToLower(person.Email)+"%").Count(ctx))(t)); n != 0 {
		t.Fatalf("%d events still hold the address", n)
	}
	if err := eraseAccount(ctx, repository, pseudonym, person.Email, true, &out); err == nil || !strings.Contains(err.Error(), "no account uses") {
		t.Fatalf("again: %v", err)
	}
}

// export writes the same document as Download my data, and records
// admin.user.exported (no actor, the account as user) before it does.
func TestExportAccount(t *testing.T) {
	repository := migrated(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := must(factory.Users.CreateOne(ctx, repository.DB, f))(t)
	must(factory.WorkspaceFor(ctx, repository.DB, f, user.ID, factory.Named("Mine")))(t)
	var out bytes.Buffer
	if err := exportAccount(ctx, repository, "nobody@example.test", &out); err == nil {
		t.Fatal("exported an unknown address")
	}
	if err := exportAccount(ctx, repository, user.Email, &out); err != nil {
		t.Fatal(err)
	}
	var export store.AccountExport
	must(0, json.Unmarshal(out.Bytes(), &export))(t)
	if export.Account.Email != user.Email || len(export.OwnedWorkspaces) != 1 || export.OwnedWorkspaces[0].Name != "Mine" || len(export.Memberships) != 1 {
		t.Fatalf("export: %s", out.String())
	}
	if strings.Contains(out.String(), "password") || strings.Contains(out.String(), user.PasswordHash) {
		t.Fatal("the export holds the password hash")
	}
	var payload string
	must(0, repository.DB.NewSelect().Table("events").ColumnExpr("payload::text").Where("name = 'admin.user.exported' AND actor_id IS NULL AND user_id = ?", user.ID).Scan(ctx, &payload))(t)
	if payload != `{"user": "`+user.PublicID+`"}` {
		t.Fatalf("admin.user.exported: %s", payload)
	}
}
