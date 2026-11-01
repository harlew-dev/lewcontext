package store

import (
	"testing"
	"time"
)

// A passed counter follows another counter's window: a new window starts
// again, and a late hit from an earlier one counts in the later one and
// gets the later window's end, never its own.
func TestCountHitUntil(t *testing.T) {
	s, ctx := queueStore(t, nil)
	now := time.Now().Truncate(time.Microsecond)
	first, second := now.Add(time.Hour), now.Add(2*time.Hour)
	count := func(resetsAt time.Time, hits int, ends time.Time) {
		t.Helper()
		got, gotEnds, err := s.CountHitUntil(ctx, "register:global:passed", resetsAt)
		if err != nil || got != hits || !gotEnds.Equal(ends) {
			t.Fatalf("CountHitUntil = %d, %s (%v); want %d, %s", got, gotEnds, err, hits, ends)
		}
	}
	peek := func(resetsAt time.Time, hits int) {
		t.Helper()
		if got, err := s.FindHits(ctx, "register:global:passed", resetsAt); err != nil || got != hits {
			t.Fatalf("FindHits = %d (%v), want %d", got, err, hits)
		}
	}
	peek(first, 0)
	count(first, 1, first)
	count(first, 2, first)
	peek(first, 2)
	count(second, 1, second)
	peek(second, 1)
	count(first, 2, second)
	peek(first, 2)
	peek(second.Add(time.Hour), 0)
}
