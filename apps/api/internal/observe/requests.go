package observe

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// MaxRoutes is how many routes the request table holds before a new route
// is counted as Unmatched. Routes are registered patterns and fixed page
// labels, far fewer than this; the bound is a backstop.
const MaxRoutes = 300

// Requests counts every request by route: how many, by status class, how
// many timed out or were aborted, and how long they took. One row per route
// (request_minutes), rather than a series per metric and status, which is
// how the minute's flush stores it. Lock-free once a route has been seen.
type Requests struct {
	routes atomic.Pointer[map[string]*routeStats]
	mu     sync.Mutex
	bounds []int64
}

// routeStats is one route's running totals.
type routeStats struct {
	route    string
	requests atomic.Int64
	// statuses counts answers by class: 1 to 5 for 1xx–5xx; index 0 is
	// unused (an aborted request has no class).
	statuses [6]atomic.Int64
	// tooMany counts 429s, which are also 4xx.
	tooMany  atomic.Int64
	timeouts atomic.Int64
	aborted  atomic.Int64
	// sum is the requests' total duration, in nanoseconds.
	sum     atomic.Int64
	buckets [12]atomic.Int64
}

func newRequests() *Requests {
	q := &Requests{}
	for _, b := range DurationBounds {
		q.bounds = append(q.bounds, int64(b*float64(time.Millisecond)))
	}
	if len(q.bounds)+1 != len(routeStats{}.buckets) {
		panic("observe: request buckets don't match DurationBounds")
	}
	empty := map[string]*routeStats{}
	q.routes.Store(&empty)
	return q
}

// Observe counts one request: its route (a pattern or page label, never a
// path), the status sent, how long it took, whether the 10 s deadline ended
// it, and whether it was aborted (the client went away, or the connection
// was cut). An aborted request counts as a request and as aborted, never
// in a status class: it got no answer, and a client that left isn't a
// server error.
func (q *Requests) Observe(route string, status int, d time.Duration, timedOut, aborted bool) {
	s := q.lookup(route)
	n := int64(d)
	if n < 0 {
		n = 0
	}
	i, _ := slices.BinarySearch(q.bounds, n)
	s.buckets[i].Add(1)
	s.sum.Add(n)
	switch {
	case aborted:
		s.aborted.Add(1)
	case status >= 100 && status < 600:
		s.statuses[status/100].Add(1)
		if status == 429 {
			s.tooMany.Add(1)
		}
	}
	if timedOut {
		s.timeouts.Add(1)
	}
	s.requests.Add(1)
}

// lookup finds or adds a route's totals; past MaxRoutes, or for a route
// that isn't a plain label, it is Unmatched's.
func (q *Requests) lookup(route string) *routeStats {
	if s, ok := (*q.routes.Load())[route]; ok {
		return s
	}
	if !validLabelValue(route) || route == "" {
		route = Unmatched
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	current := *q.routes.Load()
	if s, ok := current[route]; ok {
		return s
	}
	if len(current) >= MaxRoutes {
		route = Unmatched
		if s, ok := current[route]; ok {
			return s
		}
	}
	s := &routeStats{route: route}
	next := make(map[string]*routeStats, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[route] = s
	q.routes.Store(&next)
	return s
}

// Len is how many routes have been seen.
func (q *Requests) Len() int {
	return len(*q.routes.Load())
}

// sorted is every route's totals, by route.
func (q *Requests) sorted() []*routeStats {
	current := *q.routes.Load()
	list := make([]*routeStats, 0, len(current))
	for _, s := range current {
		list = append(list, s)
	}
	slices.SortFunc(list, func(a, b *routeStats) int {
		switch {
		case a.route < b.route:
			return -1
		case a.route > b.route:
			return 1
		}
		return 0
	})
	return list
}

// RouteTotals is one route's totals at a moment, or the difference
// between two moments.
type RouteTotals struct {
	Route     string
	Requests  int64
	Status2xx int64
	Status3xx int64
	// Status4xx includes the 429s, which Status429 counts again.
	Status4xx int64
	Status429 int64
	Status5xx int64
	Timeouts  int64
	Aborted   int64
	// DurationSum is the requests' total duration.
	DurationSum time.Duration
	// Buckets count requests per DurationBounds bound, and one more above
	// them all (not cumulative).
	Buckets [12]int64
}

func (s *routeStats) totals() RouteTotals {
	t := RouteTotals{
		Route:       s.route,
		Requests:    s.requests.Load(),
		Status2xx:   s.statuses[2].Load(),
		Status3xx:   s.statuses[3].Load(),
		Status4xx:   s.statuses[4].Load(),
		Status429:   s.tooMany.Load(),
		Status5xx:   s.statuses[5].Load(),
		Timeouts:    s.timeouts.Load(),
		Aborted:     s.aborted.Load(),
		DurationSum: time.Duration(s.sum.Load()),
	}
	for i := range s.buckets {
		t.Buckets[i] = s.buckets[i].Load()
	}
	return t
}

// minus is t's change since an earlier reading.
func (t RouteTotals) minus(earlier RouteTotals) RouteTotals {
	d := RouteTotals{
		Route:       t.Route,
		Requests:    t.Requests - earlier.Requests,
		Status2xx:   t.Status2xx - earlier.Status2xx,
		Status3xx:   t.Status3xx - earlier.Status3xx,
		Status4xx:   t.Status4xx - earlier.Status4xx,
		Status429:   t.Status429 - earlier.Status429,
		Status5xx:   t.Status5xx - earlier.Status5xx,
		Timeouts:    t.Timeouts - earlier.Timeouts,
		Aborted:     t.Aborted - earlier.Aborted,
		DurationSum: t.DurationSum - earlier.DurationSum,
	}
	for i := range t.Buckets {
		d.Buckets[i] = t.Buckets[i] - earlier.Buckets[i]
	}
	return d
}
