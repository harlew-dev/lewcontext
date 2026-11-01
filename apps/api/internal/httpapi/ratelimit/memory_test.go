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
	for i := 1; i <= 4; i++ {
		hits, resetsAt := m.Count("a", time.Minute)
		if hits != i || !resetsAt.Equal(c.now.Add(time.Minute)) {
			t.Fatalf("hit %d counted as %d, resets %s", i, hits, resetsAt)
		}
	}
	// Another key has its own window.
	if hits, _ := m.Count("b", time.Minute); hits != 1 {
		t.Fatal("another key shares a window")
	}
	// The window ends: counting starts again.
	c.now = c.now.Add(time.Minute)
	if hits, resetsAt := m.Count("a", time.Minute); hits != 1 || !resetsAt.Equal(c.now.Add(time.Minute)) {
		t.Fatal("a new window didn't start at 1")
	}
}
func TestMemoryLimiterSweepsEndedWindows(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	m := NewMemory()
	m.now = c.Now
	m.Count("short", time.Minute)
	m.Count("long", time.Hour)
	c.now = c.now.Add(30 * time.Second)
	m.Count("long", time.Hour)
	if m.size() != 2 {
		t.Fatal("swept within a minute of the last sweep")
	}
	c.now = c.now.Add(2 * time.Minute)
	m.Count("long", time.Hour)
	if m.size() != 1 {
		t.Fatalf("an ended window wasn't swept: %d held", m.size())
	}
}

// Run with -race (make test).
func TestMemoryLimiterConcurrentUse(t *testing.T) {
	m := NewMemory()
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int]bool{}
	for range 50 {
		wg.Go(func() {
			for range 20 {
				hits, _ := m.Count("shared", time.Minute)
				mu.Lock()
				seen[hits] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	// Every hit got its own count, 1 to 1,000, so each level's crossing
	// happens exactly once.
	for i := 1; i <= 1000; i++ {
		if !seen[i] {
			t.Fatalf("no hit was counted %d of 1,000", i)
		}
	}
}
