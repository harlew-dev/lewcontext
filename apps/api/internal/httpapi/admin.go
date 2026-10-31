package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"time"
)

// The admin dashboard (docs/api/admin.md): behind the secret ADMIN_ROUTE_KEY
// on every call (X-Admin-Key), then an admin session of its own. It reads
// across every workspace, so nothing here is scoped by membership.

type adminSessionKey struct{}

// currentAdmin is the signed-in admin's session, set by requireAdminSession.
func currentAdmin(r *http.Request) store.AdminSession {
	return r.Context().Value(adminSessionKey{}).(store.AdminSession)
}

// optionalAdmin is the request's admin session on routes that may run
// without one (the rate limiters).
func optionalAdmin(r *http.Request) (store.AdminSession, bool) {
	session, ok := r.Context().Value(adminSessionKey{}).(store.AdminSession)
	return session, ok
}

// notFound is the answer to an unknown API route, and to every admin request
// without the right key, so the two can't be told apart.
func notFound(w http.ResponseWriter, r *http.Request) {
	fail(w, 404, "Not found.")
}

// adminKeyHash is the digest of the current ADMIN_ROUTE_KEY, which each admin
// session stores: rotating the key ends them all.
func (s *Server) adminKeyHash() string {
	return auth.Digest(s.Config.AdminRouteKey)
}

// requireAdminKey answers every /api/admin/ request whose X-Admin-Key isn't
// the configured key (or with admin off) exactly as an unknown API route,
// before anything else is looked at.
func (s *Server) requireAdminKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.AdminKeyMatches(r.Header.Get("X-Admin-Key"), s.Config.AdminRouteKey) {
			notFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminSession looks up the request's admin session, refreshing its idle
// timer: errNoSession without a well-formed cookie, sql.ErrNoRows when it's
// unknown, ended, made under another key, or its user is no longer an admin.
// An app session never counts.
func (s *Server) adminSession(r *http.Request) (store.AdminSession, error) {
	cookie, err := r.Cookie(auth.AdminCookieName(s.Config.Production))
	if err != nil || len(cookie.Value) != 43 {
		return store.AdminSession{}, errNoSession
	}
	return s.Store.FindAdminSession(r.Context(), auth.Digest(cookie.Value), s.adminKeyHash())
}

// requireAdminSession runs after requireAdminKey. It lets through requests
// with a live admin session, and its CSRF token on mutations, and counts
// reads against the admin's own budget (admin-read:per-user).
func (s *Server) requireAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.adminSession(r)
		if errors.Is(err, errNoSession) {
			fail(w, 401, "Please sign in to continue.")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 401, "Your admin session has ended. Please sign in again.")
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
		ctx := context.WithValue(r.Context(), adminSessionKey{}, session)
		r = r.WithContext(events.WithActor(ctx, session.User.ID))
		if !unsafeMethod(r.Method) && !s.allow(w, r, adminReadPerUser, adminReadPerUser.Key(session.User.PublicID)) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdminPage sends visitors without a live admin session from an inner
// admin page (/admin/<key>/<page>) to the admin sign-in page, before any
// HTML is served. Run only once the key in the path is right. If the
// database can't be reached the page is served anyway, as requirePageSession
// does.
func (s *Server) requireAdminPage(w http.ResponseWriter, r *http.Request, key string) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.adminSession(r.WithContext(ctx))
	if errors.Is(err, errNoSession) || errors.Is(err, sql.ErrNoRows) {
		adminPageHeaders(w)
		http.Redirect(w, r, "/admin/"+key, http.StatusFound)
		return false
	}
	if err != nil {
		// Served anyway: the page's own check shows the outage page.
		slog.Error("admin page session check failed", "error", err)
	}
	return true
}

// adminPageHeaders are sent with every admin page and redirect: the key in
// the path never leaves in a Referer, and nothing is cached or indexed.
func adminPageHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "no-store")
}

// adminPage serves /admin/{key} and /admin/{key}/{page...}. A wrong key (or
// admin off) gets exactly what an unknown URL gets, with no query; the right
// one the app shell, which renders the admin pages. Inner pages need an
// admin session first.
func (s *Server) adminPage(inner bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !auth.AdminKeyMatches(key, s.Config.AdminRouteKey) {
			s.Pages.ServeHTTP(w, r)
			return
		}
		if inner && !s.requireAdminPage(w, r, key) {
			return
		}
		s.Pages.ServeAdmin(w, r)
	})
}
