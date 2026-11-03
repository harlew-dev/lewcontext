package workspace_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/store"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// rowsWhere counts the rows of a factory model that match where.
func rowsWhere(t *testing.T, m memberFixture, model any, where string, args ...any) int {
	t.Helper()
	n, err := m.db.NewSelect().Model(model).Where(where, args...).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// deleteWorkspaceBody is the body that deletes a workspace named name.
func deleteWorkspaceBody(name, password string) string {
	return fmt.Sprintf(`{"name":%q,"password":%q}`, name, password)
}

// deleteAccountBody is the body that deletes the signed-in account.
func deleteAccountBody(password string) string {
	return fmt.Sprintf(`{"password":%q}`, password)
}
func TestDeleteProject(t *testing.T) {
	m := newMemberFixture(t)
	ctx := context.Background()
	admin, member := m.join(t, auth.RoleAdmin), m.join(t, auth.RoleMember)
	project := apitest.Must(factory.ProjectsIn(m.workspace.ID).CreateOne(ctx, m.db, m.f))(t)
	path := m.path("/projects/" + project.PublicID)

	apitest.Call(t, m.h, "DELETE", path, "", member.CSRFToken, member.Cookie(), 403)
	apitest.Call(t, m.h, "DELETE", path, "", "", admin.Cookie(), 403) // no CSRF token
	apitest.Call(t, m.h, "DELETE", path, "", admin.CSRFToken, admin.Cookie(), 204)
	apitest.Call(t, m.h, "GET", path, "", "", admin.Cookie(), 404)
	apitest.Call(t, m.h, "DELETE", path, "", admin.CSRFToken, admin.Cookie(), 404)
	e := apitest.OnlyEvent(t, m.db, "project.deleted")
	if !apitest.Is(e.ActorID, admin.UserID) || !apitest.Is(e.WorkspaceID, m.workspace.ID) || e.Payload["project"] != project.PublicID ||
		e.Payload["workspace"] != m.workspace.PublicID || e.Payload["name"] != project.Name {
		t.Fatalf("event: %s", e.Raw)
	}

	// Another workspace's project, even one of yours, is not found here.
	elsewhere := apitest.OwnedWorkspace(t, m.db, m.f, m.owner.UserID)
	theirs := apitest.Must(factory.ProjectsIn(elsewhere.ID).CreateOne(ctx, m.db, m.f))(t)
	apitest.Call(t, m.h, "DELETE", m.path("/projects/"+theirs.PublicID), "", m.owner.CSRFToken, m.owner.Cookie(), 404)
	apitest.Call(t, m.h, "DELETE", m.path("/projects/42"), "", m.owner.CSRFToken, m.owner.Cookie(), 404)
	if rowsWhere(t, m, (*factory.Project)(nil), "id = ?", theirs.ID) != 1 {
		t.Fatal("another workspace's project was deleted")
	}
	apitest.CheckPayloadIDs(t, m.db)
}
func TestDeleteWorkspace(t *testing.T) {
	m := newMemberFixture(t)
	ctx := context.Background()
	admin, member := m.join(t, auth.RoleAdmin), m.join(t, auth.RoleMember)
	apitest.Must(factory.ProjectsIn(m.workspace.ID).Create(ctx, m.db, m.f, 3))(t)
	path := m.path("")
	owner := m.owner
	name := m.workspace.Name

	apitest.Call(t, m.h, "DELETE", path, deleteWorkspaceBody(name, factory.DefaultPassword), admin.CSRFToken, admin.Cookie(), 403)
	wrongName := apitest.Call(t, m.h, "DELETE", path, deleteWorkspaceBody("Not "+name, factory.DefaultPassword), owner.CSRFToken, owner.Cookie(), 422)
	wrongPassword := apitest.Call(t, m.h, "DELETE", path, deleteWorkspaceBody(name, "not my password"), owner.CSRFToken, owner.Cookie(), 422)
	if apitest.ErrorField(t, wrongName) != "name" || apitest.ErrorField(t, wrongPassword) != "password" {
		t.Fatalf("fields: %s / %s", wrongName.Body.String(), wrongPassword.Body.String())
	}
	apitest.Call(t, m.h, "DELETE", path, `{"name":"x","password":"y","force":true}`, owner.CSRFToken, owner.Cookie(), 400)
	// A tab opened before a rename can't delete it under the old name.
	apitest.Call(t, m.h, "PATCH", path, `{"name":"Renamed"}`, admin.CSRFToken, admin.Cookie(), 200)
	apitest.Call(t, m.h, "DELETE", path, deleteWorkspaceBody(name, factory.DefaultPassword), owner.CSRFToken, owner.Cookie(), 422)

	apitest.Call(t, m.h, "DELETE", path, deleteWorkspaceBody(" Renamed ", factory.DefaultPassword), owner.CSRFToken, owner.Cookie(), 204)
	for _, session := range []factory.Session{owner, admin, member} {
		apitest.Call(t, m.h, "GET", path, "", "", session.Cookie(), 404)
	}
	if rowsWhere(t, m, (*factory.WorkspaceUser)(nil), "workspace_id = ?", m.workspace.ID) != 0 ||
		rowsWhere(t, m, (*factory.Project)(nil), "workspace_id = ?", m.workspace.ID) != 0 {
		t.Fatal("memberships or projects outlived their workspace")
	}
	// Its history stays, without the workspace.
	e := apitest.OnlyEvent(t, m.db, "workspace.deleted")
	if !apitest.Is(e.ActorID, owner.UserID) || e.WorkspaceID.Valid || e.Payload["workspace"] != m.workspace.PublicID ||
		e.Payload["name"] != "Renamed" || e.Payload["projects"] != 3.0 || e.Payload["members"] != 3.0 {
		t.Fatalf("event: %s", e.Raw)
	}
	if renamed := apitest.OnlyEvent(t, m.db, "workspace.renamed"); renamed.WorkspaceID.Valid || renamed.Payload["workspace"] != m.workspace.PublicID {
		t.Fatalf("earlier event: %s", renamed.Raw)
	}
	steps := []string{}
	for _, check := range apitest.EventsNamed(t, m.db, "auth.password_checked") {
		steps = append(steps, fmt.Sprintf("%v %v", check.Payload["step"], check.Payload["ok"]))
	}
	if !slices.Equal(steps, []string{"destroy_workspace false", "destroy_workspace true"}) {
		t.Fatalf("password checks: %v", steps)
	}
	apitest.CheckPayloadIDs(t, m.db)
}
func TestDeleteWorkspaceRateLimit(t *testing.T) {
	m := newMemberFixture(t)
	ctx := context.Background()
	email := apitest.EmailOf(t, m.db, m.owner.UserID)
	apitest.Must(factory.RateLimits.State(factory.ForAccount(email)).State(factory.Exhausted(10)).CreateOne(ctx, m.db, m.f))(t)
	apitest.Call(t, m.h, "DELETE", m.path(""), deleteWorkspaceBody(m.workspace.Name, factory.DefaultPassword), m.owner.CSRFToken, m.owner.Cookie(), 429)
	apitest.Call(t, m.h, "GET", m.path(""), "", "", m.owner.Cookie(), 200)
}

// Deleting a workspace while its owner changes a member's role: one waits
// for the other's workspace lock, and nothing is left in the deleted
// workspace.
func TestDeleteWorkspaceDuringRoleChange(t *testing.T) {
	m := newMemberFixture(t)
	other := m.join(t, auth.RoleOwner)
	member := m.join(t, auth.RoleMember)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Go(func() {
		codes[0] = apitest.Send(m.h, "DELETE", m.path(""), deleteWorkspaceBody(m.workspace.Name, factory.DefaultPassword), m.owner.CSRFToken, m.owner.Cookie()).Code
	})
	wg.Go(func() {
		codes[1] = apitest.Send(m.h, "PATCH", m.path("/members/"+m.publicID(t, member)), `{"role":"viewer"}`, other.CSRFToken, other.Cookie()).Code
	})
	wg.Wait()
	if codes[0] != 204 || (codes[1] != 200 && codes[1] != 404) {
		t.Fatalf("codes %v", codes)
	}
	if rowsWhere(t, m, (*factory.WorkspaceUser)(nil), "workspace_id = ?", m.workspace.ID) != 0 {
		t.Fatal("a membership outlived its workspace")
	}
}

// lastOwnerRefusal is the 409 body for an account that's a workspace's only owner.
type lastOwnerRefusal struct {
	Error      string                   `json:"error"`
	Workspaces []store.WorkspaceSummary `json:"workspaces"`
}

func TestDeleteAccount(t *testing.T) {
	m := newMemberFixture(t)
	ctx := context.Background()
	h, owner := m.h, m.owner
	email := apitest.EmailOf(t, m.db, owner.UserID)
	ownerID := m.publicID(t, owner)
	// A second session of the same account, signed out by the deletion.
	second := apitest.Must(factory.SessionFor(ctx, m.db, m.f, owner.UserID))(t)
	// Alone in a workspace of their own too, so two workspaces block.
	alone := apitest.OwnedWorkspace(t, m.db, m.f, owner.UserID)
	apitest.Call(t, h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), "", owner.Cookie(), 403)
	apitest.Call(t, h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), owner.CSRFToken, nil, 401)
	wrong := apitest.Call(t, h, "DELETE", "/api/auth/me", deleteAccountBody("not my password"), owner.CSRFToken, owner.Cookie(), 422)
	if apitest.ErrorField(t, wrong) != "password" {
		t.Fatalf("wrong password: %s", wrong.Body.String())
	}

	var refusal lastOwnerRefusal
	body := apitest.Call(t, h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), owner.CSRFToken, owner.Cookie(), 409).Body.Bytes()
	if err := json.Unmarshal(body, &refusal); err != nil {
		t.Fatal(err)
	}
	want := []store.WorkspaceSummary{{PublicID: m.workspace.PublicID, Name: m.workspace.Name}, {PublicID: alone.PublicID, Name: alone.Name}}
	if refusal.Error == "" || !slices.Equal(refusal.Workspaces, want) || apitest.NumericID.Match(body) {
		t.Fatalf("refusal: %s", body)
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", owner.Cookie(), 200)
	if len(apitest.EventsNamed(t, m.db, "auth.account_deleted")) != 0 {
		t.Fatal("refused deletion recorded")
	}

	// A co-owner in the shared workspace, and their own one deleted: now it goes.
	coOwner := m.join(t, auth.RoleOwner)
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(alone.PublicID, ""), deleteWorkspaceBody(alone.Name, factory.DefaultPassword), owner.CSRFToken, owner.Cookie(), 204)
	// The owner renames the shared workspace: an event they acted in, about
	// the workspace, which stays with a null actor.
	apitest.Call(t, h, "PATCH", m.path(""), `{"name":"Shared"}`, owner.CSRFToken, owner.Cookie(), 200)
	deleted := apitest.Call(t, h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), owner.CSRFToken, owner.Cookie(), 204)
	if cookie := deleted.Result().Cookies(); len(cookie) != 1 || cookie[0].MaxAge >= 0 {
		t.Fatalf("session cookie not cleared: %v", cookie)
	}
	for _, session := range []factory.Session{owner, second} {
		apitest.Call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 401)
	}
	if m.roleOf(t, coOwner.UserID) != auth.RoleOwner || m.roleOf(t, owner.UserID) != "" {
		t.Fatal("the shared workspace lost its other owner, or kept the deleted one")
	}
	if rowsWhere(t, m, (*factory.User)(nil), "id = ?", owner.UserID) != 0 ||
		rowsWhere(t, m, (*factory.Event)(nil), "user_id = ?", owner.UserID) != 0 ||
		rowsWhere(t, m, (*factory.Event)(nil), "actor_id = ?", owner.UserID) != 0 {
		t.Fatal("the account or events about it remain")
	}
	if renamed := apitest.OnlyEvent(t, m.db, "workspace.renamed"); renamed.ActorID.Valid || !apitest.Is(renamed.WorkspaceID, m.workspace.ID) {
		t.Fatalf("an event the account acted in: %s", renamed.Raw)
	}
	e := apitest.OnlyEvent(t, m.db, "auth.account_deleted")
	if e.ActorID.Valid || e.UserID.Valid || e.WorkspaceID.Valid || e.Raw != fmt.Sprintf(`{"user": "%s"}`, ownerID) {
		t.Fatalf("event: %s", e.Raw)
	}
	// The email is free again.
	register := fmt.Sprintf(`{"name":"Again","email":%q,"password":%q}`, email, factory.DefaultPassword)
	apitest.Call(t, h, "POST", "/api/auth/register", register, "", nil, 201)
	apitest.CheckPayloadIDs(t, m.db)
}

