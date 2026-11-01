package web

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// A build shaped like React Router's: a prerendered homepage, the SPA
// fallback, hashed assets and the build manifest in a dot-directory.
const (
	homeScript     = "window.__reactRouterContext = {};"
	fallbackScript = "window.__reactRouterContext = {\"isSpaMode\":true};"
	moduleScript   = "import(\"/assets/entry.client-abc.js\");"
)

func build() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<!DOCTYPE html><html><head><title>lewcontext</title></head><body><h1>Your work</h1>` +
			`<script src="/assets/entry.client-abc.js" type="module"></script>` +
			`<script>` + homeScript + `</script><script type="module" async="">` + moduleScript + `</script></body></html>`)},
		"__spa-fallback.html": {Data: []byte(`<!DOCTYPE html><html><body><main></main>` +
			`<script>` + fallbackScript + "</script></body></html><script></script>")},
		"assets/entry.client-abc.js": {Data: []byte("console.log(1)")},
		"favicon.svg":                {Data: []byte("<svg></svg>")},
		".vite/manifest.json":        {Data: []byte("{}")},
	}
}

func hash(script string) string {
	sum := sha256.Sum256([]byte(script))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func serve(t *testing.T, p *Pages, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

// The admin build: its shell with its own inline script, hashed assets, the
// build manifest and the favicon React Router copies in.
const adminScript = "window.__reactRouterContext = {\"isSpaMode\":true,\"admin\":true};"

func adminBuild() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte(`<!DOCTYPE html><html><body><main>admin shell</main>` +
			`<script>` + adminScript + `</script></body></html>`)},
		"assets/entry.client-admin.js": {Data: []byte("console.log('admin')")},
		"assets/root-admin.css":        {Data: []byte("body{}")},
		"assets/inter-latin.woff2":     {Data: []byte("wOF2")},
		"favicon.svg":                  {Data: []byte("<svg></svg>")},
		".vite/manifest.json":          {Data: []byte("{}")},
	}
}

func pages(t *testing.T, files fstest.MapFS) *Pages {
	t.Helper()
	p, err := New(files, adminBuild(), false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestServesPrerenderedPagesWithTheirOwnScriptHashes(t *testing.T) {
	w := serve(t, pages(t, build()), "GET", "/")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<h1>Your work</h1>") {
		t.Fatalf("homepage: %d %q", w.Code, w.Body.String())
	}
	csp := w.Header().Get("Content-Security-Policy")
	want := "script-src 'self' " + hash(homeScript) + " " + hash(moduleScript) + ";"
	if !strings.Contains(csp, want) {
		t.Fatalf("CSP %q lacks %q", csp, want)
	}
	if strings.Contains(csp, hash(fallbackScript)) {
		t.Fatalf("homepage CSP allows another file's script: %q", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "object-src 'none'") {
		t.Fatalf("CSP lacks framing/object limits: %q", csp)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("homepage Cache-Control = %q", got)
	}
}

func TestServesTheFallbackForAppPagesAnd404ForUnknownPaths(t *testing.T) {
	p := pages(t, build())
	for target, status := range map[string]int{
		"/app": 200,
		"/app/wsp_2yEHYLwiwPVhLW/projects?page=2": 200,
		"/auth/login":          200,
		"/nowhere":             404,
		"/index.html":          404,
		"/__spa-fallback.html": 404,
		"/.vite/manifest.json": 404,
		"/assets":              404,
	} {
		w := serve(t, p, "GET", target)
		if w.Code != status || !strings.Contains(w.Body.String(), "<main></main>") {
			t.Errorf("%s: want fallback with %d, got %d %q", target, status, w.Code, w.Body.String())
			continue
		}
		// Both inline scripts, including the empty one React Router streams
		// after </html>, are allowed by hash.
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, hash(fallbackScript)) || !strings.Contains(csp, hash("")) {
			t.Errorf("%s: CSP %q lacks the fallback's script hashes", target, csp)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", target, got)
		}
	}
}

func TestCachesHashedAssetsAndRevalidatesOtherFiles(t *testing.T) {
	p := pages(t, build())
	w := serve(t, p, "GET", "/assets/entry.client-abc.js")
	if w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("asset: %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control = %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Fatalf("asset Content-Type = %q", got)
	}
	if w.Header().Get("Content-Security-Policy") != "" {
		t.Fatal("assets don't need a CSP")
	}
	w = serve(t, p, "GET", "/favicon.svg")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("favicon: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestSetsSecurityHeadersEverywhere(t *testing.T) {
	for _, production := range []bool{false, true} {
		p, err := New(build(), adminBuild(), production)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{"/", "/app", "/assets/entry.client-abc.js", "/nowhere"} {
			h := serve(t, p, "GET", target).Header()
			if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" ||
				h.Get("Referrer-Policy") == "" || h.Get("Permissions-Policy") == "" {
				t.Errorf("%s: missing security headers: %v", target, h)
			}
			if hsts := h.Get("Strict-Transport-Security") != ""; hsts != production {
				t.Errorf("%s: HSTS %v in production=%v", target, hsts, production)
			}
		}
	}
}

func TestHeadRequestsGetHeadersOnly(t *testing.T) {
	w := serve(t, pages(t, build()), "HEAD", "/app")
	if w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("HEAD /app: %d %v", w.Code, w.Header())
	}
}

func TestRefusesABuildWithoutTheFallback(t *testing.T) {
	files := build()
	delete(files, "__spa-fallback.html")
	if _, err := New(files, nil, false); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("want ErrNotBuilt, got %v", err)
	}
	// The committed placeholder: no web build at all.
	if _, err := New(fstest.MapFS{".gitkeep": {}}, nil, false); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("want ErrNotBuilt for an empty build, got %v", err)
	}
}

// With admin on (an admin build passed), its shell must be there: the
// committed placeholder alone refuses to start. Admin off needs none.
func TestRefusesAdminWithoutTheAdminBuild(t *testing.T) {
	if _, err := New(build(), fstest.MapFS{".gitkeep": {}}, true); !errors.Is(err, ErrAdminNotBuilt) {
		t.Fatalf("want ErrAdminNotBuilt, got %v", err)
	}
	p, err := New(build(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.AdminBuilt() || !pages(t, build()).AdminBuilt() {
		t.Fatal("AdminBuilt doesn't say whether the admin build is loaded")
	}
	// Admin off: the admin shell and files are the ordinary 404.
	w := httptest.NewRecorder()
	p.ServeAdmin(w, httptest.NewRequest("GET", "/admin/x", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "<main></main>") {
		t.Fatalf("ServeAdmin without an admin build: %d %q", w.Code, w.Body.String())
	}
	if p.ServeAdminFile(httptest.NewRecorder(), httptest.NewRequest("GET", "/admin-assets/x.js", nil), "assets/entry.client-admin.js") {
		t.Fatal("ServeAdminFile served a file without an admin build")
	}
}

// The admin shell is its own build's index.html, never the app's fallback,
// with its own script hashes and the admin headers.
func TestServesTheAdminShellWithItsOwnScriptHashes(t *testing.T) {
	w := httptest.NewRecorder()
	pages(t, build()).ServeAdmin(w, httptest.NewRequest("GET", "/admin/key/users", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "admin shell") || strings.Contains(w.Body.String(), "<main></main>") {
		t.Fatalf("admin shell: %d %q", w.Code, w.Body.String())
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' "+hash(adminScript)+";") {
		t.Fatalf("admin CSP %q lacks exactly the shell's script hash", csp)
	}
	want := map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, nofollow", "X-Frame-Options": "DENY"}
	for header, value := range want {
		if got := w.Header().Get(header); got != value {
			t.Errorf("admin shell %s = %q, want %q", header, got, value)
		}
	}
}

// Admin files are cached by browsers only (private, never public), and
// HTML, dot-files, directories and unknown names are never served.
func TestServesAdminFilesPrivately(t *testing.T) {
	p := pages(t, build())
	for name, body := range map[string]string{
		"assets/entry.client-admin.js": "console.log('admin')",
		"assets/root-admin.css":        "body{}",
		"assets/inter-latin.woff2":     "wOF2",
		"favicon.svg":                  "<svg></svg>",
	} {
		w := httptest.NewRecorder()
		if !p.ServeAdminFile(w, httptest.NewRequest("GET", "/admin-assets/"+name, nil), name) {
			t.Fatalf("%s wasn't served", name)
		}
		if w.Code != 200 || w.Body.String() != body {
			t.Errorf("%s: %d %q", name, w.Code, w.Body.String())
		}
		// For the assets pass's 8 hours, no longer.
		if got := w.Header().Get("Cache-Control"); got != "private, max-age=28800, immutable" {
			t.Errorf("%s Cache-Control = %q", name, got)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s lacks the security headers: %v", name, w.Header())
		}
	}
	for _, name := range []string{"index.html", "./index.html", "assets/../index.html", ".vite/manifest.json", ".vite", "assets", "assets/", "", ".", "/", "../dist/index.html", "assets/missing.js", "missing"} {
		w := httptest.NewRecorder()
		if p.ServeAdminFile(w, httptest.NewRequest("GET", "/admin-assets/x", nil), name) {
			t.Errorf("%q was served: %d %q", name, w.Code, w.Body.String())
		}
		if w.Code != 200 || w.Body.Len() != 0 || len(w.Header()) != 0 {
			t.Errorf("%q wrote a response: %d %v", name, w.Code, w.Header())
		}
	}
}

// The app's side never serves the admin build: /admin-assets/… is the
// fallback 404 there, as is the admin shell's name.
func TestTheAppNeverServesAdminFiles(t *testing.T) {
	p := pages(t, build())
	for _, target := range []string{"/admin-assets/assets/entry.client-admin.js", "/assets/entry.client-admin.js", "/admin-assets/favicon.svg", "/admin"} {
		w := serve(t, p, "GET", target)
		if w.Code != 404 || !strings.Contains(w.Body.String(), "<main></main>") {
			t.Errorf("%s: %d %q", target, w.Code, w.Body.String())
		}
	}
}

func TestMapsPrerenderedFilesToTheirURLs(t *testing.T) {
	for name, want := range map[string]string{
		"index.html":         "/",
		"pricing/index.html": "/pricing",
		"pricing.html":       "/pricing",
		"reindex.html":       "/reindex",
	} {
		if got := urlPath(name); got != want {
			t.Errorf("urlPath(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestHashesScriptsAsBrowsersDo(t *testing.T) {
	// Entities stay literal in scripts and CRLF becomes LF before hashing.
	got, err := scriptHashes([]byte("<script>a &amp;&amp; b\r\nc</script><script src=x></script><SCRIPT>d</SCRIPT>"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{hash("a &amp;&amp; b\nc"), hash("d")}
	if len(got) != len(want) {
		t.Fatalf("got %d hashes, want %d", len(got), len(want))
	}
	for i := range want {
		if "'sha256-"+got[i]+"'" != want[i] {
			t.Errorf("hash %d = %s, want %s", i, got[i], want[i])
		}
	}
	if p := policy(nil); !strings.Contains(p, "script-src 'self';") {
		t.Fatalf("no inline scripts should allow only 'self': %q", p)
	}
}
