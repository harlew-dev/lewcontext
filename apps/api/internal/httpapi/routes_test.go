package httpapi

import (
	"context"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A workspace mutation registered without a permission stops the server
// starting, so it can't ship unguarded.
func TestWorkspaceMutationsNeedAPermission(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s without a permission was registered", method)
				}
			}()
			ws := group(http.NewServeMux(), "/api/workspaces/{workspace}")
			ws.requirePermissions = true
			ws.HandleFunc(method, "/things", ok)
		})
	}
	ws := group(http.NewServeMux(), "/api/workspaces/{workspace}")
	ws.requirePermissions = true
	ws.HandleFunc("GET", "/things", ok)
	ws.Can("POST", "/things", auth.ProjectStore, ok)
	ws.Can("DELETE", "/things", auth.AnyMember, ok)
}

// requirePermission reads the role requireWorkspace loaded, never the request.
func TestRequirePermission(t *testing.T) {
	handler := requirePermission(auth.ProjectStore, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
	}))
	for role, want := range map[auth.Role]int{auth.RoleOwner: 201, auth.RoleMember: 201, auth.RoleViewer: 403, "": 403, "root": 403} {
		r := httptest.NewRequest("POST", "/api/workspaces/wsp_00000000000000/projects?role=owner", nil)
		r = r.WithContext(context.WithValue(r.Context(), workspaceKey{}, store.Workspace{Role: role}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%q: %d, want %d", role, w.Code, want)
		}
	}
}
