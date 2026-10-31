package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// clock is a time the test moves by hand.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }
func TestMemoryLimiterWindows(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	m := NewMemory()
	m.now = c.Now
	for i := 1; i <= 3; i++ {
		if allowed, first, _ := m.Allow("a", 3, time.Minute); !allowed || first {
			t.Fatalf("hit %d refused", i)
		}
	}
	allowed, first, resetsAt := m.Allow("a", 3, time.Minute)
	if allowed || !first || !resetsAt.Equal(c.now.Add(time.Minute)) {
		t.Fatalf("fourth hit: allowed %v, first refusal %v, resets %s", allowed, first, resetsAt)
	}
	if allowed, first, _ := m.Allow("a", 3, time.Minute); allowed || first {
		t.Fatal("fifth hit: only the first refusal is the first")
	}
	// Another key has its own window.
	if allowed, _, _ := m.Allow("b", 3, time.Minute); !allowed {
		t.Fatal("another key refused")
	}
	// The window ends: counting starts again.
	c.now = c.now.Add(time.Minute)
	if allowed, _, resetsAt := m.Allow("a", 3, time.Minute); !allowed || !resetsAt.Equal(c.now.Add(time.Minute)) {
		t.Fatal("a new window was refused")
	}
}
func TestMemoryLimiterSweepsEndedWindows(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	m := NewMemory()
	m.now = c.Now
	m.Allow("short", 5, time.Minute)
	m.Allow("long", 5, time.Hour)
	c.now = c.now.Add(30 * time.Second)
	m.Allow("long", 5, time.Hour)
	if m.size() != 2 {
		t.Fatal("swept within a minute of the last sweep")
	}
	c.now = c.now.Add(2 * time.Minute)
	m.Allow("long", 5, time.Hour)
	if m.size() != 1 {
		t.Fatalf("an ended window wasn't swept: %d held", m.size())
	}
}

// Run with -race (make test).
func TestMemoryLimiterConcurrentUse(t *testing.T) {
	m := NewMemory()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed, firsts := 0, 0
	for range 50 {
		wg.Go(func() {
			for range 20 {
				ok, first, _ := m.Allow("shared", 600, time.Minute)
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
