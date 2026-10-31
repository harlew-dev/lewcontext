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
	// tooMany replaces a 429's "Too many requests." when what's counted
	// isn't the requester, so the message says what was refused.
	tooMany string
	// inMemory counts in this process only: approximate (each server counts
	// its own), but without a database write per request.
	inMemory bool
}

var (
	// authPerIP: everything that signs in or checks a password, per client IP.
	authPerIP = rateLimit{name: "auth:per-ip", limit: 60, window: 15 * time.Minute, attempts: true}
	// authPerAccount: password guesses against one account, from any IP.
	authPerAccount = rateLimit{name: "auth:per-account", limit: 10, window: 15 * time.Minute, attempts: true}
	// mfaPerAccount: second-factor codes checked for one account, from any IP.
	// Signing in, turning MFA on or off, new recovery codes. With one step of
	// drift a guess covers 3 codes in a million, so this caps an attacker who
	// already has the password at about 0.14% a day.
	mfaPerAccount = rateLimit{name: "mfa:per-account", limit: 5, window: 15 * time.Minute, attempts: true}

	// passwordResetPerIP: reset links requested from one IP.
	// So one IP can't mail many people.
	passwordResetPerIP = rateLimit{name: "password-reset:per-ip", limit: 10, window: time.Hour, attempts: true}
	// passwordResetPerEmail: reset links requested for one email, from any IP.
	// Caps mail to one inbox.
	passwordResetPerEmail = rateLimit{name: "password-reset:per-email", limit: 3, window: time.Hour, attempts: true}
	// passwordResetGlobal: reset links requested for any account.
	// Caps mail during a distributed flood.
	passwordResetGlobal = rateLimit{
		name:   "password-reset:global",
		limit:  100,
		window: time.Hour,
		busy:   "Password reset is busy. Try again later.",
	}

	// registerPerIP: accounts created from one IP.
	registerPerIP = rateLimit{name: "register:per-ip", limit: 10, window: time.Hour}
	// registerPerEmail: sign-ups for one email, from any IP, with verification on.
	// Each one emails the address, a code or "you already have an account".
	// Counted for every address alike, so a refusal says nothing about who has
	// an account.
	registerPerEmail = rateLimit{
		name:    "register:per-email",
		limit:   3,
		window:  time.Hour,
		tooMany: "This email address has had too many sign-up attempts recently.",
	}
	// registerGlobal: accounts created by everyone.
	// Caps sign-ups during a distributed flood.
	registerGlobal = rateLimit{
		name:   "register:global",
		limit:  1000,
		window: time.Hour,
		busy:   "Sign-up is busy. Try again later.",
	}

	// emailCodePerEmail: emailed codes checked for one email, from any browser or IP.
	// Signing up, or verifying an account's email. Each code also ends after 5
	// wrong tries, so guessing one is about 1 in 100,000 a day.
	emailCodePerEmail = rateLimit{name: "email-code:per-email", limit: 10, window: 24 * time.Hour, attempts: true}
	// emailVerificationPerEmail: codes a signed-in account asks for to verify its email.
	// Each one emails the account's address, so this caps mail to one inbox.
	emailVerificationPerEmail = rateLimit{
		name:    "email-verification:per-email",
		limit:   3,
		window:  time.Hour,
		tooMany: "You’ve asked for too many codes recently.",
	}
	// emailVerificationGlobal: codes asked for by everyone.
	// Caps the mail when EMAIL_VERIFICATION_EXISTING=required sends every
	// unverified account to verify at its next sign-in.
	emailVerificationGlobal = rateLimit{
		name:   "email-verification:global",
		limit:  500,
		window: time.Hour,
		busy:   "Email verification is busy. Try again in a few minutes.",
	}

	// apiPerUser: every signed-in write except the recovery routes (requireSession).
	apiPerUser = rateLimit{name: "api:per-user", limit: 300, window: 15 * time.Minute}
	// apiReadPerUser: every signed-in read.
	// About 3 a second sustained, with room for SWR's refreshes on focus.
	apiReadPerUser = rateLimit{name: "api-read:per-user", limit: 1000, window: 5 * time.Minute, inMemory: true}
	// adminReadPerUser: every admin dashboard read, per admin.
	// The admin pages' own budget, as api-read:per-user is the app's.
	adminReadPerUser = rateLimit{name: "admin-read:per-user", limit: 1000, window: 5 * time.Minute, inMemory: true}

	// workspacePerUser: workspaces created by one user.
	workspacePerUser = rateLimit{name: "workspace:per-user", limit: 10, window: time.Hour}
	// projectPerUser: projects created by one user, across all of their workspaces.
	projectPerUser = rateLimit{name: "project:per-user", limit: 100, window: time.Hour}

	// invitationPerUser: invitations sent by one user, across all of their workspaces.
	// Each one emails an address the inviter chooses.
	invitationPerUser = rateLimit{name: "invitation:per-user", limit: 20, window: time.Hour}
	// invitationPerEmail: invitations to one email, from any inviter and workspace.
	// Caps mail to one inbox. Counted for every address, registered or not, so
	// the message says nothing about who has an account.
	invitationPerEmail = rateLimit{
		name:    "invitation:per-email",
		limit:   5,
		window:  time.Hour,
		tooMany: "This email address has had too many invitations recently.",
	}
	// invitationGlobal: invitations sent by everyone.
	// Caps mail during an abuse wave.
	invitationGlobal = rateLimit{
		name:   "invitation:global",
		limit:  200,
		window: time.Hour,
		busy:   "Invitations are busy. Try again later.",
	}
)

// rateLimits lists every limit, for TestRateLimitNames.
var rateLimits = []rateLimit{
	authPerIP, authPerAccount, mfaPerAccount,
	passwordResetPerIP, passwordResetPerEmail, passwordResetGlobal,
	registerPerIP, registerPerEmail, registerGlobal,
	emailCodePerEmail, emailVerificationPerEmail, emailVerificationGlobal,
	apiPerUser, apiReadPerUser, adminReadPerUser,
	workspacePerUser, projectPerUser,
	invitationPerUser, invitationPerEmail, invitationGlobal,
}

// key is the rate_limits key for one subject (an IP, an email or a user's
// public ID), stored as a digest so the table holds no addresses or IDs.
func (l rateLimit) key(subject string) string {
	return l.name + ":" + auth.Digest(subject)
}

// ipKey is the rate_limits key for the request's client address
// (limitSubject: an IPv4 address, or an IPv6 address's /64).
func (l rateLimit) ipKey(r *http.Request) string {
	return l.key(limitSubject(clientIP(r)))
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
	if l.tooMany != "" {
		tooMany = l.tooMany
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
// "12 minutes" or "an hour", and past an hour to whole hours: "3 hours".
func waitFor(left time.Duration) string {
	minutes := int((left + time.Minute - 1) / time.Minute)
	switch {
	case minutes <= 1:
		return "a minute"
	case minutes <= 60:
		if minutes == 60 {
			return "an hour"
		}
		return strconv.Itoa(minutes) + " minutes"
	default:
		return strconv.Itoa((minutes+59)/60) + " hours"
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
	e := events.RateLimitReached{Limit: l.name + ":block", Window: int(l.window.Seconds()), ResetsAt: resetsAt}
	var scope store.EventScope
	if session, ok := optionalSession(r); ok {
		e.User, scope.UserID = session.User.PublicID, session.User.ID
	}
	if admin, ok := optionalAdmin(r); ok {
		e.User, scope.UserID = admin.User.PublicID, admin.User.ID
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
