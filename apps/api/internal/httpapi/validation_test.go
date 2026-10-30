package httpapi

import (
	"forma/api/internal/config"
	"forma/api/internal/factory"
	"forma/api/internal/store"
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
	}{{" HELLO@example.com ", "a long safe passphrase", true}, {"Display <a@example.com>", "a long safe passphrase", false}, {"bad", "a long safe passphrase", false}, {"a@example.com", "short", false}, {"a@example.com", strings.Repeat("x", 129), false}} {
		in := credentials{Email: tc.email, Password: tc.password}
		if validCredentials(&in) != tc.want {
			t.Errorf("unexpected validation for %s", tc.email)
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
	handler := s.security(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
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
		var in credentials
		if decode(w, r, &in) {
			t.Fatal("bad JSON accepted")
		}
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
	for _, u := range factory.Users.Make(f, 500) {
		in := credentials{Name: u.Name, Email: u.Email, Password: factory.DefaultPassword}
		if !validCredentials(&in) || in.Email != u.Email || utf8.RuneCountInString(u.Name) > 80 {
			t.Fatalf("fake user rejected or changed by validation: %+v", u)
		}
	}
}
