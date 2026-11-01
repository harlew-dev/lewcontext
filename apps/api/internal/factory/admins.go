package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"net/http"
	"time"
)

// AdminRouteKey is the ADMIN_ROUTE_KEY of servers under test, random per
// process: integrationServer passes it, and AdminSessions are made under it.
var AdminRouteKey = mustToken()

type Admin struct {
	bun.BaseModel `bun:"table:admins,alias:adm"`
	UserID        int64 `bun:",pk"`
	MFASetupUntil *time.Time
	Role          auth.AdminRole `bun:",notnull"`
	CreatedAt     time.Time      `bun:",nullzero,notnull,default:current_timestamp"`
}

// Admins builds viewers with no setup window (as when MFA is on), as admin
// grant makes them. Set UserID, or use AdminFor; AdminRole for an operator.
var Admins = Define(func(f *gofakeit.Faker) Admin {
	return Admin{Role: auth.AdminViewer}
})

// AdminRole gives the admin role instead of viewer.
func AdminRole(role auth.AdminRole) func(*Admin) {
	return func(a *Admin) { a.Role = role }
}

// AdminFor makes the user an admin.
func AdminFor(userID int64) func(*Admin) {
	return func(a *Admin) { a.UserID = userID }
}

// SetupWindow opens the 24-hour window to set up MFA at the door, as
// admin grant does for an account without MFA.
func SetupWindow(a *Admin) {
	until := time.Now().Add(auth.AdminSetupWindow)
	a.MFASetupUntil = &until
}

// ClosedSetupWindow is a window that ended a minute ago.
func ClosedSetupWindow(a *Admin) {
	until := time.Now().Add(-time.Minute)
	a.MFASetupUntil = &until
}

type AdminSession struct {
	bun.BaseModel `bun:"table:admin_sessions,alias:adms"`
	TokenHash     string    `bun:",pk"`
	UserID        int64     `bun:",notnull"`
	KeyHash       string    `bun:",notnull"`
	CSRFToken     string    `bun:"csrf_token,notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	LastSeenAt    time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
	IP            string    `bun:"ip,type:inet,notnull"`
	UserAgent     string    `bun:",notnull"`
	MovedAt       *time.Time
	// Token is the raw cookie value; only its digest is stored.
	Token string `bun:"-"`
}

// AdminSessions builds live admin sessions under AdminRouteKey, signed in
// from 127.0.0.1 (the address the integration tests' requests come from)
// with a fake browser. The user must be an admin (Admins); use
// AdminSessionFor.
var AdminSessions = Define(func(f *gofakeit.Faker) AdminSession {
	token, csrf := mustToken(), mustToken()
	now := time.Now()
	return AdminSession{
		TokenHash:  auth.Digest(token),
		KeyHash:    auth.Digest(AdminRouteKey),
		CSRFToken:  csrf,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(auth.AdminSessionLifetime),
		IP:         "127.0.0.1",
		UserAgent:  f.UserAgent(),
		Token:      token,
	}
})

// SignedInFrom is a session signed in from ip.
func SignedInFrom(ip string) func(*AdminSession) {
	return func(s *AdminSession) { s.IP = ip }
}

// AdminSessionFor makes the session the admin's.
func AdminSessionFor(userID int64) func(*AdminSession) {
	return func(s *AdminSession) { s.UserID = userID }
}

// AdminIdle has been unused for longer than the 15-minute idle timeout.
func AdminIdle(s *AdminSession) {
	s.LastSeenAt = time.Now().Add(-auth.AdminSessionIdle - time.Minute)
}

// AdminExpired is past its absolute expiry.
func AdminExpired(s *AdminSession) {
	s.ExpiresAt = time.Now().Add(-time.Second)
}

// UnderKey makes the session as if created under another ADMIN_ROUTE_KEY.
func UnderKey(key string) func(*AdminSession) {
	return func(s *AdminSession) { s.KeyHash = auth.Digest(key) }
}

// Cookie is the admin cookie a browser would send in development.
func (s AdminSession) Cookie() *http.Cookie {
	return auth.AdminCookie(s.Token, false)
}

// Purpose sets a challenge's purpose: "admin" or "admin_setup" for the
// admin door.
func Purpose(purpose string) func(*MFAChallenge) {
	return func(c *MFAChallenge) { c.Purpose = purpose }
}

// AdminCookie is the admin door's challenge cookie a browser would send in
// development.
func (c MFAChallenge) AdminCookie() *http.Cookie {
	return auth.AdminMFACookie(c.Token, false, auth.MFAChallengeLifetime)
}
