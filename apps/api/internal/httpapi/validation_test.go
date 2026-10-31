package httpapi

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCredentialsValidation(t *testing.T) {
	for _, tc := range []struct {
		email, password string
		want            bool
	}{{" HELLO@example.com ", "a long safe passphrase", true}, {"Display <a@example.com>", "a long safe passphrase", false}, {"bad", "a long safe passphrase", false}, {"a@example.com", "short", true}, {"a@example.com", "", false}, {"a@example.com", strings.Repeat("x", 129), false}} {
		in := Credentials{Email: tc.email, Password: tc.password}
		if ValidCredentials(&in) != tc.want {
			t.Errorf("unexpected validation for %s / %q", tc.email, tc.password)
		}
	}
}
func TestStrongPassword(t *testing.T) {
	for password, want := range map[string]bool{"password": false, "elevenchars": false, "twelve chars": true, "ünïcödé pässwörd": true} {
		if StrongPassword(password) != want {
			t.Errorf("strongPassword(%q) != %v", password, want)
		}
	}
}
func TestProjectValidation(t *testing.T) {
	good := store.ProjectInput{Name: " A project ", Status: "Planned", Color: "violet"}
	if !validProject(&good) || good.Name != "A project" {
		t.Fatal("valid project rejected")
	}
	for _, in := range []store.ProjectInput{{Name: "", Status: "Planned", Color: "violet"}, {Name: "Test", Status: "Unknown", Color: "violet"}, {Name: "Test", Status: "Planned", Color: "red"}, {Name: strings.Repeat("x", 101), Status: "Planned", Color: "violet"}} {
		if validProject(&in) {
			t.Fatal("invalid project accepted")
		}
	}
}
func TestOriginProtection(t *testing.T) {
	s := &Server{Config: config.Config{Origin: "http://localhost:3000"}}
	handler := s.Security(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		origin, content, site string
		want                  int
	}{{"", "application/json", "", 403}, {"https://evil.test", "application/json", "", 403}, {"http://localhost:3000", "text/plain", "", 415}, {"http://localhost:3000", "application/json", "cross-site", 403}, {"http://localhost:3000", "application/json", "same-origin", 204}} {
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.content)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("want %d got %d", tc.want, w.Code)
		}
	}
}
func TestStrictJSON(t *testing.T) {
	for _, body := range []string{`{"unknown":1}`, `{} {}`, strings.Repeat("x", 20000)} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var in Credentials
		if Decode(w, r, &in) {
			t.Fatal("bad JSON accepted")
		}
	}
}

func TestWorkspaceValidation(t *testing.T) {
	good := store.WorkspaceInput{Name: "  Acme  "}
	if !validWorkspace(&good) || good.Name != "Acme" {
		t.Fatal("valid workspace rejected")
	}
	for _, name := range []string{"", "   ", strings.Repeat("é", 81), "tab\there"} {
		in := store.WorkspaceInput{Name: name}
		if validWorkspace(&in) {
			t.Fatalf("invalid workspace name %q accepted", name)
		}
	}
	if !validWorkspace(&store.WorkspaceInput{Name: strings.Repeat("é", 80)}) {
		t.Fatal("80-character name rejected")
	}
}

// Fake data has to be data the API would accept, or tests exercise states
// real users can't reach.
func TestFactoriesMatchValidation(t *testing.T) {
	f := factory.TestFaker(t)
	for _, p := range factory.Projects.Make(f, 500) {
		in := store.ProjectInput{Name: p.Name, Description: p.Description, Status: p.Status, Color: p.Color}
		if !validProject(&in) || in.Name != p.Name || in.Description != p.Description {
			t.Fatalf("fake project rejected or changed by validation: %+v", p)
		}
	}
	for _, w := range factory.Workspaces.Make(f, 500) {
		in := store.WorkspaceInput{Name: w.Name}
		if !validWorkspace(&in) || in.Name != w.Name {
			t.Fatalf("fake workspace rejected or changed by validation: %+v", w)
		}
	}
	for _, m := range factory.WorkspaceUsers.Make(f, 50) {
		if _, ok := auth.ParseRole(string(m.Role)); !ok {
			t.Fatalf("fake membership has an unknown role: %+v", m)
		}
	}
	for _, i := range factory.WorkspaceInvitations.Make(f, 500) {
		email := i.Email
		if !ValidEmail(&email) || email != i.Email || i.Role == auth.RoleOwner {
			t.Fatalf("fake invitation rejected or changed by validation: %+v", i)
		}
	}
	for _, u := range factory.Users.Make(f, 500) {
		in := Credentials{Name: u.Name, Email: u.Email, Password: factory.DefaultPassword}
		if !ValidCredentials(&in) || in.Email != u.Email || utf8.RuneCountInString(u.Name) > 80 {
			t.Fatalf("fake user rejected or changed by validation: %+v", u)
		}
	}
}
