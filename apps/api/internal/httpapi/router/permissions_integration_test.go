package router

import (
	"context"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// server is a Server on a migrated database of its own; New(s) is its
// handler.
func server(t *testing.T) (*httpapi.Server, *bun.DB) {
	t.Helper()
	db := apitest.Database(t)
	return apitest.Must(httpapi.New(&store.Store{DB: db}, apitest.Config()))(t), db
}

// workspaceRoute is one row of the permission matrix: a route inside a
// workspace, a request to it, and the roles allowed to use it.
type workspaceRoute struct {
	pattern string
	path    string // after /api/workspaces/{workspace}
	body    string
	// newBody, when set, makes each request's body instead of body.
	newBody func() string
	ok      int
	allowed []auth.Role // nil: every member
	// target appends a new record's ID to path for each request, for routes
	// that change or delete the record they name: "member" (a viewer's user
	// ID), "project" or "invitation" (a viewer's).
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
		{pattern: "GET /api/workspaces/{workspace}/invitations", path: "/invitations", ok: 200, allowed: owners},
		{pattern: "POST /api/workspaces/{workspace}/invitations", path: "/invitations", newBody: newInvitee, ok: 201, allowed: owners},
		{pattern: "DELETE /api/workspaces/{workspace}/invitations/{invitation}", path: "/invitations", ok: 204, allowed: owners, target: "invitation"},
		{pattern: "DELETE /api/workspaces/{workspace}/membership", path: "/membership", ok: 204},
	}
}

var invitees atomic.Int64

// newInvitee is an invitation body to an address nobody has invited yet.
func newInvitee() string {
	return fmt.Sprintf(`{"email":"invitee-%d@example.test","role":"viewer"}`, invitees.Add(1))
}

// bodyFor is the body of one request to the route.
func (route workspaceRoute) bodyFor() string {
	if route.newBody != nil {
		return route.newBody()
	}
	return route.body
}

func TestWorkspacePermissionMatrix(t *testing.T) {
	s, db := server(t)
	h := New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	project := apitest.Must(factory.ProjectsIn(workspace.ID).CreateOne(ctx, db, f))(t)
	sessions := map[auth.Role]factory.Session{auth.RoleOwner: owner}
	for _, role := range auth.Roles[1:] {
		sessions[role] = apitest.SignedIn(t, db, f)
		apitest.Must(factory.MemberOf(ctx, db, f, sessions[role].UserID, workspace.ID, role))(t)
	}
	// A second owner, so the first may leave.
	apitest.Must(factory.MemberOf(ctx, db, f, apitest.SignedIn(t, db, f).UserID, workspace.ID, auth.RoleOwner))(t)
	newViewer := func() string {
		user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
		apitest.Must(factory.MemberOf(ctx, db, f, user.ID, workspace.ID, auth.RoleViewer))(t)
		return user.PublicID
	}
	newProject := func() string {
		return apitest.Must(factory.ProjectsIn(workspace.ID).CreateOne(ctx, db, f))(t).PublicID
	}
	newInvitation := func() string {
		return apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.InvitedAs(fmt.Sprintf("revoke-%d@example.test", invitees.Add(1)), auth.RoleViewer)).CreateOne(ctx, db, f))(t).PublicID
	}
	newWorkspace := func() string {
		w := apitest.Must(factory.WorkspaceFor(ctx, db, f, owner.UserID, factory.Named("Matrix")))(t)
		for _, role := range auth.Roles[1:] {
			apitest.Must(factory.MemberOf(ctx, db, f, sessions[role].UserID, w.ID, role))(t)
		}
		return w.PublicID
	}
	outsider := apitest.SignedIn(t, db, f)
	matrix := workspaceRoutesMatrix(project.PublicID)

	var registered, listed []string
	workspaceRoutes(s, routeGroup{mux: http.NewServeMux(), prefix: "/api/workspaces/{workspace}", registered: &registered})
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
				return apitest.WorkspacePath(id, route.path+"/"+newViewer())
			case "project":
				return apitest.WorkspacePath(id, route.path+"/"+newProject())
			case "invitation":
				return apitest.WorkspacePath(id, route.path+"/"+newInvitation())
			}
			return apitest.WorkspacePath(id, route.path)
		}
		for _, role := range auth.Roles {
			want := 403
			if route.allowed == nil || slices.Contains(route.allowed, role) {
				want = route.ok
			}
			session := sessions[role]
			t.Run(route.pattern+" as "+string(role), func(t *testing.T) {
				apitest.Call(t, h, method, pathFor(), route.bodyFor(), session.CSRFToken, session.Cookie(), want)
			})
		}
		t.Run(route.pattern+" as an outsider", func(t *testing.T) {
			apitest.Call(t, h, method, pathFor(), route.bodyFor(), outsider.CSRFToken, outsider.Cookie(), 404)
		})
	}
}
