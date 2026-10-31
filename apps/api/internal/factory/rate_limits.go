package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"time"
)

type RateLimit struct {
	bun.BaseModel `bun:"table:rate_limits,alias:l"`
	Key           string    `bun:",pk"`
	Hits          int       `bun:",notnull"`
	ResetsAt      time.Time `bun:",notnull"`
}

// RateLimits builds rate-limit counters in the current 15-minute window,
// keyed like the API's auth:per-ip limit. Keys are "<limit name>:<digest>",
// as in httpapi/limits.go.
var RateLimits = Define(func(f *gofakeit.Faker) RateLimit {
	return RateLimit{
		Key:      "auth:per-ip:" + auth.Digest(f.IPv4Address()),
		Hits:     f.IntRange(1, 5),
		ResetsAt: time.Now().Add(15 * time.Minute),
	}
})

// ForAccount counts sign-in attempts for an email (auth:per-account).
func ForAccount(email string) func(*RateLimit) {
	return ForLimit("auth:per-account", email)
}

// ForIP counts sign-in attempts from a client address (auth:per-ip).
func ForIP(ip string) func(*RateLimit) {
	return ForLimit("auth:per-ip", ip)
}

// ForLimit counts hits against the named limit for subject: an IP, an email
// or a user's public ID. An empty subject is a limit shared by everyone, such
// as password-reset:global.
func ForLimit(name, subject string) func(*RateLimit) {
	if subject == "" {
		return func(l *RateLimit) { l.Key = name }
	}
	return func(l *RateLimit) { l.Key = name + ":" + auth.Digest(subject) }
}

// Exhausted uses up a limit, so the next request is refused.
func Exhausted(limit int) func(*RateLimit) {
	return func(l *RateLimit) { l.Hits = limit }
}
