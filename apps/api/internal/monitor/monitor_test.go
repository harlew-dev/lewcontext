package monitor

import (
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"slices"
	"strings"
	"testing"
	"time"
)

// Every signal is well formed: a unique name, a page, a unit, a level
// that can be reached, and a drill-down on a window the Events page takes,
// naming only catalogue names and groups.
func TestSignalsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	groups := events.GroupNames()
	units := []string{UnitCount, UnitPercent, UnitMs, UnitMinutes, UnitPerMin, UnitBytes}
	for _, sig := range Signals() {
		if sig.Name == "" || seen[sig.Name] || !strings.Contains(sig.Name, ".") {
			t.Errorf("%q: names are unique and dotted", sig.Name)
		}
		seen[sig.Name] = true
		if sig.Page == "" || sig.Label == "" || !slices.Contains(units, sig.Unit) || sig.Measure == nil || sig.Window <= 0 {
			t.Errorf("%s: page, label, unit, window and measure", sig.Name)
		}
		if sig.Warn < 0 || sig.Critical < 0 || sig.Warn > 0 && sig.Critical > 0 && sig.Warn >= sig.Critical {
			t.Errorf("%s: warn %v, critical %v", sig.Name, sig.Warn, sig.Critical)
		}
		if sig.Drill == nil {
			if sig.DrillExact {
				t.Errorf("%s: exact, with no drill-down", sig.Name)
			}
			continue
		}
		if windowName(sig.Window) == "" {
			t.Errorf("%s: a drill-down needs a window the Events page takes, not %v", sig.Name, sig.Window)
		}
		for key := range sig.Drill {
			if !slices.Contains([]string{"name", "outcome"}, key) {
				t.Errorf("%s: drill-down filter %q", sig.Name, key)
			}
		}
		for _, token := range strings.Split(sig.Drill.Get("name"), ",") {
			if !slices.Contains(events.Names(), token) && groups[token] == nil {
				t.Errorf("%s: %q is no event name or group", sig.Name, token)
			}
		}
	}
	// Every tile's signal is defined, and every page's signals are its own.
	for _, tile := range overviewTiles {
		for _, name := range tile.Signals {
			if !seen[name] {
				t.Errorf("tile %s: no signal %s", tile.ID, name)
			}
		}
	}
	for _, p := range pages[1:] {
		for _, sig := range p.Signals() {
			if sig.Page != p.Name {
				t.Errorf("%s is %s's, listed on %s", sig.Name, sig.Page, p.Name)
			}
		}
	}
	if len(overviewSignals()) == 0 || len(PageNames()) != 12 {
		t.Fatal("pages")
	}
}

// Thresholds are sized from the limits they watch: the shared limits the
// Security page reads exist, and the sign-up levels are halfway and all of
// register:global.
func TestThresholdsMatchTheLimits(t *testing.T) {
	env := Env{Limits: httpapi.RateLimits}
	for _, name := range []string{registerGlobal, passwordResetGlobal} {
		if _, ok := limitNamed(env, name); !ok || !slices.Contains(globalLimits(env), name) {
			t.Errorf("%s isn't a declared shared limit", name)
		}
	}
	if len(globalLimits(env)) < 4 {
		t.Fatalf("shared limits: %v", globalLimits(env))
	}
	for _, sig := range SecuritySignals {
		if sig.Name == "auth.sign_up_limit_used" && (sig.Warn != 50 || sig.Critical != 100) {
			t.Fatalf("sign-up levels: %+v", sig)
		}
	}
}

// Levels: at or over a threshold, the highest wins; under the minimum
// sample, ok whatever the value.
func TestLevelOf(t *testing.T) {
	sig := Signal{Warn: 50, Critical: 100, MinSample: 10}
	for _, c := range []struct {
		r    Reading
		want Level
	}{
		{Reading{49.9, 10}, LevelOK},
		{Reading{50, 10}, LevelWarn},
		{Reading{99.9, 10}, LevelWarn},
		{Reading{100, 10}, LevelCritical},
		{Reading{100, 9}, LevelOK},
	} {
		if got := sig.LevelOf(c.r); got != c.want {
			t.Errorf("%+v: %v, want %v", c.r, got, c.want)
		}
	}
	if (Signal{Critical: 1}).LevelOf(Reading{Value: 1}) != LevelCritical || (Signal{}).LevelOf(Reading{Value: 1e9}) != LevelOK {
		t.Fatal("no warning level, or none at all")
	}
}

