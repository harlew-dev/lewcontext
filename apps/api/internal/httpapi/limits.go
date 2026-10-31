package httpapi

import (
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"time"
)

var (
	// AuthPerIP: everything that signs in or checks a password, per client IP.
	AuthPerIP = ratelimit.Limit{Name: "auth:per-ip", Max: 60, Window: 15 * time.Minute, Attempts: true}
	// AuthPerAccount: password guesses against one account, from any IP.
	AuthPerAccount = ratelimit.Limit{Name: "auth:per-account", Max: 10, Window: 15 * time.Minute, Attempts: true}
	// MFAPerAccount: second-factor codes checked for one account, from any IP.
	// Signing in, turning MFA on or off, new recovery codes. With one step of
	// drift a guess covers 3 codes in a million, so this caps an attacker who
	// already has the password at about 0.14% a day.
	MFAPerAccount = ratelimit.Limit{Name: "mfa:per-account", Max: 5, Window: 15 * time.Minute, Attempts: true}

	// PasswordResetPerIP: reset links requested from one IP.
	// So one IP can't mail many people.
	PasswordResetPerIP = ratelimit.Limit{Name: "password-reset:per-ip", Max: 10, Window: time.Hour, Attempts: true}
	// PasswordResetPerEmail: reset links requested for one email, from any IP.
	// Caps mail to one inbox.
	PasswordResetPerEmail = ratelimit.Limit{Name: "password-reset:per-email", Max: 3, Window: time.Hour, Attempts: true}
	// PasswordResetGlobal: reset links requested for any account.
	// Caps mail during a distributed flood.
	PasswordResetGlobal = ratelimit.Limit{
		Name:   "password-reset:global",
		Max:    100,
		Window: time.Hour,
		Busy:   "Password reset is busy. Try again later.",
	}

	// RegisterPerIP: accounts created from one IP.
	RegisterPerIP = ratelimit.Limit{Name: "register:per-ip", Max: 10, Window: time.Hour}
	// RegisterPerEmail: sign-ups for one email, from any IP, with verification on.
	// Each one emails the address, a code or "you already have an account".
	// Counted for every address alike, so a refusal says nothing about who has
	// an account.
	RegisterPerEmail = ratelimit.Limit{
		Name:    "register:per-email",
		Max:     3,
		Window:  time.Hour,
		TooMany: "This email address has had too many sign-up attempts recently.",
	}
	// RegisterGlobal: accounts created by everyone.
	// Caps sign-ups during a distributed flood.
	RegisterGlobal = ratelimit.Limit{
		Name:   "register:global",
		Max:    1000,
		Window: time.Hour,
		Busy:   "Sign-up is busy. Try again later.",
	}

	// EmailCodePerEmail: emailed codes checked for one email, from any browser or IP.
	// Signing up, or verifying an account's email. Each code also ends after 5
	// wrong tries, so guessing one is about 1 in 100,000 a day.
	EmailCodePerEmail = ratelimit.Limit{Name: "email-code:per-email", Max: 10, Window: 24 * time.Hour, Attempts: true}
	// EmailVerificationPerEmail: codes a signed-in account asks for to verify its email.
	// Each one emails the account's address, so this caps mail to one inbox.
	EmailVerificationPerEmail = ratelimit.Limit{
		Name:    "email-verification:per-email",
		Max:     3,
		Window:  time.Hour,
		TooMany: "You’ve asked for too many codes recently.",
	}
	// EmailVerificationGlobal: codes asked for by everyone.
	// Caps the mail when EMAIL_VERIFICATION_EXISTING=required sends every
	// unverified account to verify at its next sign-in.
	EmailVerificationGlobal = ratelimit.Limit{
		Name:   "email-verification:global",
		Max:    500,
		Window: time.Hour,
		Busy:   "Email verification is busy. Try again in a few minutes.",
	}

	// APIPerUser: every signed-in write except the recovery routes (RequireSession).
	APIPerUser = ratelimit.Limit{Name: "api:per-user", Max: 300, Window: 15 * time.Minute}
	// APIReadPerUser: every signed-in read.
	// About 3 a second sustained, with room for SWR's refreshes on focus.
	APIReadPerUser = ratelimit.Limit{Name: "api-read:per-user", Max: 1000, Window: 5 * time.Minute, InMemory: true}
	// AdminReadPerUser: every admin dashboard read, per admin.
	// The admin pages' own budget, as api-read:per-user is the app's.
	AdminReadPerUser = ratelimit.Limit{Name: "admin-read:per-user", Max: 1000, Window: 5 * time.Minute, InMemory: true}

	// WorkspacePerUser: workspaces created by one user.
	WorkspacePerUser = ratelimit.Limit{Name: "workspace:per-user", Max: 10, Window: time.Hour}
	// ProjectPerUser: projects created by one user, across all of their workspaces.
	ProjectPerUser = ratelimit.Limit{Name: "project:per-user", Max: 100, Window: time.Hour}

	// InvitationPerUser: invitations sent by one user, across all of their workspaces.
	// Each one emails an address the inviter chooses.
	InvitationPerUser = ratelimit.Limit{Name: "invitation:per-user", Max: 20, Window: time.Hour}
	// InvitationPerEmail: invitations to one email, from any inviter and workspace.
	// Caps mail to one inbox. Counted for every address, registered or not, so
	// the message says nothing about who has an account.
	InvitationPerEmail = ratelimit.Limit{
		Name:    "invitation:per-email",
		Max:     5,
		Window:  time.Hour,
		TooMany: "This email address has had too many invitations recently.",
	}
	// InvitationGlobal: invitations sent by everyone.
	// Caps mail during an abuse wave.
	InvitationGlobal = ratelimit.Limit{
		Name:   "invitation:global",
		Max:    200,
		Window: time.Hour,
		Busy:   "Invitations are busy. Try again later.",
	}
)

