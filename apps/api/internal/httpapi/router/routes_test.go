package router

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi/apitest"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
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

// Every /api/admin/ route is registered in adminRoutes, behind
// RequireAdminKey: a route added anywhere else in routes.go fails here.
func TestAdminRoutesStayInTheAdminGroup(t *testing.T) {
	source := string(apitest.Must(os.ReadFile("routes.go"))(t))
	start := strings.Index(source, "func adminRoutes(")
	end := strings.Index(source[start:], "\n}\n")
	if start < 0 || end < 0 {
		t.Fatal("adminRoutes not found in routes.go")
	}
	outside := source[:start] + source[start+end:]
	if regexp.MustCompile(`"(GET |POST |PATCH |DELETE |PUT )?/api/admin`).MatchString(outside) {
		t.Fatal("an /api/admin route is registered outside adminRoutes")
	}
	inside := source[start : start+end]
	for _, line := range strings.Split(inside, "\n") {
		if strings.Contains(line, "mux.Handle") && !strings.Contains(line, "RequireAdminKey") {
			t.Fatalf("an admin route without RequireAdminKey: %s", strings.TrimSpace(line))
		}
	}
}

// Only the five routes an unverified account needs skip the check: reading
// the session, signing out, signing out other sessions, asking for a code
// and entering it. A new route is covered unless someone adds it here.
func TestOnlyFiveRoutesAllowUnverified(t *testing.T) {
	source, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	var allowed []string
	for _, m := range regexp.MustCompile(`mux\.Handle\("([^"]+)", httpapi\.AllowUnverified\(`).FindAllStringSubmatch(string(source), -1) {
		allowed = append(allowed, m[1])
	}
	slices.Sort(allowed)
	want := []string{"DELETE /api/auth/sessions", "GET /api/auth/me", "PATCH /api/auth/email-verification", "POST /api/auth/email-verification", "POST /api/auth/logout"}
	if !slices.Equal(allowed, want) || strings.Count(string(source), "httpapi.AllowUnverified(") != len(want) {
		t.Fatalf("AllowUnverified on %v", allowed)
	}
}
