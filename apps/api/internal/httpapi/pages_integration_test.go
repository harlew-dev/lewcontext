package httpapi_test

import (
	"context"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"net/http"
	"strings"
	"testing"
)

func TestSignedInPagesNeedASessionBeforeAnyHTML(t *testing.T) {
	s, db := server(t)
	s.Pages = apitest.Pages(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	session := apitest.SignedIn(t, db, f)

	// A valid session gets the app shell; the browser renders the page.
	for _, target := range []string{"/app", "/app/settings", "/app/wsp_2yEHYLwiwPVhLW/projects?page=2"} {
		w := apitest.Get(h, "GET", target, session.Cookie())
		if w.Code != 200 || !strings.Contains(w.Body.String(), "app shell") {
			t.Fatalf("signed in %s: %d %q", target, w.Code, w.Body.String())
		}
	}

	// No cookie, an unknown token, or an idle or expired session go to login
	// and return to the page afterwards. No HTML is sent.
	visitors := map[string]*http.Cookie{"no cookie": nil, "unknown token": factory.Sessions.Make(f, 1)[0].Cookie()}
	for name, state := range map[string]func(*factory.Session){"idle": factory.Idle, "expired": factory.Expired} {
		stale := apitest.Must(factory.Sessions.State(func(s *factory.Session) { s.UserID = session.UserID }).State(state).CreateOne(ctx, db, f))(t)
		visitors[name] = stale.Cookie()
	}
	for name, cookie := range visitors {
		w := apitest.Get(h, "GET", "/app/wsp_2yEHYLwiwPVhLW/projects?page=2", cookie)
		want := "/auth/login?next=%2Fapp%2Fwsp_2yEHYLwiwPVhLW%2Fprojects%3Fpage%3D2"
		if w.Code != http.StatusFound || w.Header().Get("Location") != want {
			t.Errorf("%s: want 302 to %s, got %d %q", name, want, w.Code, w.Header().Get("Location"))
		}
		if strings.Contains(w.Body.String(), "app shell") || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: redirect leaked the shell or is cacheable: %v %q", name, w.Header(), w.Body.String())
		}
	}

	// Public and guest pages need no session.
	for target, want := range map[string]string{"/": "home", "/auth/login?next=%2Fapp": "app shell"} {
		if w := apitest.Get(h, "GET", target, nil); w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("signed out %s: %d %q", target, w.Code, w.Body.String())
		}
	}
}

func TestAPIAndPagesStayApart(t *testing.T) {
	s, db := server(t)
	s.Pages = apitest.Pages(t)
	h := router.New(s)
	session := apitest.SignedIn(t, db, factory.TestFaker(t))

	// Unknown API paths are API 404s, never the app shell.
	for _, target := range []string{"/api/nowhere", "/api/workspaces/wsp_2yEHYLwiwPVhLW/nothing"} {
		w := apitest.Get(h, "GET", target, session.Cookie())
		if w.Code != 404 || strings.Contains(w.Body.String(), "app shell") || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %v %q", target, w.Code, w.Header(), w.Body.String())
		}
	}
	// apitest.Pages only answer GET and HEAD.
	if w := apitest.Get(h, "POST", "/app/settings", session.Cookie()); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /app/settings = %d", w.Code)
	}
	if w := apitest.Get(h, "HEAD", "/app/settings", session.Cookie()); w.Code != 200 {
		t.Errorf("HEAD /app/settings = %d", w.Code)
	}
	// Without a web build the API runs alone.
	s.Pages = nil
	if w := apitest.Get(router.New(s), "GET", "/app/settings", session.Cookie()); w.Code != 404 {
		t.Errorf("API-only GET /app/settings = %d", w.Code)
	}
}
