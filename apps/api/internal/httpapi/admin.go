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
// session made at the door stores: rotating the key ends them all, apart
// from those made under ADMIN_ROUTE_KEY_PREVIOUS while it is set.
func (s *Server) AdminKeyHash() string {
	return auth.Digest(s.Config.AdminRouteKey)
}

// AdminKeys are the digests a live admin session may have been made under:
// the current key's, and the previous one's while it is set.
func (s *Server) AdminKeys() store.AdminKeys {
	keys := store.AdminKeys{Current: s.AdminKeyHash()}
	if s.Config.AdminRouteKeyPrevious != "" {
		keys.Previous = auth.Digest(s.Config.AdminRouteKeyPrevious)
	}
	return keys
}

// matchAdminKey says which configured key given is, comparing it with both
// every time (auth.MatchAdminKey).
func (s *Server) matchAdminKey(given string) auth.AdminKeyMatch {
	return auth.MatchAdminKey(given, s.Config.AdminRouteKey, s.Config.AdminRouteKeyPrevious)
}

// adminKeyMatchKey holds which key a request's X-Admin-Key matched, for
// RequireAdminSession.
type adminKeyMatchKey struct{}

// adminKeyStatusHeader tells the admin pages they were reached under the
// key being retired (ADMIN_ROUTE_KEY_PREVIOUS), so they can say so. Only
// sent with a live session made under that key.
const adminKeyStatusHeader = "X-Admin-Key-Status"

// RequireAdminKey answers every /api/admin/ request whose X-Admin-Key isn't
// the current key (or with admin off), or that comes from an address
// outside ADMIN_ALLOWED_IPS, exactly as an unknown API route, before
// anything else is looked at. The previous key gets that answer too: it
// never opens the door (the sign-in steps).
func (s *Server) RequireAdminKey(next http.Handler) http.Handler {
	return s.requireAdminKey(false, next)
}

// RequireAdminKeyOrPrevious is RequireAdminKey for the routes behind
// RequireAdminSession, which also take the previous key: there
// RequireAdminSession lets through only a live session made under it.
func (s *Server) RequireAdminKeyOrPrevious(next http.Handler) http.Handler {
	return s.requireAdminKey(true, next)
}

func (s *Server) requireAdminKey(previousAllowed bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		match := s.matchAdminKey(r.Header.Get("X-Admin-Key"))
		// The address is checked only once a key matched, so a wrong key
		// costs nothing and records nothing.
		if match == auth.AdminKeyNone || !s.adminAddressAllowed(r) || (match == auth.AdminKeyPrevious && !previousAllowed) {
			NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminKeyMatchKey{}, match)))
	})
}

// errPreviousKeyHidden: a request under the previous key without a live
// session made under it, which is answered exactly as a wrong key.
var errPreviousKeyHidden = errors.New("the previous admin key without its session")

// adminSession looks up the request's admin session, refreshing its idle
// timer, for a request whose key matched match. Only a session made under
// that key is looked up (and refreshed):
//   - errNoSession without a well-formed cookie;
//   - sql.ErrNoRows when it's unknown, ended, made under another key, or its
//     user is no longer an admin;
//   - under the previous key, errPreviousKeyHidden for anything but a live
//     session made under one of the keys (a live one under the current key
//     is sql.ErrNoRows, 401), so the previous key looks like a wrong one to
//     anyone without such a session. A database error stays an error: the
//     callers answer it as a wrong key too.
//
// The session's key digest is checked again against both keys' in constant
// time (auth.MatchAdminKeyHash). An app session never counts.
func (s *Server) adminSession(r *http.Request, match auth.AdminKeyMatch) (store.AdminSession, error) {
	cookie, err := r.Cookie(auth.AdminCookieName(s.Config.Production))
	if err != nil || len(cookie.Value) != 43 {
		if match == auth.AdminKeyPrevious {
			return store.AdminSession{}, errPreviousKeyHidden
		}
		return store.AdminSession{}, errNoSession
	}
	keys, hash := s.AdminKeys(), auth.Digest(cookie.Value)
	matched, other := keys.Current, keys.Previous
	if match == auth.AdminKeyPrevious {
		matched, other = keys.Previous, keys.Current
	}
	session, err := s.Repo.FindAdminSession(r.Context(), hash, matched)
	if errors.Is(err, sql.ErrNoRows) && match == auth.AdminKeyPrevious {
		// A live session under the current key, sent with the previous one,
		// is 401 as under the current key; anything else is hidden.
		live, err := s.Repo.CheckAdminSession(r.Context(), hash, other)
		if err != nil {
			return session, err
		}
		if live {
			return session, sql.ErrNoRows
		}
		return session, errPreviousKeyHidden
	}
	if err != nil {
		return session, err
	}
	// Defence in depth: the store found it by the matched digest.
	if auth.MatchAdminKeyHash(session.KeyHash, keys.Current, keys.Previous) != match {
		return store.AdminSession{}, sql.ErrNoRows
	}
	return session, nil
}

