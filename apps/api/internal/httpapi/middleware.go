package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"mime"
	"net/http"
	"net/url"
	"time"
)

func unsafeMethod(method string) bool {
	return method != "GET" && method != "HEAD" && method != "OPTIONS"
}

// apiHeaders are on every API answer, a panic's 500 included: nothing is
// cached, sniffed, framed or sent on as a Referer.
func apiHeaders(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
}

func (s *Server) Security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHeaders(w.Header())
		if unsafeMethod(r.Method) {
			if r.Header.Get("Origin") != s.Config.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				Fail(w, 403, "Request origin is not allowed.")
				return
			}
			// A DELETE may have no body; one that has a body sends JSON too.
			if r.Method != "DELETE" || r.ContentLength != 0 {
				media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || media != "application/json" {
					Fail(w, 415, "Content-Type must be application/json.")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(events.WithUserAgent(ctx, r.UserAgent())))
		markTimedOut(ctx)
	})
}

// RequestTimeout is how long an API request may run: its context ends
// then, and the queries it is waiting on with it.
const RequestTimeout = 10 * time.Second

// markTimedOut flags the request as ended by its deadline when the
// deadline had passed by the time its handler returned (and not because
// the client went away, which cancels instead), for http_timeouts_total
// and request_minutes.timeouts: on a write, the change may have committed
// though the client saw an error.
func markTimedOut(ctx context.Context) {
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return
	}
	if request := observe.FromContext(ctx); request != nil {
		request.TimedOut = true
	}
}

// RequireSession lets through requests with a live session, and a valid CSRF
// token on mutations, then counts each against its user's budget (meter).
func (s *Server) RequireSession(next http.Handler) http.Handler {
	return s.signedIn(next, true)
}

// RequireSessionUnmetered is RequireSession without the budget, for the
// recovery routes: signing out, revoking other sessions and changing the
// password. Someone holding a stolen session then can't use up the owner's
// budget to stop them. They only delete rows, or keep sign-in's limits.
// The one other is POST /api/client-errors, which a page stuck in an error
// loop would otherwise spend the budget on; it has limits of its own.
func (s *Server) RequireSessionUnmetered(next http.Handler) http.Handler {
	return s.signedIn(next, false)
}
func (s *Server) signedIn(next http.Handler, metered bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.session(r)
		if errors.Is(err, errNoSession) {
			Fail(w, 401, "Please sign in to continue.")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			Fail(w, 401, "Your session has expired. Please sign in again.")
			return
		}
		if err != nil {
			InternalError(w, r, err)
			return
		}
		if unsafeMethod(r.Method) && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			Fail(w, 403, "Invalid CSRF token. Refresh and try again.")
			return
		}
		if s.mustVerify(session.User) && r.Context().Value(unverifiedKey{}) == nil {
			JSON(w, 403, map[string]any{"error": "Verify your email to continue.", "verificationRequired": true})
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey{}, session)
		r = r.WithContext(events.WithActor(ctx, session.User.ID))
		observe.SetUser(ctx, session.User.PublicID)
		if metered && !s.meter(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mustVerify reports whether u must verify its email before doing anything:
// it never has, and EMAIL_VERIFICATION_EXISTING=required.
func (s *Server) mustVerify(u store.User) bool {
	return s.Config.RequireExisting && u.EmailVerifiedAt == nil
}

// AllowUnverified lets an account that must verify its email (mustVerify)
// use the route: only reading the session, signing out, signing out other
// sessions and verifying (router/routes.go). Every other signed-in route answers it
// 403 with verificationRequired, so a route added later is covered without
// thinking about it. Wrap it around RequireSession or its variants.
func AllowUnverified(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), unverifiedKey{}, true)))
	})
}

// RequirePageSession sends visitors without a valid session from a signed-in
// page (/app/*) to login, returning there afterwards, before any HTML is
// served. If the database can't be reached the page is served anyway; its
// own session check then shows the outage page.
func (s *Server) RequirePageSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		session, err := s.session(r.WithContext(ctx))
		if err == nil {
			observe.SetUser(r.Context(), session.User.PublicID)
		}
		if errors.Is(err, errNoSession) || errors.Is(err, sql.ErrNoRows) {
			w.Header().Set("Cache-Control", "no-store")
			login := "/auth/login?next=" + url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, login, http.StatusFound)
			return
		}
		if err != nil {
			if ClientGone(r, err) {
				abortedByClient(r, err)
				return
			}
			reportFailure(r, "page session check failed", fmt.Errorf("page session check: %w", err))
		}
		next.ServeHTTP(w, r)
	})
}

// RequireWorkspace runs after RequireSession. It loads the {workspace} in the
// path through the signed-in user's membership; a malformed ID, a missing
// workspace and someone else's workspace all get the same 404.
func (s *Server) RequireWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("workspace")
		if !input.PublicID("wsp", id) {
			Fail(w, 404, "Workspace not found.")
			return
		}
		workspace, err := s.Repo.FindWorkspace(r.Context(), Session(r).User.ID, id)
		if errors.Is(err, sql.ErrNoRows) {
			Fail(w, 404, "Workspace not found.")
			return
		}
		if err != nil {
			InternalError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), workspaceKey{}, workspace)))
	})
}

// RequirePermission runs after RequireWorkspace and refuses members whose
// role doesn't grant p (auth/permissions.go).
func RequirePermission(p auth.Permission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Workspace(r).Role.Can(p) {
			Fail(w, 403, "You don’t have permission to do that in this workspace.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) LimitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Allow(w, r, AuthPerIP, AuthPerIP.IPKey(ClientIP(r))) {
			return
		}
		select {
		case s.authSlots <- struct{}{}:
			defer func() { <-s.authSlots }()
		default:
			observe.PasswordHashBusy.Inc()
			Fail(w, 503, "Sign-in is busy. Please try again shortly.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
