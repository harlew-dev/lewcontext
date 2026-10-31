package httpapi

import (
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
)

// Routes is the single registry of API endpoints and their authorization
// requirements, and of the web app's pages when Pages is set.
func (s *Server) Routes() http.Handler {
	api := s.security(s.apiRoutes())
	pages := s.pageRoutes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		pages.ServeHTTP(w, r)
	})
}

// apiRoutes are the JSON endpoints under /api.
func (s *Server) apiRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.Handle("POST /api/auth/register", s.limitAuth(http.HandlerFunc(s.register)))
	mux.Handle("POST /api/auth/login", s.limitAuth(http.HandlerFunc(s.login)))
	mux.Handle("GET /api/auth/me", s.requireSession(http.HandlerFunc(s.me)))
	mux.Handle("PATCH /api/auth/me", s.requireSession(http.HandlerFunc(s.updateProfile)))
	mux.Handle("POST /api/auth/logout", s.requireSession(http.HandlerFunc(s.logout)))
	mux.Handle("DELETE /api/auth/sessions", s.requireSession(http.HandlerFunc(s.revokeOthers)))
	// Password checks hash like sign-in, so they share its IP limit and hashing slots.
	mux.Handle("POST /api/auth/password/verify", s.requireSession(s.limitAuth(http.HandlerFunc(s.verifyPassword))))
	mux.Handle("PUT /api/auth/password", s.requireSession(s.limitAuth(http.HandlerFunc(s.changePassword))))
	mux.Handle("GET /api/workspaces", s.requireSession(http.HandlerFunc(s.workspaces)))
	mux.Handle("POST /api/workspaces", s.requireSession(http.HandlerFunc(s.createWorkspace)))

	// Everything inside a workspace requires a session and membership.
	ws := group(mux, "/api/workspaces/{workspace}", s.requireSession, s.requireWorkspace)
	ws.HandleFunc("GET", "", s.workspace)
	ws.Handle("PATCH", "", requireRole(store.RoleAdmin, http.HandlerFunc(s.updateWorkspace)))
	ws.HandleFunc("GET", "/projects", s.projects)
	ws.HandleFunc("POST", "/projects", s.createProject)
	ws.HandleFunc("GET", "/projects/{project}", s.project)
	ws.HandleFunc("PATCH", "/projects/{project}", s.updateProject)
	return mux
}

// pageRoutes serve the built web app (internal/web) for every other GET.
// Signed-in pages (/app/*) need a session before any HTML is sent; the app
// itself decides which page a URL is (apps/web/src/routes.ts).
func (s *Server) pageRoutes() http.Handler {
	mux := http.NewServeMux()
	if s.Pages == nil {
		return mux // API only: in development Vite serves the pages
	}
	mux.Handle("GET /", s.Pages)
	mux.Handle("GET /app", s.requirePageSession(s.Pages))
	mux.Handle("GET /app/", s.requirePageSession(s.Pages))
	return mux
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DB.PingContext(r.Context()); err != nil {
		fail(w, 503, "Database unavailable")
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "ok"})
}

// routeGroup registers routes under a shared path prefix, wrapped in shared
// middleware. net/http has no groups, and http.StripPrefix can't strip a
// prefix with a {wildcard}, so the group prefixes each pattern instead.
type routeGroup struct {
	mux        *http.ServeMux
	prefix     string
	middleware []func(http.Handler) http.Handler
}

// group starts a route group; the first middleware runs first.
func group(mux *http.ServeMux, prefix string, middleware ...func(http.Handler) http.Handler) routeGroup {
	return routeGroup{mux: mux, prefix: prefix, middleware: middleware}
}
func (g routeGroup) Handle(method, path string, h http.Handler) {
	for i := len(g.middleware) - 1; i >= 0; i-- {
		h = g.middleware[i](h)
	}
	g.mux.Handle(method+" "+g.prefix+path, h)
}
func (g routeGroup) HandleFunc(method, path string, h http.HandlerFunc) {
	g.Handle(method, path, h)
}
