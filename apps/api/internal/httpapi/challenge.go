package httpapi

import (
	"context"
	"errors"
	"lewcontext/api/internal/captcha"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/observe"
	"log/slog"
	"net/http"
)

// ChallengeTokenHeader carries a solved CAPTCHA's token, on the request
// resent after a 428.
const ChallengeTokenHeader = "X-Challenge-Token"

// The challenge actions: the routes whose requests a rate limit's challenge
// level may ask to solve a CAPTCHA (Challengeable). Each is also the action
// the token must have been solved for, so a token from one form can't be
// spent on another. Routes on pages holding a token in their URL (a reset
// or invitation link) never name one, so the provider's script never loads
// there.
const (
	ChallengeLogin         = "login"
	ChallengeRegister      = "register"
	ChallengePasswordReset = "password_reset"
	ChallengeInvitation    = "invitation"
)

// challenge is a request's challenge action, and whether its token has been
// verified: one token covers every limit the request is counted against.
type challenge struct {
	action string
	passed bool
}

type challengeKey struct{}

// Challengeable lets next's requests answer a challenge level with a
// CAPTCHA solved for action. Without it, a limit's challenge level is
// skipped on the route and only its block level applies. It goes outside
// whatever counts the limits (LimitAuth).
func (s *Server) Challengeable(action string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), challengeKey{}, &challenge{action: action})))
	})
}

// ChallengeableFunc is Challengeable for a HandlerFunc.
func (s *Server) ChallengeableFunc(action string, next http.HandlerFunc) http.HandlerFunc {
	return s.Challengeable(action, next).ServeHTTP
}

// passChallenge decides a request above a challenge level: through on a
// route without an action or with no provider (CAPTCHA_PROVIDER=off), or
// once its token is verified (once per request). Otherwise 428 with what
// the browser needs to show the CAPTCHA, or 503 while the provider can't be
// reached (fail closed, only in challenge mode). It runs before any side
// effect, so a refused request changed nothing and can be sent again.
func (s *Server) passChallenge(w http.ResponseWriter, r *http.Request, l ratelimit.Limit) bool {
	c := challengeOf(r)
	if c == nil || s.Captcha == nil {
		return true
	}
	return s.solve(w, r, c, l)
}

// challengeOf is the request's challenge context (Challengeable), or nil
// on a route that names no action.
func challengeOf(r *http.Request) *challenge {
	c, _ := r.Context().Value(challengeKey{}).(*challenge)
	return c
}

// challengeableFor refuses a request that counts a shared limit with a
// challenge level (register:global) on a route that isn't Challengeable.
// Such a route could never be asked for a token, so its requests would
// pass the challenge for free and push the limit to its block level for
// everyone (admit). It's a mistake in router/routes.go, so it fails closed
// and loudly, on every request, whatever RATE_LIMITS says: 503 and an
// error in the log. Per-IP limits (auth:per-ip) may be counted anywhere:
// on a route without an action their challenge level is skipped.
func (s *Server) challengeableFor(w http.ResponseWriter, r *http.Request, l ratelimit.Limit) bool {
	if !l.Shared() || !l.Challenge.Set() || challengeOf(r) != nil {
		return true
	}
	slog.ErrorContext(r.Context(), "a route counts a shared limit with a challenge level but isn't Challengeable (router/routes.go)", "limit", l.Name, "route", r.Pattern)
	Fail(w, 503, verificationUnavailable)
	return false
}

// verificationUnavailable is the 503 while a token can't be checked.
const verificationUnavailable = "Verification is unavailable. Try again shortly."

// solve requires c's token: through once it's verified (once per request,
// c.passed), else 428 or 503. c is non-nil and there is a provider. Each
// refusal is counted as l's challenge level (rate_limit_refused_total,
// "<limit>:challenge"), except for a client that went away while the token
// was being checked (ClientGone), which isn't answered.
func (s *Server) solve(w http.ResponseWriter, r *http.Request, c *challenge, l ratelimit.Limit) bool {
	if c.passed {
		return true
	}
	token := r.Header.Get(ChallengeTokenHeader)
	if token == "" {
		s.challengeRequired(w, l, c.action, "Complete the check to continue.")
		return false
	}
	err := s.Captcha.Verify(r.Context(), token, c.action, ClientIP(r))
	switch {
	case err == nil:
		c.passed = true
		return true
	case ClientGone(r, err):
		abortedByClient(r, err)
	case errors.Is(err, captcha.ErrRejected):
		s.challengeRequired(w, l, c.action, "The check failed. Try again.")
	default:
		observe.RateLimitRefused.Inc(l.Name + ":challenge")
		Fail(w, 503, verificationUnavailable)
	}
	return false
}

// challengeRequired answers 428 with the provider, its public site key and
// the action, which is all the browser needs to show the CAPTCHA.
func (s *Server) challengeRequired(w http.ResponseWriter, l ratelimit.Limit, action, message string) {
	observe.RateLimitRefused.Inc(l.Name + ":challenge")
	JSON(w, 428, map[string]any{
		"error": message,
		"challenge": map[string]string{
			"provider": s.Captcha.Name(),
			"siteKey":  s.Captcha.SiteKey(),
			"action":   action,
		},
	})
}
