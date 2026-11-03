package httpapi

import (
	"context"
	"errors"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"mime"
	"net/http"
	"strings"
)

type apiKeyContext struct{}

func ApiKey(r *http.Request) store.ApiKeyAccess {
	a, _ := OptionalApiKey(r)
	return a
}
func OptionalApiKey(r *http.Request) (store.ApiKeyAccess, bool) {
	a, ok := r.Context().Value(apiKeyContext{}).(store.ApiKeyAccess)
	return a, ok
}

func denyApiKey(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	Fail(w, 401, "Invalid API credential.")
}

// ApiSecurity is selected by route, never by a supplied Authorization header.
// Machine writes retain JSON/no-store/deadline protections without browser CSRF.
func (s *Server) ApiSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHeaders(w.Header())
		if unsafeMethod(r.Method) && (r.Method != "DELETE" || r.ContentLength != 0) {
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				Fail(w, 415, "Content-Type must be application/json.")
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(events.WithUserAgent(ctx, r.UserAgent())))
		markTimedOut(ctx)
	})
}

func (s *Server) anonymousApiLimit(w http.ResponseWriter, r *http.Request, l ratelimit.Limit, increment bool) bool {
	if s.Config.RateLimits == config.RateLimitsOff || s.Config.PerIPLimitsOff {
		return true
	}
	hits, end, tracked := s.apiAnonymous.Count(l.IPKey(ClientIP(r)), l.Window, increment)
	if !tracked {
		observe.RateLimitRefused.Inc(l.Name + ":block")
		w.Header().Set("Retry-After", "60")
		Fail(w, 503, "API authentication is busy. Try again shortly.")
		return false
	}
	crossed := l.Block.Crossed(hits)
	if l.Name == ApiKeyAuthPerIP.Name {
		// Failed authentication closes admission at N, rather than N+1.
		// Failures already in flight may still increment past N; they must
		// not record a second crossing for the same window.
		crossed = hits == l.Block.At()
	}
	if increment && crossed {
		if err := s.recordLimitReached(r, l, "block", end); err != nil {
			InternalError(w, r, err)
			return false
		}
	}
	// The failure budget is checked before another guess; the Nth failure
	// returns 401, and subsequent requests wait for the window to end.
	blocked := l.Block.Exceeds(hits)
	if !increment {
		blocked = hits >= l.Block.At()
	}
	if blocked {
		RefuseLimit(w, l, end)
		return false
	}
	return true
}

// RequireApiKey authenticates a typed service/personal principal. Cookies have
// no authority and are neither inspected nor changed.
func (s *Server) RequireApiKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.anonymousApiLimit(w, r, ApiKeyIngressPerIP, true) || !s.anonymousApiLimit(w, r, ApiKeyAuthPerIP, false) {
			return
		}
		values := r.Header.Values("Authorization")
		value := ""
		if len(values) == 1 && len(values[0]) == 75 && strings.EqualFold(values[0][:7], "Bearer ") {
			value = values[0][7:]
		}
		a, err := s.Repo.FindApiKeyAccess(r.Context(), value, s.Config.RequireExisting)
		if errors.Is(err, store.ErrApiKeyInvalid) {
			if !s.anonymousApiLimit(w, r, ApiKeyAuthPerIP, true) {
				return
			}
			denyApiKey(w)
			return
		}
		if err != nil {
			InternalError(w, r, err)
			return
		}
		if !input.PublicID("wsp", r.PathValue("workspace")) || a.WorkspacePublicID() != r.PathValue("workspace") {
			NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), apiKeyContext{}, a)
		ctx = events.WithApiKey(events.WithActor(ctx, a.OwnerID()), a.PublicID())
		r = r.WithContext(ctx)
		observe.SetUser(ctx, a.OwnerPublicID())
		if !s.meterApiKey(w, r, a) {
			return
		}
		if err := s.Repo.UpdateApiKeyLastUse(ctx, a); err != nil {
			InternalError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) meterApiKey(w http.ResponseWriter, r *http.Request, a store.ApiKeyAccess) bool {
	key, owner, workspace := ApiKeyReadPerKey, ApiKeyReadPerUser, ApiKeyReadPerWorkspace
	if unsafeMethod(r.Method) {
		key, owner, workspace = ApiKeyWritePerKey, ApiKeyWritePerUser, ApiKeyWritePerWorkspace
	}
	if !s.Allow(w, r, key, key.Key(a.PublicID())) {
		return false
	}
	if a.OwnerID() != 0 && !s.Allow(w, r, owner, owner.Key(a.OwnerPublicID())) {
		return false
	}
	return s.Allow(w, r, workspace, workspace.Key(a.WorkspacePublicID()))
}

// RequireApiScope is mandatory for every v1 route, reads included. Detail
// scope precedes permission errors so inaccessible projects remain indistinct.
func (s *Server) RequireApiScope(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := ApiKey(r)
		if id := r.PathValue("project"); id != "" {
			if !input.PublicID("prj", id) {
				NotFound(w, r)
				return
			}
			found, err := s.Repo.CheckApiProjectScope(r.Context(), a, id)
			if err != nil {
				InternalError(w, r, err)
				return
			}
			if !found {
				NotFound(w, r)
				return
			}
		}
		if !a.Allows(scope) {
			Fail(w, 403, "This credential does not permit that action.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
