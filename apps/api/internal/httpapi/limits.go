package httpapi

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// rateLimit is a fixed-window limit, counted in the rate_limits table or, for
// reads, in this process's memory (memlimit.go). Its name,
// "<feature>:<counted by>", is the key's prefix and, with the level reached,
// rate_limit.reached's limit.
type rateLimit struct {
	name   string
	limit  int
	window time.Duration
	// busy is the refusal message of a limit shared by everyone, answered
	// with 503 because it isn't the requester's doing. Limits per subject
	// leave it empty and answer 429.
	busy string
	// attempts makes a 429 say "Too many attempts" instead of "Too many
	// requests": for limits on signing in and resetting a password.
	attempts bool
	// inMemory counts in this process only: approximate (each server counts
	// its own), but without a database write per request.
	inMemory bool
}

var (
	// Everything that signs in or checks a password, per connection IP.
	authPerIP = rateLimit{name: "auth:per-ip", limit: 60, window: 15 * time.Minute, attempts: true}
	// Password guesses against one account, from any IP.
	authPerAccount = rateLimit{name: "auth:per-account", limit: 10, window: 15 * time.Minute, attempts: true}
	// Reset links requested from one IP, so it can't mail many people.
	passwordResetPerIP = rateLimit{name: "password-reset:per-ip", limit: 10, window: time.Hour, attempts: true}
	// Reset links requested for one email, from any IP: caps mail to one inbox.
	passwordResetPerEmail = rateLimit{name: "password-reset:per-email", limit: 3, window: time.Hour, attempts: true}
	// Reset links requested for any account: caps mail during a distributed flood.
	passwordResetGlobal = rateLimit{
		name:   "password-reset:global",
		limit:  100,
		window: time.Hour,
		busy:   "Password reset is busy. Try again later.",
	}
	// Accounts created from one IP.
	registerPerIP = rateLimit{name: "register:per-ip", limit: 10, window: time.Hour}
	// Accounts created by everyone: caps sign-ups during a distributed flood.
	registerGlobal = rateLimit{
		name:   "register:global",
		limit:  1000,
		window: time.Hour,
		busy:   "Sign-up is busy. Try again later.",
	}
	// Every signed-in write except the recovery routes (requireSession).
	apiPerUser = rateLimit{name: "api:per-user", limit: 300, window: 15 * time.Minute}
	// Every signed-in read: about 3 a second sustained, with room for SWR's
	// refreshes on focus.
	apiReadPerUser = rateLimit{name: "api-read:per-user", limit: 1000, window: 5 * time.Minute, inMemory: true}
	// Workspaces created by one user.
	workspacePerUser = rateLimit{name: "workspace:per-user", limit: 10, window: time.Hour}
	// Projects created by one user, across all of their workspaces.
	projectPerUser = rateLimit{name: "project:per-user", limit: 100, window: time.Hour}
	// Invitations sent by one user, across all of their workspaces: each one
	// emails an address the inviter chooses.
	invitationPerUser = rateLimit{name: "invitation:per-user", limit: 20, window: time.Hour}
	// Invitations sent by everyone: caps mail during an abuse wave.
	invitationGlobal = rateLimit{
		name:   "invitation:global",
		limit:  200,
		window: time.Hour,
		busy:   "Invitations are busy. Try again later.",
	}
)

// rateLimits lists every limit, for TestRateLimitNames.
var rateLimits = []rateLimit{
	authPerIP, authPerAccount,
	passwordResetPerIP, passwordResetPerEmail, passwordResetGlobal,
	registerPerIP, registerGlobal,
	apiPerUser, apiReadPerUser, workspacePerUser, projectPerUser,
	invitationPerUser, invitationGlobal,
}

// key is the rate_limits key for one subject (an IP, an email or a user's
// public ID), stored as a digest so the table holds no addresses or IDs.
func (l rateLimit) key(subject string) string {
	return l.name + ":" + auth.Digest(subject)
}

// everyone is the rate_limits key of a limit shared by all requests.
func (l rateLimit) everyone() string {
	return l.name
}

