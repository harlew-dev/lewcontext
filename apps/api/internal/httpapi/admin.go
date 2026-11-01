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

// AdminKeyHash is the digest of the current ADMIN_ROUTE_KEY, which each admin
// session stores: rotating the key ends them all.
func (s *Server) AdminKeyHash() string {
	return auth.Digest(s.Config.AdminRouteKey)
}

// RequireAdminKey answers every /api/admin/ request whose X-Admin-Key isn't
// the configured key (or with admin off) exactly as an unknown API route,
// before anything else is looked at.
func (s *Server) RequireAdminKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.AdminKeyMatches(r.Header.Get("X-Admin-Key"), s.Config.AdminRouteKey) {
			NotFound(w, r)
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
	return s.Repo.FindAdminSession(r.Context(), auth.Digest(cookie.Value), s.AdminKeyHash())
}

// RequireAdminSession runs after RequireAdminKey. It lets through requests
// with a live admin session, and its CSRF token on mutations, and counts
// reads against the admin's own budget (admin-read:per-user).
func (s *Server) RequireAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.adminSession(r)
		if errors.Is(err, errNoSession) {
			Fail(w, 401, "Please sign in to continue.")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			Fail(w, 401, "Your admin session has ended. Please sign in again.")
			return
		}
		if err != nil {
			InternalError(w, err)
			return
		}
		if unsafeMethod(r.Method) && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			Fail(w, 403, "Invalid CSRF token. Refresh and try again.")
			return
		}
		ctx := context.WithValue(r.Context(), adminSessionKey{}, session)
		r = r.WithContext(events.WithActor(ctx, session.User.ID))
		if !unsafeMethod(r.Method) && !s.Allow(w, r, AdminReadPerUser, AdminReadPerUser.Key(session.User.PublicID)) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdminPage sends visitors without a live admin session from an inner
// admin page (/admin/<key>/<page>) to the admin sign-in page, before any
// HTML is served. Run only once the key in the path is right. If the
// database can't be reached the page is served anyway, as RequirePageSession
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

// AdminPage serves /admin/{key} and /admin/{key}/{page...}. A wrong key (or
// admin off) gets exactly what an unknown URL gets, with no query; the right
// one the admin build's shell, which renders the admin pages, and a fresh
// assets pass for its files. Inner pages need an admin session first.
func (s *Server) AdminPage(inner bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !auth.AdminKeyMatches(key, s.Config.AdminRouteKey) {
			s.Pages.ServeHTTP(w, r)
			return
		}
		// Without an admin build there is no page to serve: the ordinary 404,
		// and no pass.
		if !s.Pages.AdminBuilt() {
			s.Pages.ServeHTTP(w, r)
			return
		}
		if inner && !s.requireAdminPage(w, r, key) {
			return
		}
		s.issueAdminAssetsPass(w)
		s.Pages.ServeAdmin(w, r)
	})
}

// issueAdminAssetsPass sets the cookie that opens the admin build's files
// for AdminAssetsPassLifetime: only here, on an admin page about to be
// served with 200, never on a redirect or a 404. It is always signed with
// the CURRENT key's digest (AdminKeyHash), whichever key the page was
// served under: once a previous key is accepted too (ADMIN_ROUTE_KEY_PREVIOUS),
// its pages still hand out passes for the current key, and AdminAssets
// accepts no other.
func (s *Server) issueAdminAssetsPass(w http.ResponseWriter) {
	pass := s.keys.AdminAssetsPass(s.AdminKeyHash(), time.Now().Add(auth.AdminAssetsPassLifetime))
	http.SetCookie(w, auth.AdminAssetsCookie(pass, s.Config.Production))
}

// AdminAssets serves GET /admin-assets/{file...}, the admin build's files,
// to a browser holding a valid assets pass (auth/admin_assets.go). Anything
// else (admin off, no pass, an expired or tampered one, one signed under
// another key or APP_KEY, an unknown file, HTML or a dot-file) gets exactly
// what an unknown URL gets: the pass is checked before any file is looked
// up, so its answer doesn't depend on which files exist. The API never reads
// the pass; it opens code, nothing else.
func (s *Server) AdminAssets(w http.ResponseWriter, r *http.Request) {
	if !s.adminAssetsPassValid(r) || !s.Pages.ServeAdminFile(w, r, r.PathValue("file")) {
		s.Pages.ServeHTTP(w, r)
	}
}

// adminAssetsPassChecked is how many cookies named as the pass a request
// may have checked: a sibling subdomain can plant one under the same name
// (the name can't be __Host-, as its path isn't /), and it may be sent
// first, so the first alone isn't enough; but no request makes us compute
// HMACs without end.
const adminAssetsPassChecked = 4

// adminAssetsPassValid reports whether the request carries a pass signed
// for the current key that hasn't ended, among its first
// adminAssetsPassChecked cookies of the pass's name. With admin off nothing
// is.
func (s *Server) adminAssetsPassValid(r *http.Request) bool {
	if s.Config.AdminRouteKey == "" {
		return false
	}
	cookies := r.CookiesNamed(auth.AdminAssetsCookieName(s.Config.Production))
	now, keyHash := time.Now(), s.AdminKeyHash()
	for i, cookie := range cookies {
		if i == adminAssetsPassChecked {
			break
		}
		if s.keys.AdminAssetsPassValid(cookie.Value, keyHash, now) {
			return true
		}
	}
	return false
}
