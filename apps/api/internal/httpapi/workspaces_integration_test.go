package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"forma/api/internal/factory"
	"forma/api/internal/store"
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
	if first.Data[0].Name != "Workspace 24" || first.Data[19].Name != "Workspace 05" || first.Data[0].Role != store.RoleOwner {
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
func TestWorkspaceMembershipAndRoles(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	owner := signedIn(t, db, f)
	workspace := ownedWorkspace(t, db, f, owner.UserID)
	path := workspacePath(workspace.PublicID, "")
	join := func(role string) factory.Session {
		session := signedIn(t, db, f)
		must(factory.MemberOf(ctx, db, f, session.UserID, workspace.ID, role))(t)
		return session
	}
	admin, member, outsider := join(store.RoleAdmin), join(store.RoleMember), signedIn(t, db, f)

	t.Run("members see the workspace with their role", func(t *testing.T) {
		for session, role := range map[*factory.Session]string{&owner: "owner", &admin: "admin", &member: "member"} {
			var got store.Workspace
			_ = json.Unmarshal(call(t, h, "GET", path, "", "", session.Cookie(), 200).Body.Bytes(), &got)
			if got.PublicID != workspace.PublicID || got.Name != workspace.Name || got.Role != role {
				t.Fatalf("%s sees %+v", role, got)
			}
		}
	})
	t.Run("only owners and admins rename", func(t *testing.T) {
		call(t, h, "PATCH", path, `{"name":"By member"}`, member.CSRFToken, member.Cookie(), 403)
		var renamed store.Workspace
		_ = json.Unmarshal(call(t, h, "PATCH", path, `{"name":" By admin "}`, admin.CSRFToken, admin.Cookie(), 200).Body.Bytes(), &renamed)
		if renamed.Name != "By admin" || renamed.Role != store.RoleAdmin || renamed.PublicID != workspace.PublicID {
			t.Fatalf("rename: %+v", renamed)
		}
		call(t, h, "PATCH", path, `{"name":"By owner"}`, owner.CSRFToken, owner.Cookie(), 200)
		for _, body := range []string{`{"name":""}`, `{"name":"   "}`, fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 81)), `{"name":"a\u0000b"}`} {
			call(t, h, "PATCH", path, body, owner.CSRFToken, owner.Cookie(), 422)
		}
		call(t, h, "PATCH", path, `{"name":"x","role":"owner"}`, owner.CSRFToken, owner.Cookie(), 400)
	})
	t.Run("every member can work on projects", func(t *testing.T) {
		body := `{"name":"Shared","description":"","status":"Planned","color":"blue"}`
		var project store.Project
		_ = json.Unmarshal(call(t, h, "POST", path+"/projects", body, member.CSRFToken, member.Cookie(), 201).Body.Bytes(), &project)
		call(t, h, "GET", path+"/projects/"+project.PublicID, "", "", admin.Cookie(), 200)
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
	t.Run("creating validates the name", func(t *testing.T) {
		call(t, h, "POST", "/api/workspaces", `{"name":""}`, owner.CSRFToken, owner.Cookie(), 422)
		call(t, h, "POST", "/api/workspaces", `{"name":"Fine","id":"wsp_00000000000000"}`, owner.CSRFToken, owner.Cookie(), 400)
	})
}
