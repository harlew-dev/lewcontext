package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func listWorkspaces(t *testing.T, h http.Handler, cookie *http.Cookie, query string) store.Page[store.Workspace] {
	t.Helper()
	var page store.Page[store.Workspace]
	if err := json.Unmarshal(call(t, h, "GET", "/api/workspaces?"+query, "", "", cookie, 200).Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
func TestWorkspaceList(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	other := signedIn(t, db, f)
	// 25 workspaces an hour apart, so "newest first" is predictable.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 25 {
		_, err := factory.WorkspaceFor(ctx, db, f, session.UserID, func(w *factory.Workspace) {
			w.Name = fmt.Sprintf("Workspace %02d", i)
			w.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	ownedWorkspace(t, db, f, other.UserID)

	first := listWorkspaces(t, h, session.Cookie(), "")
	if first.Pages != (store.Pages{Current: 1, Total: 2, PerPage: 20, Items: 25}) {
		t.Fatalf("pages: %+v", first.Pages)
	}
	if first.Data[0].Name != "Workspace 24" || first.Data[19].Name != "Workspace 05" || first.Data[0].Role != auth.RoleOwner {
		t.Fatalf("not newest first: %+v", first.Data[0])
	}
	second := listWorkspaces(t, h, session.Cookie(), "page=2")
	names := []string{}
	for _, w := range append(first.Data, second.Data...) {
		names = append(names, w.Name)
	}
	slices.Sort(names)
	if len(names) != 25 || len(slices.Compact(names)) != 25 {
		t.Fatalf("pages repeated or skipped workspaces: %v", names)
	}
	if got := listWorkspaces(t, h, other.Cookie(), ""); got.Pages.Items != 1 {
		t.Fatalf("another user's workspaces leaked: %+v", got.Pages)
	}
	for _, query := range []string{"page=0", "perPage=101", "q=Workspace", "sort=name", "page=1&page=2"} {
		call(t, h, "GET", "/api/workspaces?"+query, "", "", session.Cookie(), 400)
	}
	call(t, h, "GET", "/api/workspaces", "", "", nil, 401)
}

// workspaceAccess is the permissions part of a workspace response.
type workspaceAccess struct {
	Role            auth.Role         `json:"role"`
	Permissions     []auth.Permission `json:"permissions"`
	AssignableRoles []auth.Role       `json:"assignableRoles"`
	CanLeave        bool              `json:"canLeave"`
}

// sameAccess reports whether a response carries what role allows, in a
// workspace with one owner (who therefore can't leave).
func sameAccess(got workspaceAccess, role auth.Role) bool {
	return got.Role == role && got.CanLeave == (role != auth.RoleOwner) && slices.Equal(got.Permissions, role.Permissions()) && slices.Equal(got.AssignableRoles, role.AssignableRoles()) && got.Permissions != nil && got.AssignableRoles != nil
}
func TestWorkspaceMembershipAndRoles(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	owner := signedIn(t, db, f)
	workspace := ownedWorkspace(t, db, f, owner.UserID)
	path := workspacePath(workspace.PublicID, "")
	join := func(role auth.Role) factory.Session {
		session := signedIn(t, db, f)
		must(factory.MemberOf(ctx, db, f, session.UserID, workspace.ID, role))(t)
		return session
	}
	admin, member, viewer, outsider := join(auth.RoleAdmin), join(auth.RoleMember), join(auth.RoleViewer), signedIn(t, db, f)
	sessions := map[auth.Role]factory.Session{auth.RoleOwner: owner, auth.RoleAdmin: admin, auth.RoleMember: member, auth.RoleViewer: viewer}

	t.Run("members see the workspace with their role and permissions", func(t *testing.T) {
		for role, session := range sessions {
			body := call(t, h, "GET", path, "", "", session.Cookie(), 200).Body.Bytes()
			var got store.Workspace
			var access workspaceAccess
			_ = json.Unmarshal(body, &got)
			_ = json.Unmarshal(body, &access)
			if got.PublicID != workspace.PublicID || got.Name != workspace.Name || !sameAccess(access, role) {
				t.Fatalf("%s sees %s", role, body)
			}
			var list struct{ Data []workspaceAccess }
			_ = json.Unmarshal(call(t, h, "GET", "/api/workspaces", "", "", session.Cookie(), 200).Body.Bytes(), &list)
			if len(list.Data) != 1 || !sameAccess(list.Data[0], role) {
				t.Fatalf("%s's list: %+v", role, list.Data)
			}
		}
	})
	t.Run("owners and admins rename", func(t *testing.T) {
		body := call(t, h, "PATCH", path, `{"name":" By admin "}`, admin.CSRFToken, admin.Cookie(), 200).Body.Bytes()
		var renamed store.Workspace
		var access workspaceAccess
		_ = json.Unmarshal(body, &renamed)
		_ = json.Unmarshal(body, &access)
		if renamed.Name != "By admin" || renamed.PublicID != workspace.PublicID || !sameAccess(access, auth.RoleAdmin) {
			t.Fatalf("rename: %s", body)
		}
		for _, body := range []string{`{"name":""}`, `{"name":"   "}`, fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 81)), `{"name":"a\u0000b"}`} {
			call(t, h, "PATCH", path, body, owner.CSRFToken, owner.Cookie(), 422)
		}
		call(t, h, "PATCH", path, `{"name":"x","role":"owner"}`, owner.CSRFToken, owner.Cookie(), 400)
	})
	t.Run("outsiders and malformed IDs get 404", func(t *testing.T) {
		call(t, h, "GET", path, "", "", outsider.Cookie(), 404)
		call(t, h, "PATCH", path, `{"name":"Taken"}`, outsider.CSRFToken, outsider.Cookie(), 404)
		call(t, h, "GET", path+"/projects", "", "", outsider.Cookie(), 404)
		for _, id := range []string{"1", "abc", "wsp_short", "prj_" + workspace.PublicID[4:], "wsp_00000000000000"} {
			call(t, h, "GET", "/api/workspaces/"+id+"/projects", "", "", owner.Cookie(), 404)
		}
		call(t, h, "GET", path, "", "", nil, 401)
	})
	t.Run("creating validates the name and makes you its owner", func(t *testing.T) {
		call(t, h, "POST", "/api/workspaces", `{"name":""}`, owner.CSRFToken, owner.Cookie(), 422)
		call(t, h, "POST", "/api/workspaces", `{"name":"Fine","id":"wsp_00000000000000"}`, owner.CSRFToken, owner.Cookie(), 400)
		body := call(t, h, "POST", "/api/workspaces", `{"name":"Fine"}`, viewer.CSRFToken, viewer.Cookie(), 201).Body.Bytes()
		var access workspaceAccess
		_ = json.Unmarshal(body, &access)
		if !sameAccess(access, auth.RoleOwner) {
			t.Fatalf("created: %s", body)
		}
	})
}

// workspaceRoute is one row of the permission matrix: a route inside a
// workspace, a request to it, and the roles allowed to use it.
type workspaceRoute struct {
	pattern string
	path    string // after /api/workspaces/{workspace}
	body    string
	ok      int
	allowed []auth.Role // nil: every member
	// target appends a new record's ID to path for each request, for routes
	// that change or delete the record they name: "member" (a viewer's user
	// ID) or "project".
	target string
	// fresh sends each request to a new workspace named "Matrix" that every
	// role belongs to, for routes that delete the workspace.
	fresh bool
}

// Every route inside a workspace, for every role: allowed roles get the
// route's status, refused roles 403 and outsiders 404. A new workspace route
// fails TestWorkspacePermissionMatrix until it has a row here. Leaving comes
// last, since each role then stops being a member.
func workspaceRoutesMatrix(projectID string) []workspaceRoute {
	project := `{"name":"Matrix","description":"","status":"Planned","color":"blue"}`
	deletion := fmt.Sprintf(`{"name":"Matrix","password":%q}`, factory.DefaultPassword)
	owners := []auth.Role{auth.RoleOwner, auth.RoleAdmin}
	editors := []auth.Role{auth.RoleOwner, auth.RoleAdmin, auth.RoleMember}
	return []workspaceRoute{
		{pattern: "GET /api/workspaces/{workspace}", ok: 200},
		{pattern: "PATCH /api/workspaces/{workspace}", body: `{"name":"Renamed"}`, ok: 200, allowed: owners},
		{pattern: "DELETE /api/workspaces/{workspace}", body: deletion, ok: 204, allowed: []auth.Role{auth.RoleOwner}, fresh: true},
		{pattern: "GET /api/workspaces/{workspace}/projects", path: "/projects", ok: 200},
		{pattern: "POST /api/workspaces/{workspace}/projects", path: "/projects", body: project, ok: 201, allowed: editors},
		{pattern: "GET /api/workspaces/{workspace}/projects/{project}", path: "/projects/" + projectID, ok: 200},
		{pattern: "PATCH /api/workspaces/{workspace}/projects/{project}", path: "/projects/" + projectID, body: project, ok: 200, allowed: editors},
		{pattern: "DELETE /api/workspaces/{workspace}/projects/{project}", path: "/projects", ok: 204, allowed: owners, target: "project"},
		{pattern: "GET /api/workspaces/{workspace}/members", path: "/members", ok: 200, allowed: owners},
		{pattern: "PATCH /api/workspaces/{workspace}/members/{member}", path: "/members", body: `{"role":"member"}`, ok: 200, allowed: owners, target: "member"},
		{pattern: "DELETE /api/workspaces/{workspace}/members/{member}", path: "/members", ok: 204, allowed: owners, target: "member"},
		{pattern: "DELETE /api/workspaces/{workspace}/membership", path: "/membership", ok: 204},
	}
}
func TestWorkspacePermissionMatrix(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	owner := signedIn(t, db, f)
	workspace := ownedWorkspace(t, db, f, owner.UserID)
	project := must(factory.ProjectsIn(workspace.ID).CreateOne(ctx, db, f))(t)
	sessions := map[auth.Role]factory.Session{auth.RoleOwner: owner}
	for _, role := range auth.Roles[1:] {
		sessions[role] = signedIn(t, db, f)
		must(factory.MemberOf(ctx, db, f, sessions[role].UserID, workspace.ID, role))(t)
	}
	// A second owner, so the first may leave.
	must(factory.MemberOf(ctx, db, f, signedIn(t, db, f).UserID, workspace.ID, auth.RoleOwner))(t)
	newViewer := func() string {
		user := must(factory.Users.CreateOne(ctx, db, f))(t)
		must(factory.MemberOf(ctx, db, f, user.ID, workspace.ID, auth.RoleViewer))(t)
		return user.PublicID
	}
	newProject := func() string {
		return must(factory.ProjectsIn(workspace.ID).CreateOne(ctx, db, f))(t).PublicID
	}
	newWorkspace := func() string {
		w := must(factory.WorkspaceFor(ctx, db, f, owner.UserID, factory.Named("Matrix")))(t)
		for _, role := range auth.Roles[1:] {
			must(factory.MemberOf(ctx, db, f, sessions[role].UserID, w.ID, role))(t)
		}
		return w.PublicID
	}
	outsider := signedIn(t, db, f)
	matrix := workspaceRoutesMatrix(project.PublicID)

	var registered, listed []string
	s.workspaceRoutes(routeGroup{mux: http.NewServeMux(), prefix: "/api/workspaces/{workspace}", registered: &registered})
	for _, route := range matrix {
		listed = append(listed, route.pattern)
	}
	slices.Sort(registered)
	slices.Sort(listed)
	if !slices.Equal(registered, listed) {
		t.Fatalf("workspace routes %v, matrix lists %v", registered, listed)
	}

	for _, route := range matrix {
		method, _, _ := strings.Cut(route.pattern, " ")
		pathFor := func() string {
			id := workspace.PublicID
			if route.fresh {
				id = newWorkspace()
			}
			switch route.target {
			case "member":
				return workspacePath(id, route.path+"/"+newViewer())
			case "project":
				return workspacePath(id, route.path+"/"+newProject())
			}
			return workspacePath(id, route.path)
		}
		for _, role := range auth.Roles {
			want := 403
			if route.allowed == nil || slices.Contains(route.allowed, role) {
				want = route.ok
			}
			session := sessions[role]
			t.Run(route.pattern+" as "+string(role), func(t *testing.T) {
				call(t, h, method, pathFor(), route.body, session.CSRFToken, session.Cookie(), want)
			})
		}
		t.Run(route.pattern+" as an outsider", func(t *testing.T) {
			call(t, h, method, pathFor(), route.body, outsider.CSRFToken, outsider.Cookie(), 404)
		})
	}
}
