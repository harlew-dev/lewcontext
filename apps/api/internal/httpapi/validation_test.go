package httpapi

import (
	"lewcontext/api/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
