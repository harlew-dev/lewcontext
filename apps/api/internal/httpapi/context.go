package httpapi

// What the middleware puts on a request, for controllers to read. The keys
// are unexported, so only this package's middleware can set them.

import (
	"lewcontext/api/internal/store"
	"net/http"
)

type sessionKey struct{}

type workspaceKey struct{}

// unverifiedKey marks a route an unverified account may use while
// EMAIL_VERIFICATION_EXISTING=required (AllowUnverified).
type unverifiedKey struct{}

type adminSessionKey struct{}

func Session(r *http.Request) store.Session { return r.Context().Value(sessionKey{}).(store.Session) }

// OptionalSession is the request's session on routes that may run without
// one (the rate limiters).
func OptionalSession(r *http.Request) (store.Session, bool) {
	session, ok := r.Context().Value(sessionKey{}).(store.Session)
	return session, ok
}

// Workspace is the workspace in the path, set by RequireWorkspace.
func Workspace(r *http.Request) store.Workspace {
	return r.Context().Value(workspaceKey{}).(store.Workspace)
}

// OptionalWorkspace is the workspace in the path on routes that may run
// without one (the rate limiters).
func OptionalWorkspace(r *http.Request) (store.Workspace, bool) {
	workspace, ok := r.Context().Value(workspaceKey{}).(store.Workspace)
	return workspace, ok
}

// Admin is the signed-in admin's session, set by RequireAdminSession.
func Admin(r *http.Request) store.AdminSession {
	return r.Context().Value(adminSessionKey{}).(store.AdminSession)
}

// OptionalAdmin is the request's admin session on routes that may run
// without one (the rate limiters).
func OptionalAdmin(r *http.Request) (store.AdminSession, bool) {
	session, ok := r.Context().Value(adminSessionKey{}).(store.AdminSession)
	return session, ok
}
