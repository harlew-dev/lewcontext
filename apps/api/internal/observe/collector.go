package observe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrorGroup is the failures that share a fingerprint since the last flush:
// what the store upserts into app_errors (store.RecordErrors), one row per
// fingerprint, adding Count to the row's count.
type ErrorGroup struct {
	// Fingerprint is the hex SHA-256 of the source, route and scrubbed
	// message: equal for failures that differ only in what Scrub replaces.
	Fingerprint string
	Source      string
	Route       string
	// Message is the scrubbed message (Scrub), at most MaxMessage
	// characters.
	Message string
	// Stack is the latest occurrence's scrubbed stack (ScrubStack), or "".
	Stack string
	// Count is how many times it happened since the last flush.
	Count     int64
	FirstSeen time.Time
	LastSeen  time.Time
	// LastRequestID is the latest occurrence's request ID, or "".
	LastRequestID string
	// LastVersion is the latest occurrence's release: the server's Version,
	// or for a browser's error the build it reported.
	LastVersion string
}

// Collector limits: how many groups it holds between flushes (the stalest
// is dropped for a new one when full), how many of those may come from
// browsers (so they can never push out the server's), and how often Run
// flushes.
const (
	MaxErrorGroups       = 1000
	MaxClientErrorGroups = 100
	ErrorFlushInterval   = 10 * time.Second
	maxRouteLength       = 200
	// clientFingerprintLength is how much of a browser's scrubbed message
	// its fingerprint covers, so a browser can't make a new group per
	// report by varying the end of a long message.
	clientFingerprintLength = 120
)

// Collector groups every Problem by fingerprint in memory, and hands the
// groups to its flush (Run, Flush) every ErrorFlushInterval, only when there
// are any. Reporting is a map update under a mutex, never a query, so it
// never makes a request wait on the database; scrubbing and hashing happen
// before the lock. It holds at most MaxErrorGroups groups, at most
// MaxClientErrorGroups of them from browsers: a browser's new group only
// ever pushes out another browser's. A failed flush puts its groups back
// (within the same limits) for the next one, ErrorFlushInterval later;
// nothing retries sooner, so a database outage costs one attempt per
// interval.
type Collector struct {
	// Store writes a flush's groups (store.RecordErrors).
	Store func(ctx context.Context, groups []ErrorGroup) error
	// Version is the server's release (Version()), for its own problems.
	Version string
	// Now is time.Now unless a test sets it.
	Now func() time.Time

	mu      sync.Mutex
	groups  map[string]*ErrorGroup
	clients int
	dropped int64
}

// NewCollector is a Collector writing through store.
func NewCollector(store func(ctx context.Context, groups []ErrorGroup) error, version string) *Collector {
	return &Collector{Store: store, Version: version, Now: time.Now}
}

// Report groups p (Reporter). Safe from any goroutine.
func (c *Collector) Report(_ context.Context, p Problem) {
	g := c.group(p)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.add(g)
}

// group is p as a new group of one: scrubbed, fingerprinted and stamped.
func (c *Collector) group(p Problem) ErrorGroup {
	now := c.now()
	client := p.Source == SourceClient
	message, stack := "", ""
	if client {
		if p.Err != nil {
			message = ScrubClient(p.Err.Error())
		}
		stack = ScrubClientStack(string(p.Stack))
	} else {
		message = ScrubError(p.Err)
		stack = ScrubStack(string(p.Stack))
	}
	if message == "" {
		message = "(no message)"
	}
	// Routes are fixed labels, but a control character would make the row
	// unstorable, so any route not plainly printable is unmatched.
	route := truncateRunes(p.Route, maxRouteLength)
	if !printable(route) {
		route = Unmatched
	}
	version := c.Version
	shape := message
	if client {
		version = ""
		if ValidRelease(p.Release) {
			version = p.Release
		}
		shape = truncateRunes(message, clientFingerprintLength)
	}
	return ErrorGroup{
		Fingerprint:   Fingerprint(p.Source, route, shape),
		Source:        p.Source,
		Route:         route,
		Message:       message,
		Stack:         stack,
		Count:         1,
		FirstSeen:     now,
		LastSeen:      now,
		LastRequestID: requestIDOrEmpty(p.RequestID),
		LastVersion:   version,
	}
}

// releasePattern is a release as Version makes them: a VCS revision's hex
// (7 to 40 characters), maybe "-dirty", or "dev".
var releasePattern = regexp.MustCompile(`^(?:[0-9a-f]{7,40}(?:-dirty)?|dev)$`)

// ValidRelease reports whether a browser's release may be stored: in
// Version's shape, nothing a browser could write words into.
func ValidRelease(release string) bool {
	return releasePattern.MatchString(release)
}

// printable reports whether s is valid UTF-8 without control or format
// characters.
func printable(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	})
}

// requestIDOrEmpty keeps an ID of ours (req_ and 14 base62), the shape
// app_errors.last_request_id accepts, and nothing else.
func requestIDOrEmpty(id string) string {
	if len(id) != len(requestIDPrefix)+requestIDLength || id[:len(requestIDPrefix)] != requestIDPrefix {
		return ""
	}
	for i := len(requestIDPrefix); i < len(id); i++ {
		c := id[i]
		if !('0' <= c && c <= '9' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z') {
			return ""
		}
	}
	return id
}