// AdminSessionDigest is what admin_sessions stores for the request's admin
// cookie (signing out, the Admins page's current session). Call it only
// behind RequireAdminSession, which checked the cookie.
func (s *Server) AdminSessionDigest(r *http.Request) string {
	cookie, err := r.Cookie(auth.AdminCookieName(s.Config.Production))
	if err != nil {
		return ""
	}
	return auth.Digest(cookie.Value)
}

// RequireAdminSession runs after RequireAdminKeyOrPrevious. It lets through
// requests with a live admin session made under the key they came with, and
// its CSRF token on mutations, notices a session used from another network
// (admin.session.moved), and counts reads against the admin's own budget
// (admin-read:per-user). Under the previous key it says so
// (X-Admin-Key-Status: previous).
func (s *Server) RequireAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		match, ok := r.Context().Value(adminKeyMatchKey{}).(auth.AdminKeyMatch)
		if !ok {
			// Registered without the key's middleware: answer as one would.
			NotFound(w, r)
			return
		}
		session, err := s.adminSession(r, match)
		switch {
		case errors.Is(err, errPreviousKeyHidden):
			NotFound(w, r)
			return
		case err != nil && match == auth.AdminKeyPrevious && !errors.Is(err, sql.ErrNoRows):
			// Fail closed: under the previous key an outage answers as a
			// wrong key, never confirming the key is still set.
			slog.Error("admin session check under the previous key failed", "error", err)
			NotFound(w, r)
			return
		case errors.Is(err, errNoSession):
			Fail(w, 401, "Please sign in to continue.")
			return
		case errors.Is(err, sql.ErrNoRows):
			Fail(w, 401, "Your admin session has ended. Please sign in again.")
			return
		case err != nil:
			InternalError(w, err)
			return
		}
		if match == auth.AdminKeyPrevious {
			w.Header().Set(adminKeyStatusHeader, "previous")
		}
		if unsafeMethod(r.Method) && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			Fail(w, 403, "Invalid CSRF token. Refresh and try again.")
			return
		}
		ctx := context.WithValue(r.Context(), adminSessionKey{}, session)
		r = r.WithContext(events.WithActor(ctx, session.User.ID))
		if err := s.noticeAdminSessionMoved(r, session); err != nil {
			InternalError(w, err)
			return
		}
		if !unsafeMethod(r.Method) && !s.Allow(w, r, AdminReadPerUser, AdminReadPerUser.Key(session.User.PublicID)) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminRole lets through, after RequireAdminSession, only admins
