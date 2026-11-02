package apitest

import (
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/web"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// NewPassword is a strong password tests change or reset to.
const NewPassword = "a brand new passphrase"

// LoginBody signs in as email with the factories' password.
func LoginBody(email string) string {
	return fmt.Sprintf(`{"email":%q,"password":%q}`, email, factory.DefaultPassword)
}

// CodeBody sends a code: a second factor or an emailed code.
func CodeBody(code string) string {
	return fmt.Sprintf(`{"code":%q}`, code)
}

// TokenBody sends an emailed link's token: a password reset or an invitation.
func TokenBody(token string) string {
	return fmt.Sprintf(`{"token":%q}`, token)
}

// ChangeBody changes the password from current to next.
func ChangeBody(current, next string) string {
	return fmt.Sprintf(`{"currentPassword":%q,"newPassword":%q}`, current, next)
}

// ErrorField reads the field an error response is about.
func ErrorField(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Field string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Field
}

// CookieNamed is the response's cookie called name, or nil.
func CookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Get requests a page as a browser would, with only the cookie.
func Get(h http.Handler, method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// AdminAsset is the one script in Pages' admin build, as served under
// /admin-assets/, and AdminAssetBody what it holds.
const (
	AdminAsset     = "/admin-assets/assets/admin-abc.js"
	AdminAssetBody = "console.log('admin')"
)

// Pages is a minimal web build: the homepage and the SPA fallback, and an
// admin build with its shell, a hashed script, a dot-file and the build's
// favicon.
func Pages(t *testing.T) *web.Pages {
	t.Helper()
	pages, err := web.New(fstest.MapFS{
		"index.html":          {Data: []byte("<html><body>home</body></html>")},
		"__spa-fallback.html": {Data: []byte("<html><body>app shell</body></html>")},
	}, fstest.MapFS{
		"index.html":          {Data: []byte("<html><body>admin shell</body></html>")},
		"assets/admin-abc.js": {Data: []byte(AdminAssetBody)},
		".vite/manifest.json": {Data: []byte("{}")},
		"favicon.svg":         {Data: []byte("<svg></svg>")},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	return pages
}
