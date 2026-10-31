package ratelimit

import (
	"testing"
	"time"
)

func TestRetryAfterAndWaitFor(t *testing.T) {
	for _, c := range []struct {
		left       time.Duration
		retryAfter string
		waitFor    string
	}{
		{0, "1", "a minute"},
		{300 * time.Millisecond, "1", "a minute"},
		{59 * time.Second, "59", "a minute"},
		{61 * time.Second, "61", "2 minutes"},
		{11*time.Minute + time.Second, "661", "12 minutes"},
		{15 * time.Minute, "900", "15 minutes"},
		{time.Hour - time.Second/2, "3600", "an hour"},
		{time.Hour + time.Second, "3601", "2 hours"},
		{24 * time.Hour, "86400", "24 hours"},
	} {
		if got := RetryAfter(c.left); got != c.retryAfter {
			t.Errorf("RetryAfter(%s) = %s, want %s", c.left, got, c.retryAfter)
		}
		if got := WaitFor(c.left); got != c.waitFor {
			t.Errorf("WaitFor(%s) = %q, want %q", c.left, got, c.waitFor)
		}
	}
	if got := (Limit{Attempts: true}).Refusal(90 * time.Second); got != "Too many attempts. Try again in 2 minutes." {
		t.Errorf("auth refusal: %q", got)
	}
	if got := (Limit{TooMany: "This email address has had too many invitations recently."}).Refusal(40 * time.Minute); got != "This email address has had too many invitations recently. Try again in 40 minutes." {
		t.Errorf("invitation refusal: %q", got)
	}
	if got := (Limit{}).Refusal(12 * time.Minute); got != "Too many requests. Try again in 12 minutes." {
		t.Errorf("refusal: %q", got)
	}
}

func TestLimitSubject(t *testing.T) {
	for ip, want := range map[string]string{
		"203.0.113.1":          "203.0.113.1",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::1": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":      "2001:db8:1:3::/64",
		"not an address":       "not an address",
	} {
		if got := subject(ip); got != want {
			t.Errorf("subject(%s) = %s, want %s", ip, got, want)
		}
	}
	// Two IPv6 addresses in one /64 share a key; two IPv4 addresses don't.
	l := Limit{Name: "auth:per-ip"}
	if l.IPKey("2001:db8::1") != l.IPKey("2001:db8::ffff:1") {
		t.Error("one /64 counted as two")
	}
	if l.IPKey("2001:db8::1") == l.IPKey("2001:db8:0:1::1") {
		t.Error("two /64s counted as one")
	}
	if l.IPKey("203.0.113.1") == l.IPKey("203.0.113.2") {
		t.Error("two IPv4 addresses counted as one")
	}
}
