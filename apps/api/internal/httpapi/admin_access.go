package httpapi

import (
	"context"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// Where admin requests may come from (docs/api/admin.md): ADMIN_ALLOWED_IPS,
// and noticing an admin session used from another network.

// adminRefusalWindow is how long a refused address isn't recorded again,
// and adminRefusalsHeld how many such addresses a server remembers at
// once: someone holding the key who keeps changing address (an IPv6 /64
// each time) can't grow the set, or the events table, without bound.
const (
	adminRefusalWindow = 15 * time.Minute
	adminRefusalsHeld  = 10_000
)

// adminAddressAllowed reports whether the request's client address
// (ClientIP) may reach the admin dashboard: any, without ADMIN_ALLOWED_IPS.
// Call it only once the request has proved it holds the key (or an assets
// pass): a refusal means the key is somewhere it shouldn't be, so it is
// logged and recorded (admin.access_refused), at most once per address per
// adminRefusalWindow on this server.
func (s *Server) adminAddressAllowed(r *http.Request) bool {
	if len(s.Config.AdminAllowedIPs) == 0 {
		return true
	}
	ip := ClientIP(r)
	if addr, err := netip.ParseAddr(ip); err == nil {
		addr = addr.Unmap().WithZone("")
		for _, allowed := range s.Config.AdminAllowedIPs {
			if allowed.Contains(addr) {
				return true
			}
		}
	}
	s.refuseAdminAddress(r, ip)
	return false
}

// refuseAdminAddress queues a refused address for logging and recording,
// the first time in adminRefusalWindow (an IPv6 address counts by its /64,
// as per-IP limits do). The request never waits for it: one worker
// (adminRefusals) writes the event, so a right key from outside the list
// takes as long as a wrong one.
func (s *Server) refuseAdminAddress(r *http.Request, ip string) {
	if !s.adminRefusals.seen.first(ratelimit.Subject(ip), time.Now()) {
		return
	}
	s.adminRefusals.enqueue(adminRefusal{ip: ip, userAgent: events.UserAgent(r.Context()), requestID: observe.RequestID(r.Context())})
}

// adminRefusal is one refused address to record: the request's client
// address, browser and ID, for the event's columns and the log line.
type adminRefusal struct {
	ip, userAgent, requestID string
}

// adminRefusalQueued is how many refusals may wait for the worker; more are
// dropped (and counted in a log line at most once a minute), so a burst of
// new addresses can't hold database connections real requests need.
const adminRefusalQueued = 64

// adminRefusals records refused addresses off the request path: a bounded
// queue and one worker, stopped (after draining what's queued) by
// StopAdminRefusals on shutdown.
type adminRefusals struct {
	seen    *onceEvery
	queue   chan adminRefusal
	stop    chan struct{}
	done    chan struct{}
	stopped sync.Once
	mu      sync.Mutex
	dropped int
	logged  time.Time
}

func newAdminRefusals() *adminRefusals {
	return &adminRefusals{
		seen:  newOnceEvery(adminRefusalWindow, adminRefusalsHeld),
		queue: make(chan adminRefusal, adminRefusalQueued),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// enqueue hands a refusal to the worker, or drops it when the queue is full.
func (a *adminRefusals) enqueue(refusal adminRefusal) {
	select {
	case a.queue <- refusal:
	default:
		a.drop(time.Now())
	}
}

// drop counts a refusal the queue had no room for, and says so at most once
// a minute.
func (a *adminRefusals) drop(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropped++
	if now.Sub(a.logged) < time.Minute {
		return
	}
	slog.Warn("admin.access_refused events dropped: too many refused addresses at once", "dropped", a.dropped)
	a.logged, a.dropped = now, 0
}

// runAdminRefusals is the worker: it logs and records each queued refusal,
// one at a time, until StopAdminRefusals, then records what's still queued.
func (s *Server) runAdminRefusals() {
	defer close(s.adminRefusals.done)
	for {
		select {
		case refusal := <-s.adminRefusals.queue:
			s.recordAdminRefusal(refusal)
		case <-s.adminRefusals.stop:
			for {
				select {
				case refusal := <-s.adminRefusals.queue:
					s.recordAdminRefusal(refusal)
				default:
					return
				}
			}
		}
	}
}

// recordAdminRefusal logs a refused address (never the path: an admin page's
// holds the key) and records admin.access_refused with it in events.ip.
// Both carry the refused request's ID, so its access line is found too.
func (s *Server) recordAdminRefusal(refusal adminRefusal) {
	slog.Warn("the admin key (or an assets pass) came from an address outside ADMIN_ALLOWED_IPS: it may have leaked", "ip", refusal.ip, observe.RequestIDKey, refusal.requestID)
	// Plan A5 (alerts) raises an alert on the first refusal, here.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = events.WithUserAgent(events.WithRequest(ctx, refusal.ip, refusal.requestID), refusal.userAgent)
	e := events.AdminAccessRefused{Reason: events.AdminRefusedIP}
	if err := s.Repo.Record(ctx, e, store.EventScope{}); err != nil {
		slog.Error("recording admin.access_refused failed", "error", err, observe.RequestIDKey, refusal.requestID)
	}
}

// StopAdminRefusals stops the refusal worker once it has recorded what's
// queued, or when ctx ends: the server's graceful shutdown calls it after
// the HTTP server has stopped taking requests. Later refusals aren't
// recorded.
func (s *Server) StopAdminRefusals(ctx context.Context) {
	s.adminRefusals.stopped.Do(func() { close(s.adminRefusals.stop) })
	select {
	case <-s.adminRefusals.done:
	case <-ctx.Done():
	}
}

// onceEvery says whether something keyed by key should happen now: the
// first time, and again once window has passed. It holds at most max keys;
// when full it drops ended ones (at most once a minute) and, if still full,
// answers false rather than grow.
type onceEvery struct {
	mu        sync.Mutex
	window    time.Duration
	max       int
	until     map[string]time.Time
	lastSweep time.Time
}

func newOnceEvery(window time.Duration, max int) *onceEvery {
	return &onceEvery{window: window, max: max, until: map[string]time.Time{}}
}

// first records key at now and reports whether it was due.
func (o *onceEvery) first(key string, now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	until, held := o.until[key]
	if held && now.Before(until) {
		return false
	}
	if !held && len(o.until) >= o.max {
		o.sweep(now)
		if len(o.until) >= o.max {
			return false
		}
	}
	o.until[key] = now.Add(o.window)
	return true
}

// sweep drops ended keys, at most once a minute, so a full set doesn't cost
// a pass over every key on each call. The caller holds mu.
func (o *onceEvery) sweep(now time.Time) {
	if now.Sub(o.lastSweep) < time.Minute {
		return
	}
	o.lastSweep = now
	for key, until := range o.until {
		if !now.Before(until) {
			delete(o.until, key)
		}
	}
}

// noticeAdminSessionMoved records admin.session.moved the first time an
// admin session is used from another network than it signed in from
// (adminNetwork). Sessions aren't bound to an address, since mobile networks
// change it, but a stolen admin cookie used elsewhere shows up in the trail.
func (s *Server) noticeAdminSessionMoved(r *http.Request, session store.AdminSession) error {
	if session.Moved {
		return nil
	}
	from, to := adminNetwork(session.IP), adminNetwork(ClientIP(r))
	if !from.IsValid() || !to.IsValid() || from == to {
		return nil
	}
	e := events.AdminSessionMoved{
		Admin: session.User.PublicID, From: from.String(), To: to.String(),
		SessionAge: int(time.Since(session.CreatedAt).Seconds()),
	}
	return s.Repo.FlagAdminSessionMoved(r.Context(), s.AdminSessionDigest(r), session.User, e)
}

// adminNetwork is the network ip is in, for noticing an admin session that
// moved: its IPv4 /24 or IPv6 /48. Invalid for anything that doesn't parse.
func adminNetwork(ip string) netip.Prefix {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Prefix{}
	}
	addr = addr.Unmap().WithZone("")
	bits := 24
	if addr.Is6() {
		bits = 48
	}
	network, _ := addr.Prefix(bits)
	return network
}
