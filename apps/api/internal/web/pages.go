package web

import (
	"errors"
	"fmt"
	"io/fs"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/observe"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// fallbackFile renders only the app's root; it loads the page for the URL in
// the browser. React Router writes it when "/" is prerendered.
const fallbackFile = "__spa-fallback.html"

// adminShellFile is the admin build's one HTML file (it prerenders nothing).
const adminShellFile = "index.html"

// ErrNotBuilt means the embedded files have no web build (no `make build`).
var ErrNotBuilt = errors.New("web app not built: run make build")

// ErrAdminNotBuilt means ADMIN_ROUTE_KEY is set but the admin build isn't
// embedded beside the app's.
var ErrAdminNotBuilt = errors.New("admin dashboard not built (ADMIN_ROUTE_KEY is set): run make build")

// Pages serves the web build. Every HTML file's CSP is computed once, in New.
type Pages struct {
	files       fs.FS
	prerendered map[string]page // by URL path, e.g. "/" for index.html
	fallback    page
	// adminFiles and adminShell are the admin build, when admin is on: never
	// served from files, only by ServeAdmin and ServeAdminFile.
	adminFiles fs.FS
	adminShell page
	production bool
}

type page struct {
	body   []byte
	policy string
	// route is a prerendered page's access-line label, "page /" for "/".
	route string
}

// The access line's labels for what Pages serves (observe.SetRoute): fixed,
// never the path, which may hold a link's token or the admin key.
const (
	routeAsset      = "asset"
	routeAdminAsset = "admin asset"
	routeAdminPage  = "page /admin/*"
	routeAppPage    = "page /app/*"
	routeAuthPage   = "page /auth/*"
	routeNotFound   = "page 404"
)

// New reads the build's HTML files and hashes their inline scripts. It fails
// if the fallback page is missing or an HTML file can't be read, so a broken
// build never serves a policy that blocks the app. admin is the admin build
// when admin is on (ADMIN_ROUTE_KEY set), or nil; without its shell New
// fails with ErrAdminNotBuilt.
func New(files, admin fs.FS, production bool) (*Pages, error) {
	p := &Pages{files: files, prerendered: map[string]page{}, production: production}
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Ext(name) != ".html" {
			return err
		}
		loaded, err := load(files, name)
		if err != nil {
			return err
		}
		if name == fallbackFile {
			p.fallback = loaded
		} else {
			loaded.route = "page " + urlPath(name)
			p.prerendered[urlPath(name)] = loaded
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if p.fallback.body == nil {
		return nil, ErrNotBuilt
	}
	if admin != nil {
		shell, err := load(admin, adminShellFile)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrAdminNotBuilt
		}
		if err != nil {
			return nil, err
		}
		p.adminFiles, p.adminShell = admin, shell
	}
	return p, nil
}

func load(files fs.FS, name string) (page, error) {
	body, err := fs.ReadFile(files, name)
	if err != nil {
		return page{}, err
	}
	hashes, err := scriptHashes(body)
	if err != nil {
		return page{}, fmt.Errorf("read inline scripts of %s: %w", name, err)
	}
	return page{body: body, policy: policy(hashes)}, nil
}

// urlPath is the URL a prerendered file answers: "index.html" is "/",
// "about/index.html" and "about.html" are "/about".
func urlPath(name string) string {
	if path.Base(name) == "index.html" {
		name = path.Dir(name)
	} else {
		name = strings.TrimSuffix(name, ".html")
	}
	if name == "." {
		return "/"
	}
	return "/" + name
}

// ServeHTTP answers GET and HEAD: a prerendered page, a built file, or the
// fallback page, which renders the app's own not-found page for unknown URLs
// (with status 404).
func (p *Pages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.securityHeaders(w)
	if prerendered, ok := p.prerendered[r.URL.Path]; ok {
		observe.SetRoute(r.Context(), prerendered.route)
		// No user data is built into these, but they change with each deploy.
		p.serveHTML(w, prerendered, http.StatusOK, "no-cache")
		return
	}
	if p.serveFile(w, r) {
		observe.SetRoute(r.Context(), routeAsset)
		return
	}
	status, route := http.StatusOK, routeAuthPage
	switch {
	case r.URL.Path == "/app" || strings.HasPrefix(r.URL.Path, "/app/"):
		route = routeAppPage
	case !appPath(r.URL.Path):
		status, route = http.StatusNotFound, routeNotFound
	}
	observe.SetRoute(r.Context(), route)
	p.serveHTML(w, p.fallback, status, "no-store")
}

// AdminBuilt reports whether the admin build is loaded (admin on, and New
// found its shell), so ServeAdmin serves it rather than the 404.
func (p *Pages) AdminBuilt() bool {
	return p.adminFiles != nil
}

// ServeAdmin answers an admin page (/admin/<key>/…), once the server has
// checked the key: the admin build's shell with status 200, which renders the
// admin pages, never the app's fallback. The key is in the path, so no
// Referer leaves with it, and nothing is cached or indexed. Without an admin
// build it is the ordinary 404.
func (p *Pages) ServeAdmin(w http.ResponseWriter, r *http.Request) {
	if p.adminFiles == nil {
		p.ServeHTTP(w, r)
		return
	}
	observe.SetRoute(r.Context(), routeAdminPage)
	p.securityHeaders(w)
	adminHeaders(w)
	p.serveHTML(w, p.adminShell, http.StatusOK, "no-store")
}

// ServeAdminFile serves one of the admin build's files (name, as under
// /admin-assets/), once the server has checked the assets pass, and reports
// whether it did. HTML, dot-files, directories and unknown names aren't
// served: the caller answers those as any unknown URL. The names are
// content-hashed, so they never change, but browsers keep them only as long
// as the pass that opened them lasts, and only browsers: `private`, never
// `public`, or a shared cache (Cloudflare, a proxy) would hand them to anyone
// without a pass.
func (p *Pages) ServeAdminFile(w http.ResponseWriter, r *http.Request, name string) bool {
	if p.adminFiles == nil {
		return false
	}
	name = path.Clean(name)
	if !fs.ValidPath(name) || name == "." || path.Ext(name) == ".html" || hidden(name) {
		return false
	}
	info, err := fs.Stat(p.adminFiles, name)
	if err != nil || info.IsDir() {
		return false
	}
	observe.SetRoute(r.Context(), routeAdminAsset)
	p.securityHeaders(w)
	adminHeaders(w)
	w.Header().Set("Cache-Control", adminFileCaching)
	http.ServeFileFS(w, r, p.adminFiles, name)
	return true
}

// adminFileCaching keeps an admin file in the browser for the assets pass's
// lifetime, no longer.
var adminFileCaching = "private, max-age=" + strconv.Itoa(int(auth.AdminAssetsPassLifetime.Seconds())) + ", immutable"

// adminHeaders go with every admin page and file: nothing sends a Referer
// from them, and nothing is indexed.
func adminHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

// appPath reports whether a URL may be one of the app's pages; the browser
// decides which (src/routes.ts). Anything else is a 404.
func appPath(urlPath string) bool {
	return urlPath == "/app" || strings.HasPrefix(urlPath, "/app/") || strings.HasPrefix(urlPath, "/auth/")
}

func (p *Pages) serveHTML(w http.ResponseWriter, html page, status int, cache string) {
	w.Header().Set("Content-Security-Policy", html.policy)
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(html.body)))
	w.WriteHeader(status)
	_, _ = w.Write(html.body)
}

// serveFile serves a built file other than HTML: hashed assets cached for a
// year, anything else (the favicon) revalidated. Dot-files and directories
// are never served.
func (p *Pages) serveFile(w http.ResponseWriter, r *http.Request) bool {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || path.Ext(name) == ".html" || hidden(name) {
		return false
	}
	info, err := fs.Stat(p.files, name)
	if err != nil || info.IsDir() {
		return false
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, p.files, name)
	return true
}

func hidden(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// securityHeaders apply to every page and file. The API sets its own.
func (p *Pages) securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if p.production {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
}
