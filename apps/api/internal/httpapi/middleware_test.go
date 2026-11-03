package httpapi

import (
	"context"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

// RequirePermission reads the role RequireWorkspace loaded, never the request.
func TestRequirePermission(t *testing.T) {
	handler := RequirePermission(auth.ProjectStore, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