// RateLimits lists every limit, for TestRateLimitNames.
var RateLimits = []ratelimit.Limit{
	AuthPerIP, AuthPerAccount, MFAPerAccount,
	PasswordResetPerIP, PasswordResetPerEmail, PasswordResetGlobal,
	RegisterPerIP, RegisterPerEmail, RegisterGlobal,
	EmailCodePerEmail, EmailVerificationPerEmail, EmailVerificationGlobal,
	APIPerUser, APIReadPerUser, AdminReadPerUser,
	WorkspacePerUser, ProjectPerUser,
	InvitationPerUser, InvitationPerEmail, InvitationGlobal,
}

// Allow counts a request against l under key and answers 429 (503 with busy
// for a limit shared by everyone) when it is used up, with Retry-After set to
// the time left in the window. The first refusal in a window records
// rate_limit.reached; the ones after it don't, so a flood writes one event.
// With RATE_LIMITS=off it allows everything and counts nothing.
func (s *Server) Allow(w http.ResponseWriter, r *http.Request, l ratelimit.Limit, key string) bool {
	if s.Config.RateLimits == config.RateLimitsOff {
		return true
	}
	allowed, firstRefusal, resetsAt, err := s.count(r, l, key)
	if err == nil && firstRefusal {
		err = s.recordLimitReached(r, l, resetsAt)
	}
	if err != nil {
		InternalError(w, err)
		return false
	}
	if allowed {
		return true
	}
	left := time.Until(resetsAt)
	w.Header().Set("Retry-After", ratelimit.RetryAfter(left))
	if l.Busy != "" {
		Fail(w, 503, l.Busy)
	} else {
		Fail(w, 429, l.Refusal(left))
	}
	return false
}

// count adds a hit to key's window, in memory or in the database.
func (s *Server) count(r *http.Request, l ratelimit.Limit, key string) (allowed, firstRefusal bool, resetsAt time.Time, err error) {
	if l.InMemory {
		allowed, firstRefusal, resetsAt = s.memoryLimits.Allow(key, l.Max, l.Window)
		return allowed, firstRefusal, resetsAt, nil
	}
	return s.Repo.Allow(r.Context(), key, l.Max, l.Window)
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
	if session, ok := OptionalSession(r); ok {
		e.User, scope.UserID = session.User.PublicID, session.User.ID
	}
	if admin, ok := OptionalAdmin(r); ok {
		e.User, scope.UserID = admin.User.PublicID, admin.User.ID
	}
	if workspace, ok := OptionalWorkspace(r); ok {
		e.Workspace, scope.WorkspaceID = workspace.PublicID, workspace.ID
	}
	return s.Repo.Record(r.Context(), e, scope)
}

// Limit counts each request against l for the session's user before next.
// Routes that create rows or send mail name theirs in router/routes.go, next to the
// permission; every signed-in route also has a budget (meter).
func (s *Server) Limit(l ratelimit.Limit, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Allow(w, r, l, l.Key(Session(r).User.PublicID)) {
			next(w, r)
		}
	}
}

// meter counts a signed-in request against its user's budget: writes in the
// database (api:per-user), reads in memory (api-read:per-user). Counted after
// the CSRF check, so another site can't spend someone's budget.
func (s *Server) meter(w http.ResponseWriter, r *http.Request) bool {
	budget := APIReadPerUser
	if unsafeMethod(r.Method) {
		budget = APIPerUser
	}
	return s.Allow(w, r, budget, budget.Key(Session(r).User.PublicID))
}

func (s *Server) AccountLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	return s.Allow(w, r, AuthPerAccount, AuthPerAccount.Key(email))
}
