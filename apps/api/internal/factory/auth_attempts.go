package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"time"
)

type AuthAttempt struct {
	bun.BaseModel `bun:"table:auth_attempts,alias:a"`
	Key           string    `bun:",pk"`
	Attempts      int       `bun:",notnull"`
	ResetsAt      time.Time `bun:",notnull"`
}

// AuthAttempts builds rate-limit counters in the current 15-minute window,
// keyed like the API's auth:per-ip limit. Keys are "<limit name>:<digest>",
// as in httpapi/limits.go.
var AuthAttempts = Define(func(f *gofakeit.Faker) AuthAttempt {
	return AuthAttempt{
		Key:      "auth:per-ip:" + auth.Digest(f.IPv4Address()),
		Attempts: f.IntRange(1, 5),
		ResetsAt: time.Now().Add(15 * time.Minute),
	}
})

// ForAccount counts sign-in attempts for an email (auth:per-account).
func ForAccount(email string) func(*AuthAttempt) {
	return ForLimit("auth:per-account", email)
}

// ForIP counts sign-in attempts from a client address (auth:per-ip).
func ForIP(ip string) func(*AuthAttempt) {
	return ForLimit("auth:per-ip", ip)
}

// ForLimit counts attempts against the named limit for subject, an IP or an
// email; an empty subject is a limit across everyone, such as
// password-reset:all-accounts.
func ForLimit(name, subject string) func(*AuthAttempt) {
	if subject == "" {
		return func(a *AuthAttempt) { a.Key = name }
	}
	return func(a *AuthAttempt) { a.Key = name + ":" + auth.Digest(subject) }
}

// Exhausted uses up a limit, so the next attempt is refused.
func Exhausted(limit int) func(*AuthAttempt) {
	return func(a *AuthAttempt) { a.Attempts = limit }
}
