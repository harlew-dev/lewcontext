package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"time"
)

// sensitiveKeys finds keys in a JSON value that look like secrets or
// internal IDs; an admin response must have none.
func sensitiveKeys(value any, path string) []string {
	var found []string
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			lower := strings.ToLower(key)
			for _, word := range []string{"password", "hash", "token", "secret", "recovery", "csrf", "otp"} {
				if strings.Contains(lower, word) {
					found = append(found, path+"."+key)
				}
			}
			if _, number := child.(float64); key == "id" && number {
				found = append(found, path+".id (numeric)")
			}
			found = append(found, sensitiveKeys(child, path+"."+key)...)
		}
	case []any:
		for i, child := range v {
			found = append(found, sensitiveKeys(child, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return found
}

func decodeAdmin[T any](t *testing.T, body []byte) T {
	t.Helper()
	var raw any
	apitest.Must(0, json.Unmarshal(body, &raw))(t)
	if found := sensitiveKeys(raw, ""); len(found) > 0 {
		t.Fatalf("sensitive fields in an admin response: %v", found)
	}
	var v T
	apitest.Must(0, json.Unmarshal(body, &v))(t)
	return v
}

func TestAdminUsers(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	users := apitest.Must(factory.Users.Sequence(func(i int, u *factory.User) {
		u.Name = fmt.Sprintf("Zed %02d", i)
		u.Email = fmt.Sprintf("zed%02d_x@example.test", i)
		u.CreatedAt = time.Now().AddDate(0, 0, -30+i)
	}).Create(ctx, db, f, 25))(t)
	workspace := apitest.OwnedWorkspace(t, db, f, users[0].ID)
	apitest.Must(factory.MemberOf(ctx, db, f, users[1].ID, workspace.ID, "viewer"))(t)
	apitest.Must(factory.SessionFor(ctx, db, f, users[0].ID))(t)
	cookie := session.Cookie()

	first := decodeAdmin[store.Page[store.AdminUser]](t, adminCall(t, h, "GET", "/api/admin/users", "", "", 200, cookie).Body.Bytes())
	if first.Pages.Items != 26 || len(first.Data) != 20 {
		t.Fatalf("page 1: %+v", first.Pages)
	}
	for i := 1; i < len(first.Data); i++ {
		if first.Data[i].CreatedAt.After(first.Data[i-1].CreatedAt) {
			t.Fatalf("not newest first at %d: %+v", i, first.Data)
		}
	}
	byName := decodeAdmin[store.Page[store.AdminUser]](t, adminCall(t, h, "GET", "/api/admin/users?q=zed&sort=name&perPage=2&page=2", "", "", 200, cookie).Body.Bytes())
	if len(byName.Data) != 2 || byName.Data[0].Name != "Zed 02" {
		t.Fatalf("by name, page 2: %+v", byName.Data)
	}
	// Search covers names and emails, with LIKE wildcards taken literally.
	found := decodeAdmin[store.Page[store.AdminUser]](t, adminCall(t, h, "GET", "/api/admin/users?q=zed01_x", "", "", 200, cookie).Body.Bytes())
	if found.Pages.Items != 1 || found.Data[0].PublicID != users[1].PublicID {
		t.Fatalf("search: %+v", found.Data)
	}
	if none := decodeAdmin[store.Page[store.AdminUser]](t, adminCall(t, h, "GET", "/api/admin/users?q=zed0%25", "", "", 200, cookie).Body.Bytes()); none.Pages.Items != 0 {
		t.Fatalf("%% matched as a wildcard: %+v", none.Data)
	}
	self := decodeAdmin[store.Page[store.AdminUser]](t, adminCall(t, h, "GET", "/api/admin/users?q="+admin.User.Email, "", "", 200, cookie).Body.Bytes()).Data[0]
	if !self.Admin || !self.MFAEnabled {
		t.Fatalf("the admin's own row: %+v", self)
	}
	for _, query := range []string{"?sort=password", "?page=0", "?perPage=101", "?role=owner", "?q=a&q=b"} {
		adminCall(t, h, "GET", "/api/admin/users"+query, "", "", 400, cookie)
	}

	w := adminCall(t, h, "GET", "/api/admin/users/"+users[0].PublicID, "", "", 200, cookie)
	detail := decodeAdmin[store.AdminUserDetail](t, w.Body.Bytes())
	if detail.Email != users[0].Email || detail.WorkspaceCount != 1 || detail.SessionCount != 1 || detail.Admin || detail.MFAEnabled ||
		len(detail.Memberships) != 1 || detail.Memberships[0].Workspace.PublicID != workspace.PublicID || detail.Memberships[0].Role != "owner" {
		t.Fatalf("user detail: %s", w.Body.String())
	}
	for _, id := range []string{"usr_2yEHYLwiwPVhLW", "usr_short", "1", workspace.PublicID} {
		adminCall(t, h, "GET", "/api/admin/users/"+id, "", "", 404, cookie)
	}

	// Each read is recorded, by the admin, about nobody's own events.
	listed := apitest.EventsNamed(t, db, "admin.user.listed")
	if len(listed) != 5 || listed[1].Payload["sort"] != "name" || listed[1].Payload["page"] != 2.0 || listed[2].Payload["q"] != "zed01_x" {
		t.Fatalf("admin.user.listed: %+v", listed)
	}
	viewed := apitest.OnlyEvent(t, db, "admin.user.viewed")
	if viewed.Payload["admin"] != admin.User.PublicID || viewed.Payload["user"] != users[0].PublicID || !apitest.Is(viewed.ActorID, admin.User.ID) || viewed.UserID.Valid || viewed.WorkspaceID.Valid {
		t.Fatalf("admin.user.viewed: %+v", viewed)
	}
	apitest.CheckPayloadIDs(t, db)
}

func TestAdminWorkspaces(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	owners := apitest.Must(factory.Users.Create(ctx, db, f, 4))(t)
	busy := apitest.Must(factory.WorkspaceFor(ctx, db, f, owners[0].ID, factory.Named("Busy & Co")))(t)
	for _, owner := range owners[1:] {
		apitest.Must(factory.MemberOf(ctx, db, f, owner.ID, busy.ID, "owner"))(t)
	}
	viewer := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.MemberOf(ctx, db, f, viewer.ID, busy.ID, "viewer"))(t)
	apitest.Must(factory.ProjectsIn(busy.ID).Create(ctx, db, f, 3))(t)
	empty := apitest.OwnedWorkspace(t, db, f, viewer.ID)

	page := decodeAdmin[store.Page[store.AdminWorkspace]](t, adminCall(t, h, "GET", "/api/admin/workspaces?sort=name", "", "", 200, cookie).Body.Bytes())
	if page.Pages.Items != 2 {
		t.Fatalf("workspaces: %+v", page)
	}
	var row store.AdminWorkspace
	for _, w := range page.Data {
		if w.PublicID == busy.PublicID {
			row = w
		}
	}
	if row.MemberCount != 5 || row.ProjectCount != 3 || len(row.Owners) != 3 || row.Owners[0].PublicID != owners[0].PublicID || row.Owners[0].Email != owners[0].Email {
		t.Fatalf("busy workspace: %+v", row)
	}
	found := decodeAdmin[store.Page[store.AdminWorkspace]](t, adminCall(t, h, "GET", "/api/admin/workspaces?q=busy+%26", "", "", 200, cookie).Body.Bytes())
	if found.Pages.Items != 1 || found.Data[0].PublicID != busy.PublicID {
		t.Fatalf("search: %+v", found)
	}
	adminCall(t, h, "GET", "/api/admin/workspaces?sort=email", "", "", 400, cookie)

	w := adminCall(t, h, "GET", "/api/admin/workspaces/"+busy.PublicID, "", "", 200, cookie)
	detail := decodeAdmin[store.AdminWorkspaceDetail](t, w.Body.Bytes())
	if len(detail.Members) != 5 || detail.Members[4].PublicID != viewer.PublicID || detail.Members[4].Role != "viewer" || detail.ProjectCount != 3 {
		t.Fatalf("workspace detail: %s", w.Body.String())
	}
	if e := decodeAdmin[store.AdminWorkspaceDetail](t, adminCall(t, h, "GET", "/api/admin/workspaces/"+empty.PublicID, "", "", 200, cookie).Body.Bytes()); len(e.Owners) != 1 || e.ProjectCount != 0 {
		t.Fatalf("empty workspace: %+v", e)
	}
	for _, id := range []string{"wsp_2yEHYLwiwPVhLW", "wsp_x", viewer.PublicID} {
		adminCall(t, h, "GET", "/api/admin/workspaces/"+id, "", "", 404, cookie)
	}
	viewed := apitest.EventsNamed(t, db, "admin.workspace.viewed")
	if len(viewed) != 2 || viewed[0].Payload["workspace"] != busy.PublicID || viewed[0].Payload["admin"] != admin.User.PublicID || viewed[0].WorkspaceID.Valid {
		t.Fatalf("admin.workspace.viewed: %+v", viewed)
	}
	if listed := apitest.EventsNamed(t, db, "admin.workspace.listed"); len(listed) != 2 || listed[1].Payload["q"] != "busy &" {
		t.Fatalf("admin.workspace.listed: %+v", listed)
	}
	// Deleting the workspace keeps the record that an admin looked at it.
	apitest.Must(0, s.Repo.DeleteWorkspace(ctx, store.Workspace{ID: busy.ID, PublicID: busy.PublicID}, "Busy & Co"))(t)
	if len(apitest.EventsNamed(t, db, "admin.workspace.viewed")) != 2 {
		t.Fatal("deleting the workspace deleted the admin's trail")
	}
}
