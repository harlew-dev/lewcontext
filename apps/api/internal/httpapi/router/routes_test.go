package router

import (
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi"
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
	registration := regexp.MustCompile(`^\s*(\w+)\.(Handle|HandleFunc|Role|Action|Housekeeping|OwnSessions)\("(\w+)", "([^"]*)"`)
	// Any call that registers a route, however it's written.
	call := regexp.MustCompile(`\b(\w+)\.(Handle|HandleFunc|Role|Action|Housekeeping|OwnSessions|handle)\(`)
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
		// Action and Housekeeping add the operator role and the action
		// limit themselves (Housekeeping only for housekeepingRoutes).
		if m[3] == "GET" || m[3] == "HEAD" || m[2] == "Action" || m[2] == "Housekeeping" {
			continue
		}
		// An operator's mutation is an Action (a fresh code and the action
		// limit), apart from the step-up that opens the window.
		if m[2] == "Role" {
			if !strings.Contains(line, "auth.AdminViewer") && !(m[3] == "POST" && m[4] == "/step-up") {
				t.Errorf("%s %s is an operator mutation registered with Role: register it with Action", m[3], m[4])
			}
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
	signedIn.server = &httpapi.Server{}
	signedIn.HandleFunc("GET", "/things", ok)
	signedIn.Role("POST", "/things", auth.AdminViewer, ok)
	signedIn.Role("POST", "/step-up", auth.AdminOperator, ok)
	signedIn.Action("DELETE", "/things", ok)
	signedIn.OwnSessions("DELETE", "/sessions", ok)
	// An operator's mutation other than the step-up must be an Action.
	for _, route := range [][2]string{{"POST", "/things"}, {"DELETE", "/email-blocks"}, {"PATCH", "/step-up"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Role(operator) took %s %s", route[0], route[1])
				}
			}()
			signedIn.Role(route[0], route[1], auth.AdminOperator, ok)
		}()
	}
	// Action is only for the signed-in admin group.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Action registered outside the admin group")
			}
		}()
		group(http.NewServeMux(), "/api/workspaces/{workspace}").Action("POST", "/things", ok)
	}()
	signedIn.OwnSessions("POST", "/logout", ok)
	// Housekeeping takes only housekeepingRoutes, so an account-and-access
	// action can't use it to skip its code (silencing an alert among
	// them); and only in the signed-in admin group.
	signedIn.Housekeeping("PATCH", "/alerts/{alert}", ok)
	for _, route := range [][2]string{{"POST", "/things"}, {"DELETE", "/users/{user}/sessions"}, {"POST", "/alerts/{alert}/silence"}, {"PATCH", "/users/{user}"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Housekeeping took %s %s", route[0], route[1])
				}
			}()
			signedIn.Housekeeping(route[0], route[1], ok)
		}()
	}
	housekeepingRoutes["PATCH /chores/{chore}"] = true
	defer delete(housekeepingRoutes, "PATCH /chores/{chore}")
	signedIn.Housekeeping("PATCH", "/chores/{chore}", ok)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Housekeeping registered outside the admin group")
			}
		}()
		group(http.NewServeMux(), "/api/workspaces/{workspace}").Housekeeping("PATCH", "/chores/{chore}", ok)
	}()
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

// Only the nine routes an unverified account needs skip the check: reading
// the session, signing out, signing out other sessions, asking for a code
// and entering it. A new route is covered unless someone adds it here.
func TestOnlyNineRoutesAllowUnverified(t *testing.T) {
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
	want = append(want, "GET /api/auth/email-change", "POST /api/auth/email-change", "PATCH /api/auth/email-change", "DELETE /api/auth/email-change")
	slices.Sort(want)
	if !slices.Equal(allowed, want) || strings.Count(string(source), "httpapi.AllowUnverified(") != len(want) {
		t.Fatalf("AllowUnverified on %v", allowed)
	}
}
