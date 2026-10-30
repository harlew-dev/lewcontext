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
// keyed like the API's per-IP limiter.
var AuthAttempts = Define(func(f *gofakeit.Faker) AuthAttempt {
	return AuthAttempt{
		Key:      "ip:" + auth.Digest(f.IPv4Address()),
		Attempts: f.IntRange(1, 5),
		ResetsAt: time.Now().Add(15 * time.Minute),
	}
})

// ForAccount counts sign-in attempts for an email, like the per-account limiter.
func ForAccount(email string) func(*AuthAttempt) {
	return func(a *AuthAttempt) { a.Key = "account:" + auth.Digest(email) }
}

// ForIP counts attempts from a client address, like the per-IP limiter.
func ForIP(ip string) func(*AuthAttempt) {
	return func(a *AuthAttempt) { a.Key = "ip:" + auth.Digest(ip) }
}

// Exhausted uses up a limit, so the next attempt is refused.
func Exhausted(limit int) func(*AuthAttempt) {
	return func(a *AuthAttempt) { a.Attempts = limit }
}
