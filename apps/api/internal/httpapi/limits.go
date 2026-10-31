package httpapi

import (
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

var (
	// authPerIP: everything that signs in or checks a password, per client IP.
	authPerIP = ratelimit.Limit{Name: "auth:per-ip", Max: 60, Window: 15 * time.Minute, Attempts: true}
	// authPerAccount: password guesses against one account, from any IP.
	authPerAccount = ratelimit.Limit{Name: "auth:per-account", Max: 10, Window: 15 * time.Minute, Attempts: true}
	// mfaPerAccount: second-factor codes checked for one account, from any IP.
	// Signing in, turning MFA on or off, new recovery codes. With one step of
	// drift a guess covers 3 codes in a million, so this caps an attacker who
	// already has the password at about 0.14% a day.
	mfaPerAccount = ratelimit.Limit{Name: "mfa:per-account", Max: 5, Window: 15 * time.Minute, Attempts: true}

	// passwordResetPerIP: reset links requested from one IP.
	// So one IP can't mail many people.
	passwordResetPerIP = ratelimit.Limit{Name: "password-reset:per-ip", Max: 10, Window: time.Hour, Attempts: true}
	// passwordResetPerEmail: reset links requested for one email, from any IP.
	// Caps mail to one inbox.
	passwordResetPerEmail = ratelimit.Limit{Name: "password-reset:per-email", Max: 3, Window: time.Hour, Attempts: true}
	// passwordResetGlobal: reset links requested for any account.
	// Caps mail during a distributed flood.
	passwordResetGlobal = ratelimit.Limit{
		Name:   "password-reset:global",
		Max:    100,
		Window: time.Hour,
		Busy:   "Password reset is busy. Try again later.",
	}

	// registerPerIP: accounts created from one IP.
	registerPerIP = ratelimit.Limit{Name: "register:per-ip", Max: 10, Window: time.Hour}
	// registerPerEmail: sign-ups for one email, from any IP, with verification on.
	// Each one emails the address, a code or "you already have an account".
	// Counted for every address alike, so a refusal says nothing about who has
	// an account.
	registerPerEmail = ratelimit.Limit{
		Name:    "register:per-email",
		Max:     3,
		Window:  time.Hour,
		TooMany: "This email address has had too many sign-up attempts recently.",
	}
	// registerGlobal: accounts created by everyone.
	// Caps sign-ups during a distributed flood.
	registerGlobal = ratelimit.Limit{
		Name:   "register:global",
		Max:    1000,
		Window: time.Hour,
		Busy:   "Sign-up is busy. Try again later.",
	}

	// emailCodePerEmail: emailed codes checked for one email, from any browser or IP.
	// Signing up, or verifying an account's email. Each code also ends after 5
	// wrong tries, so guessing one is about 1 in 100,000 a day.
	emailCodePerEmail = ratelimit.Limit{Name: "email-code:per-email", Max: 10, Window: 24 * time.Hour, Attempts: true}
	// emailVerificationPerEmail: codes a signed-in account asks for to verify its email.
	// Each one emails the account's address, so this caps mail to one inbox.
	emailVerificationPerEmail = ratelimit.Limit{
		Name:    "email-verification:per-email",
		Max:     3,
		Window:  time.Hour,
		TooMany: "You’ve asked for too many codes recently.",
	}
	// emailVerificationGlobal: codes asked for by everyone.
	// Caps the mail when EMAIL_VERIFICATION_EXISTING=required sends every
	// unverified account to verify at its next sign-in.
	emailVerificationGlobal = ratelimit.Limit{
		Name:   "email-verification:global",
		Max:    500,
		Window: time.Hour,
		Busy:   "Email verification is busy. Try again in a few minutes.",
	}

	// apiPerUser: every signed-in write except the recovery routes (requireSession).
	apiPerUser = ratelimit.Limit{Name: "api:per-user", Max: 300, Window: 15 * time.Minute}
	// apiReadPerUser: every signed-in read.
	// About 3 a second sustained, with room for SWR's refreshes on focus.
	apiReadPerUser = ratelimit.Limit{Name: "api-read:per-user", Max: 1000, Window: 5 * time.Minute, InMemory: true}
	// adminReadPerUser: every admin dashboard read, per admin.
	// The admin pages' own budget, as api-read:per-user is the app's.
	adminReadPerUser = ratelimit.Limit{Name: "admin-read:per-user", Max: 1000, Window: 5 * time.Minute, InMemory: true}

	// workspacePerUser: workspaces created by one user.
	workspacePerUser = ratelimit.Limit{Name: "workspace:per-user", Max: 10, Window: time.Hour}
	// projectPerUser: projects created by one user, across all of their workspaces.
	projectPerUser = ratelimit.Limit{Name: "project:per-user", Max: 100, Window: time.Hour}

	// invitationPerUser: invitations sent by one user, across all of their workspaces.
	// Each one emails an address the inviter chooses.
	invitationPerUser = ratelimit.Limit{Name: "invitation:per-user", Max: 20, Window: time.Hour}
	// invitationPerEmail: invitations to one email, from any inviter and workspace.
	// Caps mail to one inbox. Counted for every address, registered or not, so
	// the message says nothing about who has an account.
	invitationPerEmail = ratelimit.Limit{
		Name:    "invitation:per-email",
		Max:     5,
		Window:  time.Hour,
		TooMany: "This email address has had too many invitations recently.",
	}
	// invitationGlobal: invitations sent by everyone.
	// Caps mail during an abuse wave.
	invitationGlobal = ratelimit.Limit{
		Name:   "invitation:global",
		Max:    200,
		Window: time.Hour,
		Busy:   "Invitations are busy. Try again later.",
	}
)

// rateLimits lists every limit, for TestRateLimitNames.
var rateLimits = []ratelimit.Limit{
	authPerIP, authPerAccount, mfaPerAccount,
	passwordResetPerIP, passwordResetPerEmail, passwordResetGlobal,
	registerPerIP, registerPerEmail, registerGlobal,
	emailCodePerEmail, emailVerificationPerEmail, emailVerificationGlobal,
	apiPerUser, apiReadPerUser, adminReadPerUser,
	workspacePerUser, projectPerUser,
	invitationPerUser, invitationPerEmail, invitationGlobal,
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
func (s *Server) allow(w http.ResponseWriter, r *http.Request, l ratelimit.Limit, key string) bool {
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
	w.Header().Set("Retry-After", ratelimit.RetryAfter(left))
	if l.Busy != "" {
		fail(w, 503, l.Busy)
	} else {
		fail(w, 429, l.Refusal(left))
	}
	return false
}

// count adds a hit to key's window, in memory or in the database.
func (s *Server) count(r *http.Request, l ratelimit.Limit, key string) (allowed, firstRefusal bool, resetsAt time.Time, err error) {
	if l.InMemory {
		allowed, firstRefusal, resetsAt = s.memoryLimits.Allow(key, l.Max, l.Window)
		return allowed, firstRefusal, resetsAt, nil
	}
	return s.Store.Allow(r.Context(), key, l.Max, l.Window)
}

// recordLimitReached records rate_limit.reached for l, with the user and the
// workspace when the request has them. Every limit has one level for now,
// block (the CAPTCHA plan adds challenge).
func (s *Server) recordLimitReached(r *http.Request, l ratelimit.Limit, resetsAt time.Time) error {
	if l.Busy != "" {
		slog.Warn("a rate limit shared by everyone was reached", "limit", l.Name, "window", l.Window)
	}
	e := events.RateLimitReached{Limit: l.Name + ":block", Window: int(l.Window.Seconds()), ResetsAt: resetsAt}
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
func (s *Server) limit(l ratelimit.Limit, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.allow(w, r, l, l.Key(current(r).User.PublicID)) {
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
	return s.allow(w, r, budget, budget.Key(current(r).User.PublicID))
}
