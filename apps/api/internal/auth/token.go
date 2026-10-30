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
		return "__Host-forma_session"
	}
	return "forma_session"
}
func Cookie(token string, production bool) *http.Cookie {
	return &http.Cookie{Name: CookieName(production), Value: token, Path: "/", HttpOnly: true, Secure: production, SameSite: http.SameSiteLaxMode, MaxAge: int(SessionLifetime.Seconds()), Expires: time.Now().Add(SessionLifetime)}
}