// Fingerprint groups failures: the hex SHA-256 of the source, the route and
// the scrubbed message's shape.
func Fingerprint(source, route, message string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + route + "\x00" + message))
	return hex.EncodeToString(sum[:])
}

// add merges g into its group, or makes room for it. Under c.mu.
func (c *Collector) add(g ErrorGroup) {
	if c.groups == nil {
		c.groups = make(map[string]*ErrorGroup)
	}
	if existing, ok := c.groups[g.Fingerprint]; ok {
		merge(existing, g)
		return
	}
	client := g.Source == SourceClient
	if client && c.clients >= MaxClientErrorGroups || len(c.groups) >= MaxErrorGroups {
		if !c.evict(client) {
			c.dropped += g.Count
			return
		}
	}
	c.groups[g.Fingerprint] = &g
	if client {
		c.clients++
	}
}

// merge adds g's occurrences to existing: the first and last times widen,
// and the latest occurrence's request, release and stack win.
func merge(existing *ErrorGroup, g ErrorGroup) {
	existing.Count += g.Count
	if g.FirstSeen.Before(existing.FirstSeen) {
		existing.FirstSeen = g.FirstSeen
	}
	if !g.LastSeen.Before(existing.LastSeen) {
		existing.LastSeen = g.LastSeen
		existing.LastRequestID = g.LastRequestID
		existing.LastVersion = g.LastVersion
		if g.Stack != "" {
			existing.Stack = g.Stack
		}
	}
}

// evict drops the stalest group (least recently seen) to make room: for a
// browser's group, only another browser's. It reports whether it dropped
// one. Under c.mu.
func (c *Collector) evict(clientOnly bool) bool {
	var stalest *ErrorGroup
	for _, g := range c.groups {
		if clientOnly && g.Source != SourceClient {
			continue
		}
		if stalest == nil || g.LastSeen.Before(stalest.LastSeen) {
			stalest = g
		}
	}
	if stalest == nil {
		return false
	}
	delete(c.groups, stalest.Fingerprint)
	if stalest.Source == SourceClient {
		c.clients--
	}
	c.dropped += stalest.Count
	return true
}

// take empties the collector and returns its groups and how many
// occurrences were dropped since the last take.
func (c *Collector) take() ([]ErrorGroup, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	groups := make([]ErrorGroup, 0, len(c.groups))
	for _, g := range c.groups {
		groups = append(groups, *g)
	}
	dropped := c.dropped
	c.groups, c.clients, c.dropped = nil, 0, 0
	return groups, dropped
}

// putBack returns a failed flush's groups, merged with what arrived
// meanwhile, within the limits.
func (c *Collector) putBack(groups []ErrorGroup) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range groups {
		c.add(g)
	}
}

// Len is how many groups wait for the next flush.
func (c *Collector) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.groups)
}

// FlushError is what a Collector's Store returns when it stored some groups
// and not others: Retry are put back for the next flush (the database
// failed for them); Dropped were refused for their own data, and are never
// put back, so one bad group can't stop every later flush.
type FlushError struct {
	Retry   []ErrorGroup
	Dropped int
	Err     error
}

func (e *FlushError) Error() string {
	if e.Err == nil {
		return "error groups dropped for their data"
	}
	return e.Err.Error()
}

func (e *FlushError) Unwrap() error { return e.Err }

// Flush stores the groups collected so far, if there are any. On failure
// they are kept for the next flush (within the limits) and the error is
// returned, apart from groups the store refused for their own data
// (FlushError), which are dropped and counted in the log; it never retries
// itself. A flush that committed but still returned an error (a lost
// connection on the commit) puts its groups back too, so they are counted
// twice: accepted, as rare and only an overcount.
func (c *Collector) Flush(ctx context.Context) error {
	groups, dropped := c.take()
	if dropped > 0 {
		slog.WarnContext(ctx, "error groups dropped: more distinct errors than the collector holds", "occurrences", dropped)
	}
	if len(groups) == 0 {
		return nil
	}
	err := c.Store(ctx, groups)
	var partial *FlushError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &partial):
		if partial.Dropped > 0 {
			slog.WarnContext(ctx, "error groups dropped: refused for their data", "groups", partial.Dropped)
		}
		c.putBack(partial.Retry)
		if len(partial.Retry) == 0 {
			return nil
		}
		return err
	}
	c.putBack(groups)
	return err
}

// Run flushes every interval (ErrorFlushInterval when 0) until ctx ends,
// then returns without flushing: the server flushes once more after its
// requests have finished (Flush). A failed flush is logged and left for the
// next tick.
func (c *Collector) Run(ctx context.Context, interval time.Duration) {
	if interval == 0 {
		interval = ErrorFlushInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flush, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := c.Flush(flush); err != nil && ctx.Err() == nil {
				slog.Warn("storing error groups failed; kept for the next flush", ErrorAttr(err), "groups", c.Len())
			}
			cancel()
		}
	}
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
