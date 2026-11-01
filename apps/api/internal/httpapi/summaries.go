package httpapi

import (
	"context"
	"sync"
	"time"
)

// Summaries bound the admin dashboard's aggregate queries (the Events
// page's charts and the Monitoring pages) on this server: one at a time for
// each admin, at most two at once, so a burst of wide charts can't take the
// database pool from the app, each with a deadline.
// Neither is a rate limit: admin-read:per-user still counts the request.
type Summaries struct {
	slots chan struct{}
	// running holds the admins with a summary in flight.
	mu      sync.Mutex
	running map[string]bool
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
	return &Summaries{slots: make(chan struct{}, summarySlots), running: map[string]bool{}, Wait: summaryWait, Deadline: summaryDeadline}
}

// Acquire takes a slot for admin, waiting at most Wait (or until ctx
// ends). It returns the release, or false when none came free, or at once
// when admin already has a summary in flight.
func (q *Summaries) Acquire(ctx context.Context, admin string) (release func(), ok bool) {
	q.mu.Lock()
	if q.running[admin] {
		q.mu.Unlock()
		return nil, false
	}
	q.running[admin] = true
	q.mu.Unlock()
	done := func() {
		q.mu.Lock()
		delete(q.running, admin)
		q.mu.Unlock()
	}
	timer := time.NewTimer(q.Wait)
	defer timer.Stop()
	select {
	case q.slots <- struct{}{}:
		return func() { <-q.slots; done() }, true
	case <-timer.C:
	case <-ctx.Done():
	}
	done()
	return nil, false
}