// whose role may do what needs role (an operator anything a viewer may);
// the rest get 403. Each admin action names the role it needs in
// adminRoutes, as workspace mutations name a permission (ws.Can). The web
// app hides what a role can't do; this is what enforces it.
func RequireAdminRole(role auth.AdminRole, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Admin(r).Role.Can(role) {
			Fail(w, 403, "Your admin role doesn't allow this.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminStepUpNeeded is the answer to an account-and-access action outside
// the admin session's step-up window: the web app asks for a code, opens
// the window (POST /api/admin/step-up) and tries again.
const AdminStepUpNeeded = "Enter a code to continue."

// RequireAdminStepUp lets through, after RequireAdminSession (its CSRF check
// included) and RequireAdminRole, only an admin session whose step-up
// window is open: a fresh code within auth.AdminStepUpWindow. Otherwise 403
// {error, stepUp: true}. It reads the window from the session already
// loaded, compared with the database's clock there, so it costs no query.
// Every account-and-access action in adminRoutes is wrapped in it.
func RequireAdminStepUp(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Admin(r).SteppedUp {
			JSON(w, 403, map[string]any{"error": AdminStepUpNeeded, "stepUp": true})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdminPage sends visitors without a live admin session from an inner
// admin page (/admin/<key>/<page>) to the admin sign-in page, before any
// HTML is served, and reports whether to serve the page. Run only once the
// key in the path is right. Under the previous key there is no sign-in page
// to send them to: anything but a live session made under it gets exactly
// what a wrong key gets, an outage included (it fails closed, so it never
// confirms the key is still set nor hands out a pass). Under the current
// key, if the database can't be reached the page is served anyway, as
// RequirePageSession does: its own check then shows the outage page.
func (s *Server) requireAdminPage(w http.ResponseWriter, r *http.Request, key string, match auth.AdminKeyMatch) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.adminSession(r.WithContext(ctx), match)
	switch {
	case err == nil:
		return true
	case match == auth.AdminKeyPrevious:
		if !errors.Is(err, errPreviousKeyHidden) && !errors.Is(err, sql.ErrNoRows) {
			slog.Error("admin page session check under the previous key failed", "error", err)
		}
		s.Pages.ServeHTTP(w, r)
		return false
	case errors.Is(err, errNoSession), errors.Is(err, sql.ErrNoRows):
		adminPageHeaders(w)
		http.Redirect(w, r, "/admin/"+key, http.StatusFound)
		return false
	default:
		// Served anyway, with its assets pass: anyone with the current key
		// gets both from the sign-in page, so the pass gives nothing more,
		// and without it the page couldn't load to show the outage.
		slog.Error("admin page session check failed", "error", err)
		return true
	}
}

// adminPageHeaders are sent with every admin page and redirect: the key in
// the path never leaves in a Referer, and nothing is cached or indexed.
func adminPageHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "no-store")
}

// AdminPage serves /admin/{key} and /admin/{key}/{page...}. A wrong key (or
// admin off), or an address outside ADMIN_ALLOWED_IPS, gets exactly what an
// unknown URL gets, with no query; the right one the admin build's shell,
// which renders the admin pages, and a fresh assets pass for its files.
// Inner pages need an admin session first. The previous key serves only
// inner pages, to a live session made under it: never the sign-in page.
func (s *Server) AdminPage(inner bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		match := s.matchAdminKey(key)
		// Without an admin build there is no page to serve: the ordinary 404,
		// and no pass.
		if match == auth.AdminKeyNone || !s.Pages.AdminBuilt() || !s.adminAddressAllowed(r) || (match == auth.AdminKeyPrevious && !inner) {
			s.Pages.ServeHTTP(w, r)
			return
		}
		if inner && !s.requireAdminPage(w, r, key, match) {
			return
		}
		if match == auth.AdminKeyPrevious {
			w.Header().Set(adminKeyStatusHeader, "previous")
		}
		s.issueAdminAssetsPass(w)
		s.Pages.ServeAdmin(w, r)
	})
}

// issueAdminAssetsPass sets the cookie that opens the admin build's files
// for AdminAssetsPassLifetime: only here, on an admin page about to be
// served with 200, never on a redirect or a 404. It is always signed with
// the CURRENT key's digest (AdminKeyHash), whichever key the page was
// served under: pages under ADMIN_ROUTE_KEY_PREVIOUS still hand out passes
// for the current key, and AdminAssets accepts no other.
func (s *Server) issueAdminAssetsPass(w http.ResponseWriter) {
	pass := s.keys.AdminAssetsPass(s.AdminKeyHash(), time.Now().Add(auth.AdminAssetsPassLifetime))
	http.SetCookie(w, auth.AdminAssetsCookie(pass, s.Config.Production))
}

// AdminAssets serves GET /admin-assets/{file...}, the admin build's files,
// to a browser holding a valid assets pass (auth/admin_assets.go). Anything
// else (admin off, no pass, an expired or tampered one, one signed under
// another key or APP_KEY, an address outside ADMIN_ALLOWED_IPS, an unknown
// file, HTML or a dot-file) gets exactly what an unknown URL gets: the pass
// is checked before any file is looked up, so its answer doesn't depend on
// which files exist. The address is checked only with a valid pass, so a
// refusal (recorded) means a pass is held outside the list. The API never
// reads the pass; it opens code, nothing else.
func (s *Server) AdminAssets(w http.ResponseWriter, r *http.Request) {
	if !s.adminAssetsPassValid(r) || !s.adminAddressAllowed(r) || !s.Pages.ServeAdminFile(w, r, r.PathValue("file")) {
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
