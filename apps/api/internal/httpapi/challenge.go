package httpapi

import (
	"context"
	"errors"
	"lewcontext/api/internal/captcha"
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
func (s *Server) passChallenge(w http.ResponseWriter, r *http.Request) bool {
	c, _ := r.Context().Value(challengeKey{}).(*challenge)
	if c == nil || s.Captcha == nil || c.passed {
		return true
	}
	token := r.Header.Get(ChallengeTokenHeader)
	if token == "" {
		s.challengeRequired(w, c.action, "Complete the check to continue.")
		return false
	}
	err := s.Captcha.Verify(r.Context(), token, c.action, ClientIP(r))
	switch {
	case err == nil:
		c.passed = true
		return true
	case errors.Is(err, captcha.ErrRejected):
		s.challengeRequired(w, c.action, "The check failed. Try again.")
	default:
		Fail(w, 503, "Verification is unavailable. Try again shortly.")
	}
	return false
}

// challengeRequired answers 428 with the provider, its public site key and
// the action, which is all the browser needs to show the CAPTCHA.
func (s *Server) challengeRequired(w http.ResponseWriter, action, message string) {
	JSON(w, 428, map[string]any{
		"error": message,
		"challenge": map[string]string{
			"provider": s.Captcha.Name(),
			"siteKey":  s.Captcha.SiteKey(),
			"action":   action,
		},
	})
}
