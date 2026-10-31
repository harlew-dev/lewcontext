package httpapi

import (
	"fmt"
	"lewcontext/api/internal/auth"
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
	// Deleting the account checks the password, like the routes below.
	mux.Handle("DELETE /api/auth/me", s.requireSession(s.limitAuth(http.HandlerFunc(s.destroyAccount))))
	mux.Handle("POST /api/auth/logout", s.requireSession(http.HandlerFunc(s.logout)))
	mux.Handle("DELETE /api/auth/sessions", s.requireSession(http.HandlerFunc(s.revokeOthers)))
	// Password checks hash like sign-in, so they share its IP limit and hashing slots.
	mux.Handle("POST /api/auth/password/verify", s.requireSession(s.limitAuth(http.HandlerFunc(s.verifyPassword))))
	mux.Handle("PATCH /api/auth/password", s.requireSession(s.limitAuth(http.HandlerFunc(s.changePassword))))
	// Password reset by link, signed out (password_reset.go). Requesting a
	// link and checking one don't hash, so they take only the IP quota.
	mux.HandleFunc("POST /api/auth/password-reset", s.requestPasswordReset)
	mux.HandleFunc("POST /api/auth/password-reset/check", s.checkPasswordReset)
	mux.Handle("PATCH /api/auth/password-reset", s.limitAuth(http.HandlerFunc(s.resetPassword)))
	workspaces := workspaceHandlers{s}
	mux.Handle("GET /api/workspaces", s.requireSession(http.HandlerFunc(workspaces.index)))
	mux.Handle("POST /api/workspaces", s.requireSession(http.HandlerFunc(workspaces.store)))

	// Everything inside a workspace requires a session and membership.
	s.workspaceRoutes(group(mux, "/api/workspaces/{workspace}", s.requireSession, s.requireWorkspace))
	return mux
}

// workspaceRoutes are the routes inside a workspace. Reading needs only
// membership; every mutation is registered with Can and names the permission
// it needs (auth/permissions.go), or the server won't start.
func (s *Server) workspaceRoutes(ws routeGroup) {
	ws.requirePermissions = true
	workspaces := workspaceHandlers{s}
	projects := projectHandlers{s}
	members := memberHandlers{s}
	membership := membershipHandlers{s}
	ws.HandleFunc("GET", "", workspaces.show)
	ws.Can("PATCH", "", auth.WorkspaceUpdate, workspaces.update)
	// Deleting checks the owner's password, so it shares sign-in's limits.
	ws.Can("DELETE", "", auth.WorkspaceDestroy, s.limitAuth(http.HandlerFunc(workspaces.destroy)).ServeHTTP)
	ws.HandleFunc("GET", "/projects", projects.index)
	ws.Can("POST", "/projects", auth.ProjectStore, projects.store)
	ws.HandleFunc("GET", "/projects/{project}", projects.show)
	ws.Can("PATCH", "/projects/{project}", auth.ProjectUpdate, projects.update)
	ws.Can("DELETE", "/projects/{project}", auth.ProjectDestroy, projects.destroy)
	ws.Can("GET", "/members", auth.WorkspaceMemberIndex, members.index)
	ws.Can("PATCH", "/members/{member}", auth.WorkspaceMemberUpdate, members.update)
	ws.Can("DELETE", "/members/{member}", auth.WorkspaceMemberDestroy, members.destroy)
	// Your own membership: anyone may leave, apart from the last owner.
	ws.Can("DELETE", "/membership", auth.AnyMember, membership.destroy)
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
	// requirePermissions makes Handle refuse anything but GET and HEAD, so
	// each mutation has to be registered with Can.
	requirePermissions bool
	// registered collects each pattern, when set (the permission matrix test).
	registered *[]string
}

// group starts a route group; the first middleware runs first.
func group(mux *http.ServeMux, prefix string, middleware ...func(http.Handler) http.Handler) routeGroup {
	return routeGroup{mux: mux, prefix: prefix, middleware: middleware}
}
func (g routeGroup) Handle(method, path string, h http.Handler) {
	if g.requirePermissions && method != "GET" && method != "HEAD" {
		panic(fmt.Sprintf("%s %s%s: register it with Can and the permission it needs", method, g.prefix, path))
	}
	g.handle(method, path, h)
}

// Can registers a route only members whose role grants p may use; others get
// 403. auth.AnyMember allows every member.
func (g routeGroup) Can(method, path string, p auth.Permission, h http.HandlerFunc) {
	g.handle(method, path, requirePermission(p, h))
}
func (g routeGroup) handle(method, path string, h http.Handler) {
	for i := len(g.middleware) - 1; i >= 0; i-- {
		h = g.middleware[i](h)
	}
	pattern := method + " " + g.prefix + path
	if g.registered != nil {
		*g.registered = append(*g.registered, pattern)
	}
	g.mux.Handle(pattern, h)
}
func (g routeGroup) HandleFunc(method, path string, h http.HandlerFunc) {
	g.Handle(method, path, h)
}
