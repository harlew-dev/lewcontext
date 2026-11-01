package admin_test

import (
	"context"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"lewcontext/api/internal/web"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// A wrong key, or admin off, gets exactly what an unknown URL gets; the right
// one the app shell with the admin pages' stricter headers.
func TestAdminPages(t *testing.T) {
	s, db := server(t)
	s.Pages = apitest.Pages(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	app := apitest.SignedIn(t, db, f)
	off := apitest.Must(httpapi.New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000", AppKey: factory.AppKey}))(t)
	off.Pages = s.Pages
	key := factory.AdminRouteKey

	nowhere := apitest.Get(h, "GET", "/nowhere", nil)
	if nowhere.Code != 404 {
		t.Fatalf("/nowhere: %d", nowhere.Code)
	}
	wrong := []string{"/admin", "/admin/", "/admin/x", "/admin/x/users", "/admin/" + key + "x", "/admin/" + strings.ToUpper(key) + "/users", "/admin/" + key[:len(key)-1]}
	for _, target := range wrong {
		sameAnswer(t, target, apitest.Get(h, "GET", target, session.Cookie()), nowhere)
		sameAnswer(t, target+" (admin off)", apitest.Get(router.New(off), "GET", target, session.Cookie()), nowhere)
	}
	for _, target := range []string{"/admin/" + key, "/admin/" + key + "/users"} {
		sameAnswer(t, target+" (admin off)", apitest.Get(router.New(off), "GET", target, session.Cookie()), nowhere)
	}

	// The sign-in page needs no session. It is the admin build's shell,
	// never the app's, and hands out the assets pass.
	w := apitest.Get(h, "GET", "/admin/"+key, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "admin shell") || strings.Contains(w.Body.String(), "app shell") {
		t.Fatalf("sign-in page: %d %q", w.Code, w.Body.String())
	}
	checkAdminHeaders(t, "sign-in page", w)
	checkAssetsPass(t, "sign-in page", w, s)

	// Inner pages need an admin session: an app session doesn't count. A
	// redirect sets no pass.
	for _, cookie := range []*http.Cookie{nil, app.Cookie()} {
		w := apitest.Get(h, "GET", "/admin/"+key+"/users/usr_2yEHYLwiwPVhLW", cookie)
		if w.Code != 302 || w.Header().Get("Location") != "/admin/"+key || strings.Contains(w.Body.String(), "shell") {
			t.Fatalf("inner page without an admin session: %d %v", w.Code, w.Header())
		}
		checkAdminHeaders(t, "redirect", w)
		if w.Header().Get("Set-Cookie") != "" {
			t.Fatalf("a redirect set a cookie: %v", w.Header())
		}
	}
	stale := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(session.UserID)).State(factory.AdminIdle).CreateOne(ctx, db, f))(t)
	if w := apitest.Get(h, "GET", "/admin/"+key+"/users", stale.Cookie()); w.Code != 302 || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("an idle admin session: %d %v", w.Code, w.Header())
	}
	w = apitest.Get(h, "GET", "/admin/"+key+"/workspaces", session.Cookie())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "admin shell") {
		t.Fatalf("inner page with an admin session: %d", w.Code)
	}
	checkAdminHeaders(t, "inner page", w)
	// Each admin page renews the pass.
	checkAssetsPass(t, "inner page", w, s)
}

// checkAssetsPass checks that an admin page set a valid assets pass, for
// the current key, with its cookie's attributes.
func checkAssetsPass(t *testing.T, name string, w *httptest.ResponseRecorder, s *httpapi.Server) {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("%s: want one cookie, the pass: %v", name, cookies)
	}
	c := cookies[0]
	if c.Name != "lewcontext_admin_assets" || c.Path != "/admin-assets/" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge != 8*60*60 {
		t.Fatalf("%s: pass cookie %+v", name, c)
	}
	if !factory.Keys.AdminAssetsPassValid(c.Value, s.AdminKeyHash(), time.Now()) {
		t.Fatalf("%s: the pass %q isn't valid for the current key", name, c.Value)
	}
}