// Two co-owners deleting their accounts at once: one goes, the other is the
// workspace's last owner (fails without the workspace lock).
func TestCoOwnersDeletingTheirAccounts(t *testing.T) {
	m := newMemberFixture(t)
	for range 5 {
		pair := []factory.Session{m.join(t, auth.RoleOwner), m.join(t, auth.RoleOwner)}
		// Only the pair own the workspace this round.
		apitest.Call(t, m.h, "PATCH", m.path("/members/"+m.publicID(t, m.owner)), `{"role":"admin"}`, pair[0].CSRFToken, pair[0].Cookie(), 200)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i, session := range pair {
			wg.Go(func() {
				codes[i] = apitest.Send(m.h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), session.CSRFToken, session.Cookie()).Code
			})
		}
		wg.Wait()
		slices.Sort(codes)
		owners := rowsWhere(t, m, (*factory.WorkspaceUser)(nil), "workspace_id = ? AND role = ?", m.workspace.ID, auth.RoleOwner)
		if !slices.Equal(codes, []int{204, 409}) || owners != 1 {
			t.Fatalf("codes %v, %d owners left", codes, owners)
		}
		// Hand the workspace back to the fixture's owner for the next round.
		for _, session := range pair {
			if m.roleOf(t, session.UserID) == auth.RoleOwner {
				apitest.Call(t, m.h, "PATCH", m.path("/members/"+m.publicID(t, m.owner)), `{"role":"owner"}`, session.CSRFToken, session.Cookie(), 200)
				apitest.Call(t, m.h, "DELETE", m.path("/members/"+m.publicID(t, session)), "", m.owner.CSRFToken, m.owner.Cookie(), 204)
			}
		}
	}
}

