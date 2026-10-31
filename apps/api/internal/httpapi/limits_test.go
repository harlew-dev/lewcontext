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
	for _, l := range append(RateLimits, ratelimit.Limit{Name: exists.Name, Max: exists.Limit, Window: exists.Window}) {
		if !valid.MatchString(l.Name) {
			t.Errorf("%q isn't <feature>:<counted by>", l.Name)
		}
		if seen[l.Name] {
			t.Errorf("%q is declared twice", l.Name)
		}
		seen[l.Name] = true
		if l.Max < 1 || l.Window < time.Minute {
			t.Errorf("%q: limit %d per %s", l.Name, l.Max, l.Window)
		}
		if (l.Busy != "") != regexp.MustCompile(`:global$`).MatchString(l.Name) {
			t.Errorf("%q: only limits shared by everyone answer 503 with a busy message", l.Name)
		}
	}
}