// Percentiles from histogram counts: linear within the bucket, the last
// bound above it, zero for none.
func TestPercentile(t *testing.T) {
	counts := make([]int64, 12)
	if Percentile(counts, 0.95) != 0 {
		t.Fatal("empty")
	}
	counts[3], counts[8] = 900, 100 // ≤ 50 ms; 1–2.5 s
	if got := Percentile(counts, 0.95); got != 1750 {
		t.Fatalf("p95 %v", got)
	}
	if got := Percentile(counts, 0.5); got <= 25 || got > 50 {
		t.Fatalf("p50 %v", got)
	}
	counts[11] = 10000
	if got := Percentile(counts, 0.95); got != 10000 {
		t.Fatalf("over the last bound: %v", got)
	}
}

// A signal's span starts on its window's bucket, as the Events page's
// window of the same length does, and its drill-down names that window.
func TestSignalSpans(t *testing.T) {
	now := time.Date(2026, 11, 1, 14, 47, 31, 0, time.UTC)
	hour := Signal{Window: time.Hour}.SpanAt(now)
	if !hour.From.Equal(time.Date(2026, 11, 1, 13, 47, 0, 0, time.UTC)) || !hour.To.Equal(now) {
		t.Fatalf("hour: %+v", hour)
	}
	day := Signal{Window: 24 * time.Hour}.SpanAt(now)
	if !day.From.Equal(time.Date(2026, 10, 31, 14, 30, 0, 0, time.UTC)) {
		t.Fatalf("day: %+v", day)
	}
	if windowName(time.Hour) != "1h" || windowName(24*time.Hour) != "24h" || windowName(time.Minute) != "" {
		t.Fatal("window names")
	}
	if _, ok := ResolveWindow("30d", now); ok {
		t.Fatal("30d resolved")
	}
}

// One address's page takes an IP address (an IPv4-mapped one as IPv4, no
// zone) or an IPv6 /64, and offers an IPv6 address's /64.
func TestParseAddress(t *testing.T) {
	for _, c := range []struct{ raw, ip, network string }{
		{"203.0.113.66", "203.0.113.66", ""},
		{"::ffff:203.0.113.66", "203.0.113.66", ""},
		{"2001:db8:1:2::ffff", "2001:db8:1:2::ffff", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:aa::/64", "2001:db8:1:2::/64", ""},
	} {
		ip, network, ok := ParseAddress(c.raw)
		if !ok || ip != c.ip || network != c.network {
			t.Errorf("%s: %q %q %v", c.raw, ip, network, ok)
		}
	}
	for _, raw := range []string{"", "localhost", "203.0.113.0/24", "2001:db8::/48", "fe80::1%eth0", "::ffff:1.2.3.4/64", "1.2.3", "203.0.113.66 "} {
		if _, _, ok := ParseAddress(raw); ok {
			t.Errorf("%q accepted", raw)
		}
	}
}

// One address's page lists the live blocks holding the address, or part
// of its /64; others, and one that doesn't parse, aren't.
func TestBlocksCovering(t *testing.T) {
	end := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	live := []store.LiveIPBlock{
		{Network: "203.0.113.0/24", ExpiresAt: end},
		{Network: "203.0.113.66/32", ExpiresAt: end},
		{Network: "198.51.100.0/24", ExpiresAt: end},
		{Network: "2001:db8:1::/48", ExpiresAt: end},
		{Network: "2001:db8:1:2:aa::/128", ExpiresAt: end},
		{Network: "2001:db8:1:3::/64", ExpiresAt: end},
		{Network: "not a network", ExpiresAt: end},
	}
	networks := func(ip string) []string {
		var out []string
		for _, b := range blocksCovering(live, ip) {
			out = append(out, b.Network)
		}
		return out
	}
	for ip, want := range map[string][]string{
		"203.0.113.66":      {"203.0.113.0/24", "203.0.113.66/32"},
		"203.0.113.67":      {"203.0.113.0/24"},
		"192.0.2.1":         nil,
		"2001:db8:1:2::/64": {"2001:db8:1::/48", "2001:db8:1:2:aa::/128"},
		"2001:db8:1:2::9":   {"2001:db8:1::/48"},
		"2001:db8:2:2::/64": nil,
	} {
		if got := networks(ip); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", ip, got, want)
		}
	}
	if got := blocksCovering(nil, "192.0.2.1"); got == nil || len(got) != 0 {
		t.Errorf("no blocks: %#v, want an empty list", got)
	}
}