// Two users who co-own the same two workspaces (with a third owner, so both
// may go) delete their accounts at once. Taking the workspace locks in id
// order means neither waits on a lock the other holds, so neither deadlocks.
func TestAccountDeletionsDontDeadlock(t *testing.T) {
	m := newMemberFixture(t)
	ctx := context.Background()
	second := apitest.OwnedWorkspace(t, m.db, m.f, m.owner.UserID)
	for range 5 {
		pair := []factory.Session{apitest.SignedIn(t, m.db, m.f), apitest.SignedIn(t, m.db, m.f)}
		for _, session := range pair {
			for _, w := range []int64{m.workspace.ID, second.ID} {
				apitest.Must(factory.MemberOf(ctx, m.db, m.f, session.UserID, w, auth.RoleOwner))(t)
			}
		}
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i, session := range pair {
			wg.Go(func() {
				codes[i] = apitest.Send(m.h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), session.CSRFToken, session.Cookie()).Code
			})
		}
		wg.Wait()
		if codes[0] != 204 || codes[1] != 204 {
			t.Fatalf("codes %v", codes)
		}
	}
}

// canLeave=false lists exactly the workspaces whose canLeave is false.
func TestWorkspacesYouCantLeave(t *testing.T) {
	m := newMemberFixture(t)
	ctx := context.Background()
	user := m.owner
	shared := apitest.OwnedWorkspace(t, m.db, m.f, user.UserID)
	apitest.Must(factory.MemberOf(ctx, m.db, m.f, apitest.SignedIn(t, m.db, m.f).UserID, shared.ID, auth.RoleOwner))(t)
	for _, role := range auth.Roles[1:] {
		elsewhere := apitest.OwnedWorkspace(t, m.db, m.f, apitest.SignedIn(t, m.db, m.f).UserID)
		apitest.Must(factory.MemberOf(ctx, m.db, m.f, user.UserID, elsewhere.ID, role))(t)
	}
	all := listWorkspaces(t, m.h, user.Cookie(), "")
	stuck := listWorkspaces(t, m.h, user.Cookie(), "canLeave=false")
	var want, got []string
	for _, w := range all.Data {
		if !w.CanLeave {
			want = append(want, w.PublicID)
		}
	}
	for _, w := range stuck.Data {
		got = append(got, w.PublicID)
	}
	if all.Pages.Items != 5 || !slices.Equal(got, []string{m.workspace.PublicID}) || !slices.Equal(got, want) {
		t.Fatalf("all %+v, can't leave %v, want %v", all.Data, got, want)
	}
	for _, query := range []string{"canLeave=true", "canLeave=0", "canLeave=", "canLeave=false&canLeave=false"} {
		apitest.Call(t, m.h, "GET", "/api/workspaces?"+query, "", "", user.Cookie(), 400)
	}
}

