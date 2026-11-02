// Package ratelimit holds what a rate limit is (its name, maximum and window),
// its keys and refusal messages, and the in-memory counter for the limits that
// skip the database. The limits themselves are declared in httpapi.
package ratelimit

import (
	"lewcontext/api/internal/auth"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Limit is a fixed-window limit, counted in the rate_limits table or, for
// reads, in this process's memory (Memory). Its name,
// "<feature>:<counted by>", is the key's prefix and, with the level reached,
// rate_limit.reached's limit.
// Limit is a limit on requests in fixed windows, with two optional levels
// (docs/api/rate-limits.md): above Challenge, a route that names a
// challenge action needs a solved CAPTCHA (428); above Block, every
// request is refused (429, or 503 with Busy). Either may be Never.
type Limit struct {
	Name      string
	Challenge Threshold
	Block     Threshold
	Window    time.Duration
	// Busy is the refusal message of a limit shared by everyone, answered
	// with 503 because it isn't the requester's doing. Limits per subject
	// leave it empty and answer 429.
	Busy string
	// Attempts makes a 429 say "Too many attempts" instead of "Too many
	// requests": for limits on signing in and resetting a password.
	Attempts bool
	// TooMany replaces a 429's "Too many requests." when what's counted
	// isn't the requester, so the message says what was refused.
	TooMany string
	// InMemory counts in this process only: approximate (each server counts
	// its own), but without a database write per request.
	InMemory bool
}

// Key is the rate_limits key for one subject (an IP, an email or a user's
// public ID), stored as a digest so the table holds no addresses or IDs.
// PerIP reports whether the limit counts client IPs (its name ends in
// ":per-ip").
func (l Limit) PerIP() bool {
	return strings.HasSuffix(l.Name, ":per-ip")
}

// Shared reports whether the limit is shared by everyone (its name ends in
// ":global"), so one requester can push it up for all.
func (l Limit) Shared() bool {
	return strings.HasSuffix(l.Name, ":global")
}

// WithoutPerIP is l with both levels Never when it counts client IPs, for
// PER_IP_LIMITS=off (a proxy that can't send the client's address), and l
// unchanged otherwise.
func (l Limit) WithoutPerIP() Limit {
	if l.PerIP() {
		l.Challenge, l.Block = Never, Never
	}
	return l
}

func (l Limit) Key(subject string) string {
	return l.Name + ":" + auth.Digest(subject)
}

// IPKey is the rate_limits key for a client address (ClientIP): the address
// itself for IPv4, its /64 for IPv6 (subject).
func (l Limit) IPKey(ip string) string {
	return l.Key(Subject(ip))
}

// Everyone is the rate_limits key of a limit shared by all requests.
func (l Limit) Everyone() string {
	return l.Name
}

// PassedKey is the counter, beside key's, of a shared limit's requests
// that got past its challenge level (Shared): its block level counts those,
// not the requests nobody solved.
func PassedKey(key string) string {
	return key + ":passed"
}

// Refusal is the 429 message for a window that ends in left.
func (l Limit) Refusal(left time.Duration) string {
	tooMany := "Too many requests."
	if l.Attempts {
		tooMany = "Too many attempts."
	}
	if l.TooMany != "" {
		tooMany = l.TooMany
	}
	return tooMany + " Try again in " + WaitFor(left) + "."
}

// RetryAfter is the Retry-After value for a window that ends in left: whole
// seconds, rounded up, and at least 1.
func RetryAfter(left time.Duration) string {
	seconds := int((left + time.Second - 1) / time.Second)
	return strconv.Itoa(max(seconds, 1))
}

// WaitFor says how long is left, rounded up to whole minutes: "a minute",
// "12 minutes" or "an hour", and past an hour to whole hours: "3 hours".
func WaitFor(left time.Duration) string {
	minutes := int((left + time.Minute - 1) / time.Minute)
	switch {
	case minutes <= 1:
		return "a minute"
	case minutes <= 60:
		if minutes == 60 {
			return "an hour"
		}
		return strconv.Itoa(minutes) + " minutes"
	default:
		return strconv.Itoa((minutes+59)/60) + " hours"
	}
}

// Subject is what a per-IP limit counts for ip: the address itself for
// IPv4, and its /64 for IPv6, since one IPv6 user usually holds a whole /64.
func Subject(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is6() {
		return ip
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}
