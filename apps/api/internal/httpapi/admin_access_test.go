package httpapi

import (
	"fmt"
	"testing"
	"time"
)

// A refused address is recorded once per window, and someone changing
// address can't grow the set past its cap: once full, nothing more is
// recorded until entries end.
func TestOnceEvery(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	o := newOnceEvery(15*time.Minute, 3)
	if !o.first("a", now) || o.first("a", now.Add(14*time.Minute)) {
		t.Fatal("a key again inside its window")
	}
	if !o.first("a", now.Add(15*time.Minute)) {
		t.Fatal("a key once its window ended")
	}
	if !o.first("b", now) || !o.first("c", now) {
		t.Fatal("new keys below the cap")
	}
	// Full: a new key is refused rather than held, and the set stays at 3.
	for i := range 100 {
		if o.first(fmt.Sprint("x", i), now.Add(time.Minute)) {
			t.Fatalf("key %d past the cap", i)
		}
	}
	if len(o.until) != 3 {
		t.Fatalf("the set grew to %d", len(o.until))
	}
	// Once b and c have ended (a was renewed at +15m), a sweep makes room.
	if !o.first("d", now.Add(16*time.Minute)) || len(o.until) > 3 {
		t.Fatalf("no room once entries ended: %d held", len(o.until))
	}
	// Sweeps run at most once a minute, so a full set costs no pass over it
	// on every call.
	o = newOnceEvery(time.Minute, 2)
	o.first("a", now)
	o.first("b", now.Add(30*time.Second))
	if !o.first("c", now.Add(61*time.Second)) {
		t.Fatal("a ended, so c fits after a sweep")
	}
	if o.first("d", now.Add(91*time.Second)) {
		t.Fatal("swept again within the minute")
	}
	if !o.first("d", now.Add(122*time.Second)) {
		t.Fatal("a sweep a minute later makes room")
	}
}

func TestAdminNetwork(t *testing.T) {
	cases := map[string]string{
		"198.51.100.20":       "198.51.100.0/24",
		"::ffff:198.51.100.1": "198.51.100.0/24",
		"2001:db8:1:2::1":     "2001:db8:1::/48",
		"2001:db8:1:ffff::9":  "2001:db8:1::/48",
		"fe80::1%eth0":        "fe80::/48",
		"not an address":      "invalid Prefix",
	}
	for ip, want := range cases {
		if got := adminNetwork(ip).String(); got != want {
			t.Errorf("%s: %s, want %s", ip, got, want)
		}
	}
}

// The refusal queue never blocks a request: past its size, refusals are
// dropped and counted.
func TestAdminRefusalQueueDropsWhenFull(t *testing.T) {
	a := newAdminRefusals()
	for i := range adminRefusalQueued + 5 {
		a.enqueue(adminRefusal{ip: fmt.Sprint("198.51.100.", i)})
	}
	if len(a.queue) != adminRefusalQueued {
		t.Fatalf("queued %d", len(a.queue))
	}
	// The first drop is logged (and the count reset); the rest wait a minute.
	if a.dropped != 4 || a.logged.IsZero() {
		t.Fatalf("dropped %d, logged %v", a.dropped, a.logged)
	}
}
