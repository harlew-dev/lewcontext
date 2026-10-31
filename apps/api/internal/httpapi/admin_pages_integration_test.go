package httpapi

import (
	"context"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// A wrong key, or admin off, gets exactly what an unknown URL gets; the right
// one the app shell with the admin pages' stricter headers.
func TestAdminPages(t *testing.T) {
	server, db := integrationServer(t)
	server.Pages = testPages(t)
	h := server.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	app := signedIn(t, db, f)
	off := must(New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000", AppKey: factory.AppKey}))(t)
	off.Pages = server.Pages
	key := factory.AdminRouteKey

	nowhere := get(h, "GET", "/nowhere", nil)
	if nowhere.Code != 404 {
		t.Fatalf("/nowhere: %d", nowhere.Code)
	}
	wrong := []string{"/admin", "/admin/", "/admin/x", "/admin/x/users", "/admin/" + key + "x", "/admin/" + strings.ToUpper(key) + "/users", "/admin/" + key[:len(key)-1]}
	for _, target := range wrong {
		sameAnswer(t, target, get(h, "GET", target, session.Cookie()), nowhere)
		sameAnswer(t, target+" (admin off)", get(off.Routes(), "GET", target, session.Cookie()), nowhere)
	}
	for _, target := range []string{"/admin/" + key, "/admin/" + key + "/users"} {
		sameAnswer(t, target+" (admin off)", get(off.Routes(), "GET", target, session.Cookie()), nowhere)
	}

	// The sign-in page needs no session.
	w := get(h, "GET", "/admin/"+key, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "app shell") {
		t.Fatalf("sign-in page: %d", w.Code)
	}
	checkAdminHeaders(t, "sign-in page", w)

	// Inner pages need an admin session: an app session doesn't count.
	for _, cookie := range []*http.Cookie{nil, app.Cookie()} {
		w := get(h, "GET", "/admin/"+key+"/users/usr_2yEHYLwiwPVhLW", cookie)
		if w.Code != 302 || w.Header().Get("Location") != "/admin/"+key || strings.Contains(w.Body.String(), "app shell") {
			t.Fatalf("inner page without an admin session: %d %v", w.Code, w.Header())
		}
		checkAdminHeaders(t, "redirect", w)
	}
	stale := must(factory.AdminSessions.State(factory.AdminSessionFor(session.UserID)).State(factory.AdminIdle).CreateOne(ctx, db, f))(t)
	if w := get(h, "GET", "/admin/"+key+"/users", stale.Cookie()); w.Code != 302 {
		t.Fatalf("an idle admin session: %d", w.Code)
	}
	w = get(h, "GET", "/admin/"+key+"/workspaces", session.Cookie())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "app shell") {
		t.Fatalf("inner page with an admin session: %d", w.Code)
	}
	checkAdminHeaders(t, "inner page", w)
}

func checkAdminHeaders(t *testing.T, name string, w *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{"Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, nofollow", "Cache-Control": "no-store"}
	for header, value := range want {
		if got := w.Header().Get(header); got != value {
			t.Errorf("%s: %s is %q, want %q", name, header, got, value)
		}
	}
}

// Every /api/admin/ route is registered in adminRoutes, behind
// requireAdminKey: a route added anywhere else in routes.go fails here.
func TestAdminRoutesStayInTheAdminGroup(t *testing.T) {
	source := string(must(os.ReadFile("routes.go"))(t))
	start := strings.Index(source, "func (s *Server) adminRoutes(")
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
		if strings.Contains(line, "mux.Handle") && !strings.Contains(line, "requireAdminKey") {
			t.Fatalf("an admin route without requireAdminKey: %s", strings.TrimSpace(line))
		}
	}
}
