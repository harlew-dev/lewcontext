package httpapi

import (
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/mail"
	"regexp"
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
	for _, l := range append(RateLimits, ratelimit.Limit{Name: exists.Name, Block: ratelimit.Above(exists.Limit), Window: exists.Window}) {
		if !valid.MatchString(l.Name) {
			t.Errorf("%q isn't <feature>:<counted by>", l.Name)
		}
		if seen[l.Name] {
			t.Errorf("%q is declared twice", l.Name)
		}
		seen[l.Name] = true
		if !l.Block.Set() || l.Block.At() < 1 || l.Window < time.Minute {
			t.Errorf("%q: limit %d per %s", l.Name, l.Block.At(), l.Window)
		}
		if l.Challenge.Set() && l.Challenge.At() >= l.Block.At() {
			t.Errorf("%q: its challenge level isn't below its block level", l.Name)
		}
		if (l.Busy != "") != regexp.MustCompile(`:global$`).MatchString(l.Name) {
			t.Errorf("%q: only limits shared by everyone answer 503 with a busy message", l.Name)
		}
	}
}

// Every limit's levels, so changing one is deliberate (plans/features/
// implemented/captcha-challenges.md). 0 is Never.
func TestRateLimitLevels(t *testing.T) {
	want := map[string][2]int{
		"auth:per-ip":                  {5, 60},
		"auth:per-account":             {0, 10},
		"mfa:per-account":              {0, 5},
		"password-reset:per-ip":        {0, 10},
		"password-reset:per-email":     {0, 3},
		"password-reset:global":        {50, 100},
		"register:per-ip":              {0, 10},
		"register:per-email":           {0, 3},
		"register:global":              {200, 1000},
		"email-code:per-email":         {0, 10},
		"email-verification:per-email": {0, 3},
		"email-verification:global":    {0, 500},
		"api:per-user":                 {0, 300},
		"api-read:per-user":            {0, 1000},
		"admin-read:per-user":          {0, 1000},
		"workspace:per-user":           {0, 10},
		"project:per-user":             {0, 100},
		"invitation:per-user":          {0, 20},
		"invitation:per-email":         {0, 5},
		"invitation:global":            {100, 200},
		"admin-action:per-user":        {0, 60},
	}
	if len(RateLimits) != len(want) {
		t.Fatalf("%d limits, want %d", len(RateLimits), len(want))
	}
	for _, l := range RateLimits {
		levels, ok := want[l.Name]
		if !ok {
			t.Errorf("%q isn't in the table", l.Name)
			continue
		}
		if l.Challenge.Set() != (levels[0] > 0) || l.Challenge.At() != levels[0] || l.Block.At() != levels[1] {
			t.Errorf("%q: challenge %d, block %d; want %v", l.Name, l.Challenge.At(), l.Block.At(), levels)
		}
	}
	// PER_IP_LIMITS=off: every per-IP limit's levels go, nothing else changes.
	for _, l := range RateLimits {
		without := l.WithoutPerIP()
		if l.PerIP() != (without != l) || l.PerIP() && (without.Challenge.Set() || without.Block.Set()) {
			t.Errorf("%q without per-IP limits: %+v", l.Name, without)
		}
	}
}