// A DELETE that has a body sends JSON, like every other mutation; one
// without a body needs no content type.
func TestDeleteBodiesNeedJSON(t *testing.T) {
	m := newMemberFixture(t)
	project := apitest.Must(factory.ProjectsIn(m.workspace.ID).CreateOne(context.Background(), m.db, m.f))(t)
	deleteAs := func(path, contentType, body string) int {
		r := httptest.NewRequest("DELETE", path, strings.NewReader(body))
		r.Header.Set("Origin", "http://localhost:3000")
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		r.Header.Set("X-CSRF-Token", m.owner.CSRFToken)
		r.AddCookie(m.owner.Cookie())
		w := httptest.NewRecorder()
		m.h.ServeHTTP(w, r)
		return w.Code
	}
	if code := deleteAs("/api/auth/me", "text/plain", deleteAccountBody(factory.DefaultPassword)); code != 415 {
		t.Fatalf("text/plain DELETE: %d", code)
	}
	if code := deleteAs("/api/auth/me", "", deleteAccountBody(factory.DefaultPassword)); code != 415 {
		t.Fatalf("DELETE body without a content type: %d", code)
	}
	apitest.Call(t, m.h, "GET", "/api/auth/me", "", "", m.owner.Cookie(), 200)
	if code := deleteAs(m.path("/projects/"+project.PublicID), "", ""); code != 204 {
		t.Fatalf("DELETE without a body: %d", code)
	}
}
