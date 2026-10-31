package workspace_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
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
	if err := json.Unmarshal(apitest.Call(t, h, "GET", "/api/workspaces?"+query, "", "", cookie, 200).Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
func TestWorkspaceList(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	other := apitest.SignedIn(t, db, f)
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
	apitest.OwnedWorkspace(t, db, f, other.UserID)

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
		apitest.Call(t, h, "GET", "/api/workspaces?"+query, "", "", session.Cookie(), 400)
	}
	apitest.Call(t, h, "GET", "/api/workspaces", "", "", nil, 401)
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
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	path := apitest.WorkspacePath(workspace.PublicID, "")
	join := func(role auth.Role) factory.Session {
		session := apitest.SignedIn(t, db, f)
		apitest.Must(factory.MemberOf(ctx, db, f, session.UserID, workspace.ID, role))(t)
		return session
	}
	admin, member, viewer, outsider := join(auth.RoleAdmin), join(auth.RoleMember), join(auth.RoleViewer), apitest.SignedIn(t, db, f)
	sessions := map[auth.Role]factory.Session{auth.RoleOwner: owner, auth.RoleAdmin: admin, auth.RoleMember: member, auth.RoleViewer: viewer}

	t.Run("members see the workspace with their role and permissions", func(t *testing.T) {
		for role, session := range sessions {
			body := apitest.Call(t, h, "GET", path, "", "", session.Cookie(), 200).Body.Bytes()
			var got store.Workspace
			var access workspaceAccess
			_ = json.Unmarshal(body, &got)
			_ = json.Unmarshal(body, &access)
			if got.PublicID != workspace.PublicID || got.Name != workspace.Name || !sameAccess(access, role) {
				t.Fatalf("%s sees %s", role, body)
			}
			var list struct{ Data []workspaceAccess }
			_ = json.Unmarshal(apitest.Call(t, h, "GET", "/api/workspaces", "", "", session.Cookie(), 200).Body.Bytes(), &list)
			if len(list.Data) != 1 || !sameAccess(list.Data[0], role) {
				t.Fatalf("%s's list: %+v", role, list.Data)
			}
		}
	})
	t.Run("owners and admins rename", func(t *testing.T) {
		body := apitest.Call(t, h, "PATCH", path, `{"name":" By admin "}`, admin.CSRFToken, admin.Cookie(), 200).Body.Bytes()
		var renamed store.Workspace
		var access workspaceAccess
		_ = json.Unmarshal(body, &renamed)
		_ = json.Unmarshal(body, &access)
		if renamed.Name != "By admin" || renamed.PublicID != workspace.PublicID || !sameAccess(access, auth.RoleAdmin) {
			t.Fatalf("rename: %s", body)
		}
		for _, body := range []string{`{"name":""}`, `{"name":"   "}`, fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 81)), `{"name":"a\u0000b"}`} {
			apitest.Call(t, h, "PATCH", path, body, owner.CSRFToken, owner.Cookie(), 422)
		}
		apitest.Call(t, h, "PATCH", path, `{"name":"x","role":"owner"}`, owner.CSRFToken, owner.Cookie(), 400)
	})
	t.Run("outsiders and malformed IDs get 404", func(t *testing.T) {
		apitest.Call(t, h, "GET", path, "", "", outsider.Cookie(), 404)
		apitest.Call(t, h, "PATCH", path, `{"name":"Taken"}`, outsider.CSRFToken, outsider.Cookie(), 404)
		apitest.Call(t, h, "GET", path+"/projects", "", "", outsider.Cookie(), 404)
		for _, id := range []string{"1", "abc", "wsp_short", "prj_" + workspace.PublicID[4:], "wsp_00000000000000"} {
			apitest.Call(t, h, "GET", "/api/workspaces/"+id+"/projects", "", "", owner.Cookie(), 404)
		}
		apitest.Call(t, h, "GET", path, "", "", nil, 401)
	})
	t.Run("creating validates the name and makes you its owner", func(t *testing.T) {
		apitest.Call(t, h, "POST", "/api/workspaces", `{"name":""}`, owner.CSRFToken, owner.Cookie(), 422)
		apitest.Call(t, h, "POST", "/api/workspaces", `{"name":"Fine","id":"wsp_00000000000000"}`, owner.CSRFToken, owner.Cookie(), 400)
		body := apitest.Call(t, h, "POST", "/api/workspaces", `{"name":"Fine"}`, viewer.CSRFToken, viewer.Cookie(), 201).Body.Bytes()
		var access workspaceAccess
		_ = json.Unmarshal(body, &access)
		if !sameAccess(access, auth.RoleOwner) {
			t.Fatalf("created: %s", body)
		}
	})
}
