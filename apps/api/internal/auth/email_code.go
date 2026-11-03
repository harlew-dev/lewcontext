package auth

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// Emailed codes prove an address belongs to whoever asked: a new sign-up,
// or an account verifying the email it already has. A code is 6 digits,
// works for 15 minutes and 5 tries, and is stored only as an HMAC
// (Keys.EmailCodeDigest).
const (
	EmailCodeLifetime    = 15 * time.Minute
	EmailCodeMaxAttempts = 5
	emailCodeDigits      = 6
)

// GenerateEmailCode returns a new code, every one of the million equally
// likely.
func GenerateEmailCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// NormalizeEmailCode turns a code as typed ("482 913", "482-913") into its
// stored form. ok is false for anything that can't be a code.
func NormalizeEmailCode(input string) (code string, ok bool) {
	if len(input) > 32 {
		return "", false
	}
	var b strings.Builder
	for _, c := range input {
		switch {
		case c == ' ' || c == '-':
			continue
		case c >= '0' && c <= '9':
			b.WriteRune(c)
		default:
			return "", false
		}
	}
	if b.Len() != emailCodeDigits {
		return "", false
	}
	return b.String(), true
}

// FormatEmailCode spaces a code for reading in an email: "482 913".
func FormatEmailCode(code string) string {
	if len(code) != emailCodeDigits {
		return code
	}
	return code[:3] + " " + code[3:]
}

// RegistrationCookieName is the cookie tying a sign-up waiting for its code
// to the browser that started it (__Host- in production, like the
// session's).
func RegistrationCookieName(production bool) string {
	if production {
		return "__Host-lewcontext_registration"
	}
	return "lewcontext_registration"
}

// RegistrationCookie carries a sign-up's token: HttpOnly, SameSite=Strict
// (only our own pages send it) and as short-lived as its code.
func RegistrationCookie(token string, production bool) *http.Cookie {
	return &http.Cookie{Name: RegistrationCookieName(production), Value: token, Path: "/", HttpOnly: true, Secure: production, SameSite: http.SameSiteStrictMode, MaxAge: int(EmailCodeLifetime.Seconds()), Expires: time.Now().Add(EmailCodeLifetime)}
}
