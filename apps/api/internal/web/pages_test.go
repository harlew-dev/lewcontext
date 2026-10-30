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
		"index.html": {Data: []byte(`<!DOCTYPE html><html><head><title>Forma</title></head><body><h1>Your work</h1>` +
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

func pages(t *testing.T, files fstest.MapFS) *Pages {
	t.Helper()
	p, err := New(files, false)
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
		p, err := New(build(), production)
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
	if _, err := New(files, false); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("want ErrNotBuilt, got %v", err)
	}
	// The committed placeholder: no web build at all.
	if _, err := New(fstest.MapFS{".gitkeep": {}}, false); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("want ErrNotBuilt for an empty build, got %v", err)
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
