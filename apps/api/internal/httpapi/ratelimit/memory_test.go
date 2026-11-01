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

// A passed counter keeps another key's window: it starts again when that
// window is new, and a late hit from an earlier one counts in the later,
// with the later window's end.
func TestMemoryCountUntil(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	m := NewMemory()
	m.now = c.Now
	first := c.now.Add(time.Hour)
	count := func(resetsAt time.Time, hits int, ends time.Time) {
		t.Helper()
		if got, gotEnds := m.CountUntil("k", resetsAt); got != hits || !gotEnds.Equal(ends) {
			t.Fatalf("CountUntil = %d, %s; want %d, %s", got, gotEnds, hits, ends)
		}
	}
	if m.Peek("k", first) != 0 {
		t.Fatal("an empty counter")
	}
	count(first, 1, first)
	count(first, 2, first)
	if m.Peek("k", first) != 2 {
		t.Fatal("Peek in one window")
	}
	second := first.Add(time.Hour)
	if m.Peek("k", second) != 0 {
		t.Fatal("Peek in a new window")
	}
	count(second, 1, second)
	// A late hit from the earlier window counts in the later one.
	count(first, 2, second)
	if m.Peek("k", first) != 2 {
		t.Fatal("Peek from the earlier window")
	}
}
