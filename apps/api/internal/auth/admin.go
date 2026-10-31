package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"time"
)

// Admin sessions (docs/api/admin.md) are separate from the app's: their own
// table and cookie, a shorter life, and tied to the ADMIN_ROUTE_KEY they
// were created under.
const (
	AdminSessionIdle     = 15 * time.Minute
	AdminSessionLifetime = 8 * time.Hour
	// AdminSetupLifetime is how long an admin without MFA has to scan the
	// QR code and enter the first code at the door: longer than a sign-in
	// code's MFAChallengeLifetime.
	AdminSetupLifetime = 15 * time.Minute
	// AdminSetupWindow is how long after admin grant (or an MFA reset) an
	// admin may set up MFA at the door.
	AdminSetupWindow = 24 * time.Hour
)

// AdminKeyMatches compares a key a request sent with the configured
// ADMIN_ROUTE_KEY in constant time, over equal-length digests, so neither
// its content nor its length shows in the timing. An empty key never
// matches: admin is off.
func AdminKeyMatches(given, key string) bool {
	if key == "" {
		return false
	}
	a, b := sha256.Sum256([]byte(given)), sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// AdminCookieName is the admin session's cookie (__Host- in production).
func AdminCookieName(production bool) string {
	if production {
		return "__Host-lewcontext_admin"
	}
	return "lewcontext_admin"
}

// AdminCookie carries an admin session token: HttpOnly and SameSite=Strict,
// so no request from another site carries it.
func AdminCookie(token string, production bool) *http.Cookie {
	return &http.Cookie{Name: AdminCookieName(production), Value: token, Path: "/", HttpOnly: true, Secure: production, SameSite: http.SameSiteStrictMode, MaxAge: int(AdminSessionLifetime.Seconds()), Expires: time.Now().Add(AdminSessionLifetime)}
}

// AdminMFACookieName holds an admin sign-in's challenge between its password
// and its code, apart from the app sign-in's (MFACookieName).
func AdminMFACookieName(production bool) string {
	if production {
		return "__Host-lewcontext_admin_mfa"
	}
	return "lewcontext_admin_mfa"
}

// AdminMFACookie carries an admin challenge token for lifetime: a sign-in
// code's 5 minutes, or the setup's 15.
func AdminMFACookie(token string, production bool, lifetime time.Duration) *http.Cookie {
	return &http.Cookie{Name: AdminMFACookieName(production), Value: token, Path: "/", HttpOnly: true, Secure: production, SameSite: http.SameSiteStrictMode, MaxAge: int(lifetime.Seconds()), Expires: time.Now().Add(lifetime)}
}
