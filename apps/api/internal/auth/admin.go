package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"slices"
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
	// AdminStepUpWindow is how long a fresh code at POST /api/admin/step-up
	// lets an admin session take account-and-access actions (unblocking an
	// address): long enough for several in a row, as a TOTP code works
	// once per 30 seconds.
	AdminStepUpWindow = 5 * time.Minute
)

// AdminKeyMatch is which configured ADMIN_ROUTE_KEY a key a request sent
// is: none, the current one, or the one being retired
// (ADMIN_ROUTE_KEY_PREVIOUS), which only keeps the sessions made under it
// working.
type AdminKeyMatch int

const (
	AdminKeyNone AdminKeyMatch = iota
	AdminKeyCurrent
	AdminKeyPrevious
)

// MatchAdminKey compares a key a request sent with the current key and the
// previous one ("" when unset) in constant time, over equal-length digests,
// so neither its content nor its length shows in the timing. Both are
// compared every time, with no early exit, so the timing doesn't show which
// one matched either. An empty configured key never matches: with current
// empty, admin is off, and the previous key opens nothing either.
func MatchAdminKey(given, current, previous string) AdminKeyMatch {
	g, c, p := sha256.Sum256([]byte(given)), sha256.Sum256([]byte(current)), sha256.Sum256([]byte(previous))
	return matchAdminDigests(g[:], c[:], p[:], current != "", current != "" && previous != "")
}

// MatchAdminKeyHash is MatchAdminKey for a digest already made (Digest): an
// admin session's key_hash, against the current and previous keys'
// digests ("" when unset), both compared every time in constant time.
func MatchAdminKeyHash(keyHash, current, previous string) AdminKeyMatch {
	return matchAdminDigests([]byte(keyHash), []byte(current), []byte(previous), current != "", current != "" && previous != "")
}

// matchAdminDigests compares given with both digests before looking at
// either result; an unset key matches nothing. The two keys never match
// together (config refuses equal keys), and if they did the current wins.
func matchAdminDigests(given, current, previous []byte, haveCurrent, havePrevious bool) AdminKeyMatch {
	c := subtle.ConstantTimeCompare(given, current) & flag(haveCurrent)
	p := subtle.ConstantTimeCompare(given, previous) & flag(havePrevious)
	return AdminKeyMatch(subtle.ConstantTimeSelect(c, int(AdminKeyCurrent), p*int(AdminKeyPrevious)))
}

func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AdminRole is what an admin may do, set from the command line only
// (admin grant -role): a viewer reads every admin page, an operator may also
// act. Each action names the role it needs (RequireAdminRole).
type AdminRole string

const (
	AdminViewer   AdminRole = "viewer"
	AdminOperator AdminRole = "operator"
)

// AdminRoles are the roles, least access first.
var AdminRoles = []AdminRole{AdminViewer, AdminOperator}

// ParseAdminRole reads a role's name; false for anything else.
func ParseAdminRole(s string) (AdminRole, bool) {
	for _, r := range AdminRoles {
		if string(r) == s {
			return r, true
		}
	}
	return "", false
}

// Can reports whether an admin with role r may do what needs role need: an
// operator may do anything a viewer may. An unknown role may do nothing.
func (r AdminRole) Can(need AdminRole) bool {
	have, ok := slices.Index(AdminRoles, r), slices.Contains(AdminRoles, need)
	return have >= 0 && ok && have >= slices.Index(AdminRoles, need)
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
