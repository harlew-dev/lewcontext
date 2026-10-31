package httpapi

import (
	"lewcontext/api/internal/mail"
	"regexp"
	"sync"
	"testing"
	"time"
)

// Limit names are "<feature>:<counted by>" (docs/naming.md), and each is
// unique, since the name prefixes its counters' keys.
func TestRateLimitNames(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z]+(-[a-z]+)*:(per-ip|per-account|per-email|per-user|global)$`)
	seen := map[string]bool{}
	// The one limit counted in a listener, not a request.
	exists := mail.AccountExistsPerAccount
	for _, l := range append(rateLimits, rateLimit{name: exists.Name, limit: exists.Limit, window: exists.Window}) {
		if !valid.MatchString(l.name) {
			t.Errorf("%q isn't <feature>:<counted by>", l.name)
		}
		if seen[l.name] {
			t.Errorf("%q is declared twice", l.name)
		}
		seen[l.name] = true
		if l.limit < 1 || l.window < time.Minute {
			t.Errorf("%q: limit %d per %s", l.name, l.limit, l.window)
		}
		if (l.busy != "") != regexp.MustCompile(`:global$`).MatchString(l.name) {
			t.Errorf("%q: only limits shared by everyone answer 503 with a busy message", l.name)
		}
	}
}
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
		if got := retryAfter(c.left); got != c.retryAfter {
			t.Errorf("retryAfter(%s) = %s, want %s", c.left, got, c.retryAfter)
		}
		if got := waitFor(c.left); got != c.waitFor {
			t.Errorf("waitFor(%s) = %q, want %q", c.left, got, c.waitFor)
		}
	}
	if got := authPerIP.refusal(90 * time.Second); got != "Too many attempts. Try again in 2 minutes." {
		t.Errorf("auth refusal: %q", got)
	}
	if got := invitationPerEmail.refusal(40 * time.Minute); got != "This email address has had too many invitations recently. Try again in 40 minutes." {
		t.Errorf("invitation refusal: %q", got)
	}
	if got := apiPerUser.refusal(12 * time.Minute); got != "Too many requests. Try again in 12 minutes." {
		t.Errorf("refusal: %q", got)
	}
}
func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 20: "20", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, want %q", n, got, want)
		}
	}
}

// clock is a time the test moves by hand.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }
func TestMemoryLimiterWindows(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	m := newMemoryLimiter()
	m.now = c.Now
	for i := 1; i <= 3; i++ {
		if allowed, first, _ := m.allow("a", 3, time.Minute); !allowed || first {
			t.Fatalf("hit %d refused", i)
		}
	}
	allowed, first, resetsAt := m.allow("a", 3, time.Minute)
	if allowed || !first || !resetsAt.Equal(c.now.Add(time.Minute)) {
		t.Fatalf("fourth hit: allowed %v, first refusal %v, resets %s", allowed, first, resetsAt)
	}
	if allowed, first, _ := m.allow("a", 3, time.Minute); allowed || first {
		t.Fatal("fifth hit: only the first refusal is the first")
	}
	// Another key has its own window.
	if allowed, _, _ := m.allow("b", 3, time.Minute); !allowed {
		t.Fatal("another key refused")
	}
	// The window ends: counting starts again.
	c.now = c.now.Add(time.Minute)
	if allowed, _, resetsAt := m.allow("a", 3, time.Minute); !allowed || !resetsAt.Equal(c.now.Add(time.Minute)) {
		t.Fatal("a new window was refused")
	}
}
func TestMemoryLimiterSweepsEndedWindows(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	m := newMemoryLimiter()
	m.now = c.Now
	m.allow("short", 5, time.Minute)
	m.allow("long", 5, time.Hour)
	c.now = c.now.Add(30 * time.Second)
	m.allow("long", 5, time.Hour)
	if m.size() != 2 {
		t.Fatal("swept within a minute of the last sweep")
	}
	c.now = c.now.Add(2 * time.Minute)
	m.allow("long", 5, time.Hour)
	if m.size() != 1 {
		t.Fatalf("an ended window wasn't swept: %d held", m.size())
	}
}

// Run with -race (make test).
func TestMemoryLimiterConcurrentUse(t *testing.T) {
	m := newMemoryLimiter()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed, firsts := 0, 0
	for range 50 {
		wg.Go(func() {
			for range 20 {
				ok, first, _ := m.allow("shared", 600, time.Minute)
				mu.Lock()
				if ok {
					allowed++
				}
				if first {
					firsts++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 600 || firsts != 1 {
		t.Fatalf("1,000 hits against 600: %d allowed, %d first refusals", allowed, firsts)
	}
}