// passFrom requests the admin sign-in page and returns the pass it sets.
func passFrom(t *testing.T, h http.Handler, key string) *http.Cookie {
	t.Helper()
	w := apitest.Get(h, "GET", "/admin/"+key, nil)
	cookie := apitest.CookieNamed(w, "lewcontext_admin_assets")
	if w.Code != 200 || cookie == nil {
		t.Fatalf("no pass from /admin/<key>: %d %v", w.Code, w.Header())
	}
	return cookie
}

// The admin build's files open only with a valid assets pass, privately
// cached; anything else is exactly what an unknown URL gets, whether or not
// the file exists, and admin files are never in the app's own paths.
func TestAdminAssets(t *testing.T) {
	s, db := server(t)
	s.Pages = apitest.Pages(t)
	h := router.New(s)
	key := factory.AdminRouteKey
	nowhere := apitest.Get(h, "GET", "/nowhere", nil)
	nowhereHead := apitest.Get(h, "HEAD", "/nowhere", nil)
	pass := passFrom(t, h, key)

	w := apitest.Get(h, "GET", apitest.AdminAsset, pass)
	if w.Code != 200 || w.Body.String() != apitest.AdminAssetBody {
		t.Fatalf("an admin file with a pass: %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "private, max-age=28800, immutable" {
		t.Fatalf("admin file Cache-Control = %q", got)
	}
	if w.Header().Get("Set-Cookie") != "" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("admin file headers: %v", w.Header())
	}
	if w := apitest.Get(h, "HEAD", apitest.AdminAsset, pass); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD with a pass: %d", w.Code)
	}

	// Passes that don't open anything: none, expired, made under the
	// previous key (rotated since), under another APP_KEY, tampered with,
	// or for another cookie name.
	expired := auth.AdminAssetsCookie(factory.Keys.AdminAssetsPass(s.AdminKeyHash(), time.Now().Add(-time.Second)), false)
	tooLong := auth.AdminAssetsCookie(factory.Keys.AdminAssetsPass(s.AdminKeyHash(), time.Now().Add(30*24*time.Hour)), false)
	previous := apitest.Config()
	previous.AdminRouteKey = "previous-" + key
	oldServer := apitest.Must(httpapi.New(&store.Store{DB: db}, previous))(t)
	oldServer.Pages = s.Pages
	underOldKey := passFrom(t, router.New(oldServer), previous.AdminRouteKey)
	otherAppKey := apitest.Config()
	otherAppKey.AppKey = []byte(strings.Repeat("k", 32))
	otherServer := apitest.Must(httpapi.New(&store.Store{DB: db}, otherAppKey))(t)
	otherServer.Pages = s.Pages
	underOtherAppKey := passFrom(t, router.New(otherServer), key)
	// The MAC's first character changed.
	tampered := *pass
	expiry, mac, _ := strings.Cut(pass.Value, ".")
	swap := "A"
	if mac[0] == 'A' {
		swap = "B"
	}
	tampered.Value = expiry + "." + swap + mac[1:]
	renamed := *pass
	renamed.Name = "lewcontext_admin"
	refused := map[string]*http.Cookie{
		"no pass": nil, "expired": expired, "too long": tooLong, "the previous key's": underOldKey,
		"another APP_KEY's": underOtherAppKey, "tampered": &tampered, "renamed": &renamed,
	}
	for name, cookie := range refused {
		sameAnswer(t, name, apitest.Get(h, "GET", apitest.AdminAsset, cookie), nowhere)
		sameAnswer(t, name+" (HEAD)", apitest.Get(h, "HEAD", apitest.AdminAsset, cookie), nowhereHead)
		sameAnswer(t, name+" (unknown file)", apitest.Get(h, "GET", "/admin-assets/assets/missing.js", cookie), nowhere)
	}

	// With a valid pass: unknown files, HTML (the shell itself), dot-files
	// and directories are unknown URLs too.
	for _, target := range []string{
		"/admin-assets/assets/missing.js", "/admin-assets/index.html", "/admin-assets/.vite/manifest.json",
		"/admin-assets/assets", "/admin-assets/assets/", "/admin-assets/", "/admin-assets",
	} {
		sameAnswer(t, target, apitest.Get(h, "GET", target, pass), nowhere)
	}
	// Conditional and range requests without a pass are refused alike.
	for name, header := range map[string][2]string{
		"Range":         {"Range", "bytes=0-3"},
		"If-None-Match": {"If-None-Match", `"x"`},
		"If-Range":      {"If-Range", `"x"`},
	} {
		sameAnswer(t, name, getWith(h, "GET", apitest.AdminAsset, header), getWith(h, "GET", "/nowhere", header))
	}
	// A cookie of the same name planted by a sibling subdomain may come
	// first: a valid pass behind it still opens the file, among the first
	// four of that name, never further back.
	junk := &http.Cookie{Name: pass.Name, Value: "1." + strings.Repeat("A", 43)}
	if w := getWith(h, "GET", apitest.AdminAsset, [2]string{}, junk, pass); w.Code != 200 || w.Body.String() != apitest.AdminAssetBody {
		t.Fatalf("a valid pass behind a planted one: %d", w.Code)
	}
	if w := getWith(h, "GET", apitest.AdminAsset, [2]string{}, junk, junk, junk, pass); w.Code != 200 {
		t.Fatalf("a valid pass fourth: %d", w.Code)
	}
	sameAnswer(t, "a valid pass fifth", getWith(h, "GET", apitest.AdminAsset, [2]string{}, junk, junk, junk, junk, pass), nowhere)

	// Other methods are refused as anywhere else, pass or not.
	for _, cookie := range []*http.Cookie{nil, pass} {
		sameAnswer(t, "POST", apitest.Get(h, "POST", apitest.AdminAsset, cookie), apitest.Get(h, "POST", "/nowhere", nil))
	}
	// The app's paths never reach the admin build.
	for _, target := range []string{"/assets/admin-abc.js", "/index.html", "/admin-assets/../assets/admin-abc.js"} {
		w := apitest.Get(h, "GET", target, pass)
		if w.Code == 200 || strings.Contains(w.Body.String(), apitest.AdminAssetBody) {
			t.Errorf("%s: %d %q", target, w.Code, w.Body.String())
		}
	}

	// Admin off: even a pass that would be valid opens nothing.
	off := apitest.Must(httpapi.New(&store.Store{DB: db}, config.Config{Origin: apitest.Origin, AppKey: factory.AppKey}))(t)
	off.Pages = s.Pages
	sameAnswer(t, "admin off", apitest.Get(router.New(off), "GET", apitest.AdminAsset, pass), apitest.Get(router.New(off), "GET", "/nowhere", nil))
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

// getWith requests target with one header (when given) and the cookies, in
// order.
func getWith(h http.Handler, method, target string, header [2]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if header[0] != "" {
		r.Header.Set(header[0], header[1])
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// A web build without its admin build (only possible in tests: such a
// server refuses to start) serves no admin page and sets no pass: the right
// key gets the ordinary 404.
func TestAdminPagesWithoutAnAdminBuild(t *testing.T) {
	s, db := server(t)
	pages, err := web.New(fstest.MapFS{
		"index.html":          {Data: []byte("<html><body>home</body></html>")},
		"__spa-fallback.html": {Data: []byte("<html><body>app shell</body></html>")},
	}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	s.Pages = pages
	h := router.New(s)
	_, session := adminSignedIn(t, db, factory.TestFaker(t))
	nowhere := apitest.Get(h, "GET", "/nowhere", nil)
	for _, target := range []string{"/admin/" + factory.AdminRouteKey, "/admin/" + factory.AdminRouteKey + "/users"} {
		sameAnswer(t, target, apitest.Get(h, "GET", target, session.Cookie()), nowhere)
	}
}
