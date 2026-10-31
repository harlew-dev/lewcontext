package ratelimit

import (
	"sync"
	"time"
)

// Memory counts hits in fixed windows in this process's memory, for
// limits that would otherwise add a database write to every request (reads).
// Each server process counts on its own, so with N servers a user gets N
// times the limit: fine for load protection, not for anything that must be
// exact. Ended windows are swept at most once a minute, on the next hit.
type Memory struct {
	mu        sync.Mutex
	windows   map[string]memoryWindow
	lastSweep time.Time
	// now is time.Now, replaceable in tests.
	now func() time.Time
}
type memoryWindow struct {
	hits     int
	resetsAt time.Time
}

// NewMemory is an empty counter.
func NewMemory() *Memory {
	return &Memory{windows: map[string]memoryWindow{}, now: time.Now}
}

// Allow counts a hit against key, like Store.Allow: allowed while the window
// holds at most limit hits, and firstRefusal for the hit that first goes over.
func (m *Memory) Allow(key string, limit int, window time.Duration) (allowed, firstRefusal bool, resetsAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.sweep(now)
	w, ok := m.windows[key]
	if !ok || !now.Before(w.resetsAt) {
		w = memoryWindow{resetsAt: now.Add(window)}
	}
	w.hits++
	m.windows[key] = w
	return w.hits <= limit, w.hits == limit+1, w.resetsAt
}

// sweep drops ended windows, at most once a minute. The caller holds mu.
func (m *Memory) sweep(now time.Time) {
	if now.Sub(m.lastSweep) < time.Minute {
		return
	}
	m.lastSweep = now
	for key, w := range m.windows {
		if !now.Before(w.resetsAt) {
			delete(m.windows, key)
		}
	}
}

// size is how many windows are held, for tests.
func (m *Memory) size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.windows)
}
