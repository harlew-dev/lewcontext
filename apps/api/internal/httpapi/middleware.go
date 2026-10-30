package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"forma/api/internal/auth"
	"forma/api/internal/store"
	"mime"
	"net"
	"net/http"
	"time"
)

type sessionKey struct{}
type workspaceKey struct{}

func current(r *http.Request) store.Session { return r.Context().Value(sessionKey{}).(store.Session) }

// currentWorkspace is the workspace in the path, set by requireWorkspace.
func currentWorkspace(r *http.Request) store.Workspace {
	return r.Context().Value(workspaceKey{}).(store.Workspace)
}
func unsafeMethod(method string) bool {
	return method != "GET" && method != "HEAD" && method != "OPTIONS"
}
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if unsafeMethod(r.Method) {
			if r.Header.Get("Origin") != s.Config.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "Request origin is not allowed.")
				return
			}
			if r.Method != "DELETE" {
				media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || media != "application/json" {
					fail(w, 415, "Content-Type must be application/json.")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.CookieName(s.Config.Production))
		if err != nil || len(cookie.Value) != 43 {
			fail(w, 401, "Please sign in to continue.")
			return
		}
		session, err := s.Store.Session(r.Context(), auth.Digest(cookie.Value))
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 401, "Your session has expired. Please sign in again.")
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		if unsafeMethod(r.Method) && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			fail(w, 403, "Invalid CSRF token. Refresh and try again.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
	})
}

// requireWorkspace runs after requireSession. It loads the {workspace} in the
// path through the signed-in user's membership; a malformed ID, a missing
// workspace and someone else's workspace all get the same 404.
func (s *Server) requireWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("workspace")
		if !validPublicID("wsp", id) {
			fail(w, 404, "Workspace not found.")
			return
		}
		workspace, err := s.Store.MemberWorkspace(r.Context(), current(r).User.ID, id)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "Workspace not found.")
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), workspaceKey{}, workspace)))
	})
}

// requireRole runs after requireWorkspace and refuses members whose role is
// below min.
func requireRole(min string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !store.RoleAtLeast(currentWorkspace(r).Role, min) {
			fail(w, 403, "You don’t have permission to do that in this workspace.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) limitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		allowed, err := s.Store.AllowAuth(r.Context(), "ip:"+auth.Digest(ip), 60)
		if err != nil {
			internalError(w, err)
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", "900")
			fail(w, 429, "Too many attempts. Try again in 15 minutes.")
			return
		}
		select {
		case s.authSlots <- struct{}{}:
			defer func() { <-s.authSlots }()
		default:
			fail(w, 503, "Sign-in is busy. Please try again shortly.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
