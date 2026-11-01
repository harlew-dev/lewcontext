package httpapi

import (
	"context"
	"time"
)

// Summaries bound the admin dashboard's aggregate queries (the Events
// page's charts) on this server: at most two at once, so a burst of wide
// charts can't take the database pool from the app, each with a deadline.
// Neither is a rate limit: admin-read:per-user still counts the request.
type Summaries struct {
	slots chan struct{}
	// Wait is how long a summary waits for a slot before it's skipped
	// (busy); Deadline how long it may run (too slow). Tests shorten them.
	Wait     time.Duration
	Deadline time.Duration
}

// Summary bounds, as docs/api/admin.md describes them.
const (
	summarySlots    = 2
	summaryWait     = time.Second
	summaryDeadline = 5 * time.Second
)

func newSummaries() *Summaries {
	return &Summaries{slots: make(chan struct{}, summarySlots), Wait: summaryWait, Deadline: summaryDeadline}
}

// Acquire takes a slot, waiting at most Wait (or until ctx ends). It
// returns the release, or false when none came free.
func (q *Summaries) Acquire(ctx context.Context) (release func(), ok bool) {
	timer := time.NewTimer(q.Wait)
	defer timer.Stop()
	select {
	case q.slots <- struct{}{}:
		return func() { <-q.slots }, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}