// refusal is the 429 message for a window that ends in left.
func (l rateLimit) refusal(left time.Duration) string {
	tooMany := "Too many requests."
	if l.attempts {
		tooMany = "Too many attempts."
	}
	return tooMany + " Try again in " + waitFor(left) + "."
}

// retryAfter is the Retry-After value for a window that ends in left: whole
// seconds, rounded up, and at least 1.
func retryAfter(left time.Duration) string {
	seconds := int((left + time.Second - 1) / time.Second)
	return strconv.Itoa(max(seconds, 1))
}

// waitFor says how long is left, rounded up to whole minutes: "a minute",
// "12 minutes" or "an hour".
func waitFor(left time.Duration) string {
	minutes := int((left + time.Minute - 1) / time.Minute)
	switch {
	case minutes <= 1:
		return "a minute"
	case minutes == 60:
		return "an hour"
	default:
		return strconv.Itoa(minutes) + " minutes"
	}
}

// thousands writes n with a comma between thousands: 1000 is "1,000".
func thousands(n int) string {
	digits := strconv.Itoa(n)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

// allow counts a request against l under key and answers 429 (503 with busy
// for a limit shared by everyone) when it is used up, with Retry-After set to
// the time left in the window. The first refusal in a window records
// rate_limit.reached; the ones after it don't, so a flood writes one event.
// With RATE_LIMITS=off it allows everything and counts nothing.
func (s *Server) allow(w http.ResponseWriter, r *http.Request, l rateLimit, key string) bool {
	if s.Config.RateLimits == config.RateLimitsOff {
		return true
	}
	allowed, firstRefusal, resetsAt, err := s.count(r, l, key)
	if err == nil && firstRefusal {
		err = s.recordLimitReached(r, l, resetsAt)
	}
	if err != nil {
		internalError(w, err)
		return false
	}
	if allowed {
		return true
	}
	left := time.Until(resetsAt)
	w.Header().Set("Retry-After", retryAfter(left))
	if l.busy != "" {
		fail(w, 503, l.busy)
	} else {
		fail(w, 429, l.refusal(left))
	}
	return false
}

// count adds a hit to key's window, in memory or in the database.
func (s *Server) count(r *http.Request, l rateLimit, key string) (allowed, firstRefusal bool, resetsAt time.Time, err error) {
	if l.inMemory {
		allowed, firstRefusal, resetsAt = s.memoryLimits.allow(key, l.limit, l.window)
		return allowed, firstRefusal, resetsAt, nil
	}
	return s.Store.Allow(r.Context(), key, l.limit, l.window)
}

// recordLimitReached records rate_limit.reached for l, with the user and the
// workspace when the request has them. Every limit has one level for now,
// block (the CAPTCHA plan adds challenge).
func (s *Server) recordLimitReached(r *http.Request, l rateLimit, resetsAt time.Time) error {
	if l.busy != "" {
		slog.Warn("a rate limit shared by everyone was reached", "limit", l.name, "window", l.window)
	}
	e := events.RateLimitReached{Limit: l.name + ":block", IP: clientIP(r), Window: int(l.window.Seconds()), ResetsAt: resetsAt}
	var scope store.EventScope
	if session, ok := optionalSession(r); ok {
		e.User, scope.UserID = session.User.PublicID, session.User.ID
	}
	if workspace, ok := optionalWorkspace(r); ok {
		e.Workspace, scope.WorkspaceID = workspace.PublicID, workspace.ID
	}
	return s.Store.Record(r.Context(), e, scope)
}

// limit counts each request against l for the session's user before next.
// Routes that create rows or send mail name theirs in routes.go, next to the
// permission; every signed-in route also has a budget (meter).
func (s *Server) limit(l rateLimit, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.allow(w, r, l, l.key(current(r).User.PublicID)) {
			next(w, r)
		}
	}
}

// meter counts a signed-in request against its user's budget: writes in the
// database (api:per-user), reads in memory (api-read:per-user). Counted after
// the CSRF check, so another site can't spend someone's budget.
func (s *Server) meter(w http.ResponseWriter, r *http.Request) bool {
	budget := apiReadPerUser
	if unsafeMethod(r.Method) {
		budget = apiPerUser
	}
	return s.allow(w, r, budget, budget.key(current(r).User.PublicID))
}
