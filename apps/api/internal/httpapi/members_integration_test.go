package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// memberFixture is a workspace with an owner and a way to add people to it.
type memberFixture struct {
	h         http.Handler
	db        *bun.DB
	f         *gofakeit.Faker
	owner     factory.Session
	workspace factory.Workspace
}

func newMemberFixture(t *testing.T) memberFixture {
	t.Helper()
	s, db := server(t)
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	return memberFixture{h: router.New(s), db: db, f: f, owner: owner, workspace: apitest.OwnedWorkspace(t, db, f, owner.UserID)}
}

// join signs a new user in and adds them with role.
func (m memberFixture) join(t *testing.T, role auth.Role) factory.Session {
	t.Helper()
	session := apitest.SignedIn(t, m.db, m.f)
	apitest.Must(factory.MemberOf(context.Background(), m.db, m.f, session.UserID, m.workspace.ID, role))(t)
	return session
}

// publicID is the session user's usr_ ID.
func (m memberFixture) publicID(t *testing.T, session factory.Session) string {
	t.Helper()
	var id string
	err := m.db.NewSelect().Model((*factory.User)(nil)).Column("public_id").Where("id = ?", session.UserID).Scan(context.Background(), &id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (m memberFixture) path(rest string) string {
	return apitest.WorkspacePath(m.workspace.PublicID, rest)
}

// roleOf is the user's role in the fixture's workspace, or "" when they
// aren't a member.
func (m memberFixture) roleOf(t *testing.T, userID int64) auth.Role {
	t.Helper()
	var roles []auth.Role
	err := m.db.NewSelect().Model((*factory.WorkspaceUser)(nil)).Column("role").
		Where("workspace_id = ? AND user_id = ?", m.workspace.ID, userID).
		Scan(context.Background(), &roles)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) == 0 {
		return ""
	}
	return roles[0]
}
func listMembers(t *testing.T, m memberFixture, session factory.Session, query string) store.Page[store.Member] {
	t.Helper()
	var page store.Page[store.Member]
	body := apitest.Call(t, m.h, "GET", m.path("/members?"+query), "", "", session.Cookie(), 200).Body.Bytes()
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if apitest.NumericID.Match(body) {
		t.Fatalf("internal ID in %s", body)
	}
	return page
}
func TestMemberList(t *testing.T) {
	m := newMemberFixture(t)
	ctx := context.Background()
	added := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	add := func(name string, role auth.Role) {
		user := apitest.Must(factory.Users.State(func(u *factory.User) { u.Name = name }).CreateOne(ctx, m.db, m.f))(t)
		apitest.Must(factory.WorkspaceUsers.State(func(w *factory.WorkspaceUser) {
			w.UserID, w.WorkspaceID, w.Role, w.CreatedAt = user.ID, m.workspace.ID, role, added
		}).CreateOne(ctx, m.db, m.f))(t)
	}
	add("viewer Vera", auth.RoleViewer)
	add("Bea Member", auth.RoleMember)
	add("aaron Member", auth.RoleMember)
	add("Zoe 100% Admin", auth.RoleAdmin)
	add("Ann_Owner", auth.RoleOwner)
	// Someone in another workspace never appears.
	apitest.OwnedWorkspace(t, m.db, m.f, apitest.SignedIn(t, m.db, m.f).UserID)

	page := listMembers(t, m, m.owner, "")
	var names []string
	for _, member := range page.Data {
		names = append(names, member.Name)
	}
	if page.Pages.Items != 6 || len(names) != 6 {
		t.Fatalf("members: %v", names)
	}
	// Owners first (by name), then admins, members and viewers.
	if names[2] != "Zoe 100% Admin" || names[3] != "aaron Member" || names[4] != "Bea Member" || names[5] != "viewer Vera" {
		t.Fatalf("order: %v", names)
	}
	if page.Data[0].Role != auth.RoleOwner || page.Data[1].Role != auth.RoleOwner {
		t.Fatalf("owners not first: %v", names)
	}
	for _, member := range page.Data {
		if !input.PublicID("usr", member.PublicID) || member.Email == "" {
			t.Fatalf("member: %+v", member)
		}
		if member.Name == "viewer Vera" && !member.AddedAt.Equal(added) {
			t.Fatalf("addedAt %v, want %v", member.AddedAt, added)
		}
	}

	second := listMembers(t, m, m.owner, "perPage=4&page=2")
	if len(second.Data) != 2 || second.Data[1].Name != "viewer Vera" || second.Pages.Total != 2 {
		t.Fatalf("page 2: %+v", second)
	}
	for query, want := range map[string][]string{
		"q=member":    {"aaron Member", "Bea Member"},
		"q=100%25":    {"Zoe 100% Admin"},
		"q=%25":       {"Zoe 100% Admin"},
		"q=n_o":       {"Ann_Owner"},
		"q=_":         {"Ann_Owner"},
		"q=nobody":    nil,
		"q=EXAMPLE.T": names,
	} {
		var got []string
		for _, member := range listMembers(t, m, m.owner, query).Data {
			got = append(got, member.Name)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", query, got, want)
		}
	}
	admin := m.join(t, auth.RoleAdmin)
	if listMembers(t, m, admin, "").Pages.Items != 7 {
		t.Fatal("admin sees a different list")
	}
	for _, query := range []string{"page=0", "sort=name", "q=" + strings.Repeat("x", 101), "q=a%00b"} {
		apitest.Call(t, m.h, "GET", m.path("/members?"+query), "", "", m.owner.Cookie(), 400)
	}
}
func TestChangeMemberRole(t *testing.T) {
	m := newMemberFixture(t)
	admin, member, viewer := m.join(t, auth.RoleAdmin), m.join(t, auth.RoleMember), m.join(t, auth.RoleViewer)
	change := func(actor, target factory.Session, body string, want int) *store.Member {
		t.Helper()
		res := apitest.Call(t, m.h, "PATCH", m.path("/members/"+m.publicID(t, target)), body, actor.CSRFToken, actor.Cookie(), want)
		var got store.Member
		_ = json.Unmarshal(res.Body.Bytes(), &got)
		return &got
	}

	t.Run("admins move people between member and viewer", func(t *testing.T) {
		got := change(admin, member, `{"role":"viewer"}`, 200)
		if got.Role != auth.RoleViewer || got.PublicID != m.publicID(t, member) || got.Email == "" || m.roleOf(t, member.UserID) != auth.RoleViewer {
			t.Fatalf("changed: %+v", got)
		}
		change(admin, member, `{"role":"member"}`, 200)
	})
	t.Run("admins can't touch admins or owners, or make admins", func(t *testing.T) {
		other := m.join(t, auth.RoleAdmin)
		change(admin, other, `{"role":"member"}`, 403)
		change(admin, m.owner, `{"role":"member"}`, 403)
		change(admin, member, `{"role":"admin"}`, 403)
		change(admin, member, `{"role":"owner"}`, 403)
		change(admin, admin, `{"role":"member"}`, 403)
		if m.roleOf(t, other.UserID) != auth.RoleAdmin || m.roleOf(t, m.owner.UserID) != auth.RoleOwner || m.roleOf(t, member.UserID) != auth.RoleMember {
			t.Fatal("a refused change was saved")
		}
	})
	t.Run("the role change is recorded once, and not when unchanged", func(t *testing.T) {
		change(m.owner, viewer, `{"role":"viewer"}`, 200)
		if len(apitest.EventsNamed(t, m.db, "workspace.member.updated")) != 2 {
			t.Fatal("an unchanged role was recorded")
		}
		change(m.owner, viewer, `{"role":"admin"}`, 200)
		found := apitest.EventsNamed(t, m.db, "workspace.member.updated")
		e := found[len(found)-1]
		if !apitest.Is(e.ActorID, m.owner.UserID) || !apitest.Is(e.UserID, viewer.UserID) || !apitest.Is(e.WorkspaceID, m.workspace.ID) ||
			e.Payload["user"] != m.publicID(t, viewer) || e.Payload["workspace"] != m.workspace.PublicID ||
			!strings.Contains(e.Raw, `"changes": {"role": {"to": "admin", "from": "viewer"}}`) {
			t.Fatalf("event: %s", e.Raw)
		}
		change(m.owner, viewer, `{"role":"viewer"}`, 200)
	})
	t.Run("the last owner can't step down until there's another", func(t *testing.T) {
		change(m.owner, m.owner, `{"role":"admin"}`, 409)
		change(m.owner, member, `{"role":"owner"}`, 200)
		change(m.owner, m.owner, `{"role":"admin"}`, 200)
		if m.roleOf(t, m.owner.UserID) != auth.RoleAdmin {
			t.Fatal("not demoted")
		}
		change(member, m.owner, `{"role":"owner"}`, 200)
		change(m.owner, member, `{"role":"member"}`, 200)
	})
	t.Run("bad bodies and unknown members", func(t *testing.T) {
		for _, body := range []string{`{"role":"Owner"}`, `{"role":"guest"}`, `{"role":""}`, `{}`} {
			change(m.owner, viewer, body, 422)
		}
		change(m.owner, viewer, `{"role":"member","name":"x"}`, 400)
		elsewhere := apitest.SignedIn(t, m.db, m.f)
		apitest.OwnedWorkspace(t, m.db, m.f, elsewhere.UserID)
		change(m.owner, elsewhere, `{"role":"member"}`, 404)
		for _, id := range []string{"1", "usr_short", m.workspace.PublicID, "usr_00000000000000"} {
			apitest.Call(t, m.h, "PATCH", m.path("/members/"+id), `{"role":"member"}`, m.owner.CSRFToken, m.owner.Cookie(), 404)
		}
	})
}

// Two owners demoting each other at once leave one owner, never none.
func TestOwnersDemotingEachOther(t *testing.T) {
	m := newMemberFixture(t)
	for range 5 {
		other := m.join(t, auth.RoleOwner)
		ids := map[int64]string{m.owner.UserID: m.publicID(t, m.owner), other.UserID: m.publicID(t, other)}
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i, pair := range [][2]factory.Session{{m.owner, other}, {other, m.owner}} {
			actor, target := pair[0], pair[1]
			wg.Go(func() {
				codes[i] = apitest.Send(m.h, "PATCH", m.path("/members/"+ids[target.UserID]), `{"role":"member"}`, actor.CSRFToken, actor.Cookie()).Code
			})
		}
		wg.Wait()
		slices.Sort(codes)
		owners := 0
		for _, id := range []int64{m.owner.UserID, other.UserID} {
			if m.roleOf(t, id) == auth.RoleOwner {
				owners++
			}
		}
		if owners != 1 || codes[0] != 200 || (codes[1] != 403 && codes[1] != 409) {
			t.Fatalf("codes %v, %d owners left", codes, owners)
		}
		// Start the next round from the original owner.
		if m.roleOf(t, m.owner.UserID) != auth.RoleOwner {
			apitest.Call(t, m.h, "PATCH", m.path("/members/"+m.publicID(t, m.owner)), `{"role":"owner"}`, other.CSRFToken, other.Cookie(), 200)
		}
		apitest.Call(t, m.h, "DELETE", m.path("/members/"+m.publicID(t, other)), "", m.owner.CSRFToken, m.owner.Cookie(), 204)
	}
}
func TestRemoveMember(t *testing.T) {
	m := newMemberFixture(t)
	admin, member := m.join(t, auth.RoleAdmin), m.join(t, auth.RoleMember)
	remove := func(actor, target factory.Session, want int) {
		t.Helper()
		apitest.Call(t, m.h, "DELETE", m.path("/members/"+m.publicID(t, target)), "", actor.CSRFToken, actor.Cookie(), want)
	}
	apitest.Call(t, m.h, "GET", m.path(""), "", "", member.Cookie(), 200)
	remove(admin, m.owner, 403)
	remove(admin, m.join(t, auth.RoleAdmin), 403)
	remove(m.owner, m.owner, 422)
	remove(admin, admin, 422)

	remove(admin, member, 204)
	if m.roleOf(t, member.UserID) != "" {
		t.Fatal("still a member")
	}
	apitest.Call(t, m.h, "GET", m.path(""), "", "", member.Cookie(), 404)
	apitest.Call(t, m.h, "GET", m.path("/projects"), "", "", member.Cookie(), 404)
	apitest.Call(t, m.h, "GET", "/api/auth/me", "", "", member.Cookie(), 200)
	remove(admin, member, 404)
	e := apitest.OnlyEvent(t, m.db, "workspace.member.removed")
	if !apitest.Is(e.ActorID, admin.UserID) || !apitest.Is(e.UserID, member.UserID) || !apitest.Is(e.WorkspaceID, m.workspace.ID) ||
		e.Payload["user"] != m.publicID(t, member) || e.Payload["role"] != "member" || e.Payload["workspace"] != m.workspace.PublicID {
		t.Fatalf("event: %s", e.Raw)
	}

	// Owners may remove other owners.
	second := m.join(t, auth.RoleOwner)
	remove(m.owner, second, 204)
	// Someone in another workspace is not found here.
	elsewhere := apitest.SignedIn(t, m.db, m.f)
	apitest.OwnedWorkspace(t, m.db, m.f, elsewhere.UserID)
	remove(m.owner, elsewhere, 404)
	apitest.CheckPayloadIDs(t, m.db)
}
func TestLeaveWorkspace(t *testing.T) {
	m := newMemberFixture(t)
	viewer := m.join(t, auth.RoleViewer)
	leave := func(session factory.Session, want int) {
		t.Helper()
		apitest.Call(t, m.h, "DELETE", m.path("/membership"), "", session.CSRFToken, session.Cookie(), want)
	}
	leave(viewer, 204)
	apitest.Call(t, m.h, "GET", m.path(""), "", "", viewer.Cookie(), 404)
	if listWorkspaces(t, m.h, viewer.Cookie(), "").Pages.Items != 0 {
		t.Fatal("left workspace still listed")
	}
	e := apitest.OnlyEvent(t, m.db, "workspace.member.left")
	if !apitest.Is(e.ActorID, viewer.UserID) || !apitest.Is(e.UserID, viewer.UserID) || !apitest.Is(e.WorkspaceID, m.workspace.ID) ||
		e.Payload["role"] != "viewer" || e.Payload["workspace"] != m.workspace.PublicID {
		t.Fatalf("event: %s", e.Raw)
	}
	leave(viewer, 404)

	canLeave := func(session factory.Session) bool {
		t.Helper()
		var listed struct{ Data []workspaceAccess }
		var one workspaceAccess
		_ = json.Unmarshal(apitest.Call(t, m.h, "GET", m.path(""), "", "", session.Cookie(), 200).Body.Bytes(), &one)
		_ = json.Unmarshal(apitest.Call(t, m.h, "GET", "/api/workspaces", "", "", session.Cookie(), 200).Body.Bytes(), &listed)
		if len(listed.Data) != 1 || listed.Data[0].CanLeave != one.CanLeave {
			t.Fatalf("list and show disagree: %+v, %+v", listed.Data, one)
		}
		return one.CanLeave
	}
	if canLeave(m.owner) {
		t.Fatal("the only owner may leave")
	}
	leave(m.owner, 409)
	if m.roleOf(t, m.owner.UserID) != auth.RoleOwner {
		t.Fatal("the last owner left")
	}
	other := m.join(t, auth.RoleOwner)
	if !canLeave(m.owner) || !canLeave(other) {
		t.Fatal("owners can't leave with another owner")
	}
	leave(m.owner, 204)
	if canLeave(other) {
		t.Fatal("the remaining owner may leave")
	}
}
