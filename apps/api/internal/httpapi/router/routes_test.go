package router

import (
	"fmt"
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
// RequireAdminKey: a route added anywhere else in routes.go fails here. And
// every admin route that changes anything names the admin role it needs,
// apart from the door's sign-in steps and the admin's own sessions.
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
		if (strings.Contains(line, "mux.Handle") || strings.Contains(line, "group(mux")) && !strings.Contains(line, "RequireAdminKey") {
			t.Fatalf("an admin route without RequireAdminKey: %s", strings.TrimSpace(line))
		}
	}
	// The mutations that need no role, and why. Anything else that isn't a
	// GET must be registered with Role.
	unguarded := map[string]string{
		`door.Handle("POST", "/login"`:               "the sign-in's password step: no session yet",
		`door.HandleFunc("POST", "/login/mfa/setup"`: "the sign-in's MFA setup: no session yet",
		`door.HandleFunc("POST", "/login/mfa"`:       "the sign-in's code: no session yet",
		`signedIn.OwnSessions("POST", "/logout"`:     "signing out: ends only this session",
		`signedIn.OwnSessions("DELETE", "/sessions"`: "signing out your other admin sessions (decision 5): only takes your own access away, so no role or code",
	}
	registration := regexp.MustCompile(`^\s*(\w+)\.(Handle|HandleFunc|Role|OwnSessions)\("(\w+)", "([^"]*)"`)
	// Any call that registers a route, however it's written.
	call := regexp.MustCompile(`\b(\w+)\.(Handle|HandleFunc|Role|OwnSessions|handle)\(`)
	found := map[string]bool{}
	for _, line := range strings.Split(inside, "\n") {
		c := call.FindStringSubmatch(line)
		if c == nil || c[1] == "mux" {
			continue
		}
		if c[2] == "handle" {
			t.Errorf("registered with handle, which skips the role check: %s", strings.TrimSpace(line))
			continue
		}
		m := registration.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("a registration this test can't read (write it on one line, method and path as string literals): %s", strings.TrimSpace(line))
			continue
		}
		if m[3] == "GET" || m[3] == "HEAD" || m[2] == "Role" {
			continue
		}
		registered := fmt.Sprintf(`%s.%s(%q, %q`, m[1], m[2], m[3], m[4])
		if unguarded[registered] == "" {
			t.Errorf("%s %s needs an admin role: register it with Role", m[3], m[4])
		}
		found[registered] = true
	}
	for registered := range unguarded {
		if !found[registered] {
			t.Errorf("%s is listed as needing no role, but isn't registered so", registered)
		}
	}
}

// An admin mutation registered without a role stops the server starting,
// as a workspace one without a permission does.
func TestAdminMutationsNeedARole(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s without a role was registered", method)
				}
			}()
			signedIn := group(http.NewServeMux(), "/api/admin")
			signedIn.requireAdminRoles = true
			signedIn.HandleFunc(method, "/things", ok)
		})
	}
	signedIn := group(http.NewServeMux(), "/api/admin")
	signedIn.requireAdminRoles = true
	signedIn.HandleFunc("GET", "/things", ok)
	signedIn.Role("POST", "/things", auth.AdminOperator, ok)
	signedIn.OwnSessions("DELETE", "/sessions", ok)
	signedIn.OwnSessions("POST", "/logout", ok)
	// OwnSessions takes only the admin's own session routes.
	for _, route := range [][2]string{{"POST", "/things"}, {"DELETE", "/users/{user}"}, {"POST", "/sessions"}, {"DELETE", "/logout"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("OwnSessions took %s %s", route[0], route[1])
				}
			}()
			signedIn.OwnSessions(route[0], route[1], ok)
		}()
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
