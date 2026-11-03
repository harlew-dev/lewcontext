package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

// BoundedMemory bounds simultaneously live anonymous subjects as well as expiry.
// A rotating list limits sweep work to 256 entries per access, without evicting
// a live window. Keys are normalized, hashed limit subjects, never raw headers.
type BoundedMemory struct {
	mu       sync.Mutex
	capacity int
	windows  map[string]*list.Element
	sweep    list.List
	now      func() time.Time
}

type boundedWindow struct {
	key  string
	hits int
	end  time.Time
}

func NewBoundedMemory(capacity int) *BoundedMemory {
	return &BoundedMemory{capacity: capacity, windows: make(map[string]*list.Element), now: time.Now}
}

// Count reserves/checks a subject, optionally incrementing only a failed guess.
// False means saturation, never an unbounded fallback or a cleared live block.
func (m *BoundedMemory) Count(key string, window time.Duration, increment bool) (int, time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for n := min(256, m.sweep.Len()); n > 0; n-- {
		e := m.sweep.Front()
		w := e.Value.(*boundedWindow)
		if !now.Before(w.end) {
			delete(m.windows, w.key)
			m.sweep.Remove(e)
		} else {
			m.sweep.MoveToBack(e)
		}
	}
	e := m.windows[key]
	if e == nil {
		if len(key) > 128 || len(m.windows) >= m.capacity {
			return 0, now.Add(time.Minute), false
		}
		e = m.sweep.PushBack(&boundedWindow{key: key, end: now.Add(window)})
		m.windows[key] = e
	}
	w := e.Value.(*boundedWindow)
	if !now.Before(w.end) {
		w.hits, w.end = 0, now.Add(window)
	}
	if increment {
		w.hits++
	}
	return w.hits, w.end, true
}
