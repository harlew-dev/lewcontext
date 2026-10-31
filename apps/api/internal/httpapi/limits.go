package httpapi

import (
	"lewcontext/api/internal/auth"
	"time"
)

// rateLimit is a fixed-window limit counted in auth_attempts. Its name,
// "<feature>:<counted by>", is the key's prefix and auth.rate_limited's scope.
type rateLimit struct {
	name   string
	limit  int
	window time.Duration
	// busy is the refusal message of a limit shared by everyone, answered
	// with 503 because it isn't the requester's doing. Limits per IP or
	// email leave it empty and answer 429.
	busy string
}

var (
	// Everything that signs in or checks a password, per connection IP.
	authPerIP = rateLimit{name: "auth:per-ip", limit: 60, window: 15 * time.Minute}
	// Password guesses against one account, from any IP.
	authPerAccount = rateLimit{name: "auth:per-account", limit: 10, window: 15 * time.Minute}
	// Reset links requested from one IP, so it can't mail many people.
	passwordResetPerIP = rateLimit{name: "password-reset:per-ip", limit: 10, window: time.Hour}
	// Reset links requested for one email, from any IP: caps mail to one inbox.
	passwordResetPerEmail = rateLimit{name: "password-reset:per-email", limit: 3, window: time.Hour}
	// Reset links requested for any account: caps mail during a distributed flood.
	passwordResetAllAccounts = rateLimit{
		name:   "password-reset:all-accounts",
		limit:  100,
		window: time.Hour,
		busy:   "Password reset is busy. Try again later.",
	}
)

// key is the auth_attempts key for one subject (an IP or an email), stored as
// a digest so the table holds no addresses.
func (l rateLimit) key(subject string) string {
	return l.name + ":" + auth.Digest(subject)
}

// everyone is the auth_attempts key of a limit shared by all requests.
func (l rateLimit) everyone() string {
	return l.name
}
