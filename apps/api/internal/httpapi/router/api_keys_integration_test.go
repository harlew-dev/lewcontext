package router

import (
	"context"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestPublicApiPermissionMatrix(t *testing.T) {
	s, db := server(t)
	h := New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	manager := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, manager.UserID)
	rows := []struct {
		method, path, scope string
		success             int
	}{
		{"GET", "/projects", "project.index", 200},
		{"POST", "/projects", "project.store", 201},
		{"GET", "/projects/{project}", "project.show", 200},
		{"PATCH", "/projects/{project}", "project.update", 200},
		{"DELETE", "/projects/{project}", "project.destroy", 204},
	}
	var registered, listed []string
	publicProjectRoutes(s, routeGroup{mux: http.NewServeMux(), prefix: "/api/v1/workspaces/{workspace}", registered: &registered})
	for _, row := range rows {
		listed = append(listed, row.method+" /api/v1/workspaces/{workspace}"+row.path)
	}
	slices.Sort(registered)
	slices.Sort(listed)
	if !slices.Equal(registered, listed) {
		t.Fatal("every v1 route must be covered by the credential/role/scope matrix")
	}
	for _, kind := range []auth.ApiKeyKind{auth.ApiKeyWorkspace, auth.ApiKeyPersonal} {
		for _, role := range auth.Roles {
			person := apitest.SignedIn(t, db, f)
			apitest.Must(factory.MemberOf(ctx, db, f, person.UserID, workspace.ID, role))(t)
			key := apitest.Must(factory.ApiKeys.State(func(k *factory.ApiKey) {
				k.WorkspaceID = workspace.ID
				k.Kind = string(kind)
				k.TokenHash = auth.Digest(f.UUID())
				k.RevokedAt = nil
				if kind == auth.ApiKeyPersonal {
					k.OwnerUserID = person.UserID
				}
			}).CreateOne(ctx, db, f))(t)
			secret, digest, err := auth.NewApiKey(kind, key.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				for _, granted := range []bool{false, true} {
					scopes := []string{"project.index"}
					if granted {
						scopes = []string{row.scope}
					} else if row.scope == "project.index" {
						scopes = []string{"project.show"}
					}
					key.TokenHash, key.Scopes = digest, scopes
					if _, err := db.NewUpdate().Model(&key).Column("token_hash", "scopes").WherePK().Exec(ctx); err != nil {
						t.Fatal(err)
					}
					p := apitest.Must(factory.ProjectsIn(workspace.ID).CreateOne(ctx, db, f))(t)
					path := "/api/v1/workspaces/" + workspace.PublicID + strings.Replace(row.path, "{project}", p.PublicID, 1)
					r := httptest.NewRequest(row.method, path, strings.NewReader(`{"name":"Matrix","description":"","status":"Planned","color":"blue"}`))
					r.Header.Set("Authorization", "Bearer "+secret)
					r.Header.Set("Content-Type", "application/json")
					r.RemoteAddr = "127.0.0.1:54321"
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					want := 403
					if granted && (kind == auth.ApiKeyWorkspace || auth.ApiKeyRoleAllows(role, row.scope)) {
						want = row.success
					}
					if w.Code != want {
						t.Fatalf("%s %s %s granted=%v: %d %s; want %d", kind, role, row.scope, granted, w.Code, w.Body.String(), want)
					}
				}
			}
		}
	}
}
