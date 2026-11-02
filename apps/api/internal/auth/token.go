package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"
)

const SessionLifetime = 24 * time.Hour

func Token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func CookieName(production bool) string {
	if production {
		return "__Host-lewcontext_session"
	}
	return "lewcontext_session"
}
func Cookie(token string, production bool) *http.Cookie {
	return &http.Cookie{Name: CookieName(production), Value: token, Path: "/", HttpOnly: true, Secure: production, SameSite: http.SameSiteLaxMode, MaxAge: int(SessionLifetime.Seconds()), Expires: time.Now().Add(SessionLifetime)}
}

// MFAChallengeLifetime is how long a sign-in waits for its second factor.
const MFAChallengeLifetime = 5 * time.Minute

// MFACookieName is the cookie holding a sign-in's challenge token between its
// password and its code (__Host- in production, like the session's).
func MFACookieName(production bool) string {
	if production {
		return "__Host-lewcontext_mfa"
	}
	return "lewcontext_mfa"
}

// MFACookie carries a challenge token: HttpOnly, SameSite=Strict (only our
// own sign-in page sends it) and as short-lived as the challenge.
func MFACookie(token string, production bool) *http.Cookie {
	return &http.Cookie{Name: MFACookieName(production), Value: token, Path: "/", HttpOnly: true, Secure: production, SameSite: http.SameSiteStrictMode, MaxAge: int(MFAChallengeLifetime.Seconds()), Expires: time.Now().Add(MFAChallengeLifetime)}
}
