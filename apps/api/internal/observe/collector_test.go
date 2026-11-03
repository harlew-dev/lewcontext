package observe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// flushes records what a collector stored, failing while fail is set.
type flushes struct {
	mu      sync.Mutex
	calls   int
	groups  [][]ErrorGroup
	fail    bool
	block   chan struct{} // when set, Store says it started on entered, then waits for block
	entered chan struct{}
}

func (f *flushes) store(ctx context.Context, groups []ErrorGroup) error {
	if f.block != nil {
		f.entered <- struct{}{}
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")
	}
	f.groups = append(f.groups, groups)
	return nil
}

// clock is a collector's time, moved by hand.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }
func (c *clock) tick()          { c.now = c.now.Add(time.Second) }

func testCollector() (*Collector, *flushes, *clock) {
	f := &flushes{}
	c := NewCollector(f.store, "abc123def456")
	at := &clock{now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	c.Now = at.Now
	return c, f, at
}

func problem(source, route, message string) Problem {
	return Problem{Source: source, Route: route, Err: errors.New(message)}
}

// Failures differing only in scrubbed values are one group, counted; the
// group keeps the scrubbed message, the latest request, release and stack.
func TestCollectorGroups(t *testing.T) {
	c, f, at := testCollector()
	ctx := context.Background()
	route := "GET /api/workspaces/{workspace}"
	c.Report(ctx, Problem{Source: SourceRequest, Route: route, RequestID: "req_0123456789abcd", Err: errors.New("no workspace wsp_8kQ2mZ1xT4bLwN for ada@example.com")})
	at.tick()
	c.Report(ctx, Problem{Source: SourceRequest, Route: route, RequestID: "req_ZZZZZZZZZZZZZZ", Err: errors.New("no workspace wsp_0000000000000A for bob@example.org")})
	c.Report(ctx, Problem{Source: SourcePanic, Route: route, Err: errors.New("boom"), Stack: []byte("goroutine 1 [running]:\nmain.f(0xc000012345)\n\t/src/main.go:3 +0x1")})
	c.Report(ctx, Problem{Source: SourceRequest, Route: route, RequestID: "not ours", Err: nil})
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.groups) != 1 || len(f.groups[0]) != 3 {
		t.Fatalf("flushed %+v", f.groups)
	}
	byMessage := map[string]ErrorGroup{}
	for _, g := range f.groups[0] {
		byMessage[g.Message] = g
	}
	g := byMessage["no workspace <id> for <email>"]
	if g.Count != 2 || g.LastRequestID != "req_ZZZZZZZZZZZZZZ" || !g.LastSeen.After(g.FirstSeen) || g.LastVersion != "abc123def456" || g.Source != SourceRequest || g.Route != route {
		t.Errorf("the request group: %+v", g)
	}
	if g.Fingerprint != Fingerprint(SourceRequest, route, g.Message) {
		t.Error("fingerprint isn't of the scrubbed message")
	}
	if p := byMessage["boom"]; p.Stack != "main.f(…)\n\tmain.go:3" || p.Count != 1 {
		t.Errorf("the panic group: %+v", p)
	}
	if empty := byMessage["(no message)"]; empty.LastRequestID != "" || empty.Count != 1 {
		t.Errorf("a nil error with a foreign request ID: %+v", empty)
	}
	// Nothing to store: no call at all.
	if err := c.Flush(ctx); err != nil || f.calls != 1 {
		t.Fatalf("an empty flush stored: %d calls", f.calls)
	}
}

// A browser's release is kept only in Version's shape (hex, maybe -dirty,
// or dev), so nobody can leave words in it; the server's own version is
// never used for it.
func TestCollectorClientRelease(t *testing.T) {
	c, f, _ := testCollector()
	ctx := context.Background()
	for message, release := range map[string]string{
		"a": "abc1234def56", "b": "abc1234def56-dirty", "c": "dev",
		"d": "please-visit-evil.example.com", "e": "<script>", "f": strings.Repeat("a", 41), "g": "ABC1234", "h": "",
	} {
		c.Report(ctx, Problem{Source: SourceClient, Route: "/app", Release: release, Err: errors.New(message)})
	}
	_ = c.Flush(ctx)
	got := map[string]string{}
	for _, g := range f.groups[0] {
		got[g.Message] = g.LastVersion
	}
	want := map[string]string{"a": "abc1234def56", "b": "abc1234def56-dirty", "c": "dev", "d": "", "e": "", "f": "", "g": "", "h": ""}
	for message, release := range want {
		if got[message] != release {
			t.Errorf("release for %s: %q, want %q", message, got[message], release)
		}
	}
}

// The collector holds at most MaxErrorGroups, dropping the stalest; at most
// MaxClientErrorGroups come from browsers, and a browser's group never
// pushes out the server's.
func TestCollectorIsBounded(t *testing.T) {
	c, _, at := testCollector()
	ctx := context.Background()
	for i := range MaxErrorGroups {
		c.Report(ctx, problem(SourceRequest, fmt.Sprintf("route %03d", i), "failed"))
		at.tick()
	}
	if c.Len() != MaxErrorGroups {
		t.Fatalf("%d groups", c.Len())
	}
	// Full of the server's: a browser's new group is dropped.
	c.Report(ctx, problem(SourceClient, "/app", "TypeError: x is undefined"))
	if c.Len() != MaxErrorGroups || hasSource(c, SourceClient) {
		t.Fatal("a browser's group pushed out the server's")
	}
	// A server's new group pushes out the stalest (the first).
	c.Report(ctx, problem(SourceWorker, "mail.password_reset", "550"))
	if c.Len() != MaxErrorGroups || hasRoute(c, "route 000") || !hasRoute(c, "mail.password_reset") {
		t.Fatal("the stalest group wasn't dropped")
	}
	_ = c.Flush(ctx)

	// Browsers fill at most their share, then push out only their own.
	for i := range MaxClientErrorGroups + 50 {
		c.Report(ctx, problem(SourceClient, "/app", fmt.Sprintf("Error %c%c", 'a'+i%26, 'a'+i/26)))
		at.tick()
	}
	c.Report(ctx, problem(SourceRequest, "GET /api/health", "server"))
	if n := countSource(c, SourceClient); n != MaxClientErrorGroups {
		t.Fatalf("%d browser groups", n)
	}
	if !hasMessage(c, "server") || hasMessage(c, "Error aa") || !hasMessage(c, fmt.Sprintf("Error %c%c", 'a'+(MaxClientErrorGroups+49)%26, 'a'+(MaxClientErrorGroups+49)/26)) {
		t.Fatal("browsers' groups didn't push out their own stalest")
	}
}

// A failed flush keeps its groups for the next one, merged with what came
// meanwhile, and nothing retries before then.
func TestCollectorKeepsGroupsWhenTheFlushFails(t *testing.T) {
	c, f, at := testCollector()
	ctx := context.Background()
	c.Report(ctx, problem(SourceRequest, "GET /x", "down"))
	f.fail = true
	if err := c.Flush(ctx); err == nil {
		t.Fatal("the flush didn't fail")
	}
	at.tick()
	c.Report(ctx, problem(SourceRequest, "GET /x", "down"))
	if c.Len() != 1 || f.calls != 1 {
		t.Fatalf("%d groups, %d calls", c.Len(), f.calls)
	}
	f.fail = false
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if g := f.groups[0][0]; g.Count != 2 || !g.LastSeen.After(g.FirstSeen) {
		t.Errorf("merged group: %+v", g)
	}
}

// Run flushes once a tick, only when there is something, and a failing
// database costs one attempt a tick: no retry loop.
func TestCollectorRunFlushesOncePerTick(t *testing.T) {
	c, f, _ := testCollector()
	f.fail = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx, 20*time.Millisecond)
	}()
	time.Sleep(70 * time.Millisecond)
	f.mu.Lock()
	idle := f.calls
	f.mu.Unlock()
	if idle != 0 {
		t.Fatalf("flushed with nothing collected: %d calls", idle)
	}
	c.Report(ctx, problem(SourceRequest, "GET /x", "down"))
	time.Sleep(110 * time.Millisecond)
	cancel()
	<-done
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls < 2 || f.calls > 7 {
		t.Errorf("%d attempts in about 5 ticks", f.calls)
	}
	if c.Len() != 1 {
		t.Errorf("the group wasn't kept: %d", c.Len())
	}
}

// Reporting never waits on a flush in progress (the database).
func TestReportDoesntWaitForTheFlush(t *testing.T) {
	c, f, _ := testCollector()
	ctx := context.Background()
	c.Report(ctx, problem(SourceRequest, "GET /x", "first"))
	f.block, f.entered = make(chan struct{}), make(chan struct{})
	flushed := make(chan error)
	go func() { flushed <- c.Flush(ctx) }()
	<-f.entered // the flush has its groups and is "writing"
	var reported atomic.Bool
	go func() {
		for range 100 {
			c.Report(ctx, problem(SourceRequest, "GET /x", "during"))
		}
		reported.Store(true)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !reported.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !reported.Load() {
		t.Fatal("Report waited for the flush")
	}
	close(f.block)
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if c.Len() != 1 {
		t.Errorf("reports during the flush: %d groups", c.Len())
	}
}

func hasSource(c *Collector, source string) bool { return countSource(c, source) > 0 }

func countSource(c *Collector, source string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, g := range c.groups {
		if g.Source == source {
			n++
		}
	}
	return n
}

func hasRoute(c *Collector, route string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range c.groups {
		if g.Route == route {
			return true
		}
	}
	return false
}

func hasMessage(c *Collector, message string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range c.groups {
		if g.Message == message {
			return true
		}
	}
	return false
}

// A browser's group is fingerprinted on its first 120 scrubbed characters,
// so varying the end of a long message makes no new group; its text is
// scrubbed as a browser's (hosts too) and Go-format stack lines dropped.
func TestCollectorClientGroups(t *testing.T) {
	c, f, _ := testCollector()
	ctx := context.Background()
	prefix := "Error: " + strings.Repeat("x", 120)
	c.Report(ctx, Problem{Source: SourceClient, Route: "/app", Err: errors.New(prefix + " one")})
	c.Report(ctx, Problem{Source: SourceClient, Route: "/app", Err: errors.New(prefix + " two"),
		Stack: []byte("\tvisit evil.example.com x.go:1\nmain.f(0x1)\n    at f (https://h/a.js:1:2)")})
	c.Report(ctx, Problem{Source: SourceClient, Route: "/app", Err: errors.New("visit evil.example.com/login")})
	_ = c.Flush(ctx)
	if len(f.groups[0]) != 2 {
		t.Fatalf("groups: %+v", f.groups[0])
	}
	for _, g := range f.groups[0] {
		switch {
		case strings.HasPrefix(g.Message, "Error: "):
			if g.Count != 2 || g.Stack != "at f (a.js:1:2)" {
				t.Errorf("the long message's group: %+v", g)
			}
		case g.Message != "visit <url>":
			t.Errorf("a host in a browser's message: %q", g.Message)
		}
	}
}

// A store that drops some groups for their data and fails others puts back
// only those it failed: a group refused for its data never comes back.
func TestCollectorDropsWhatTheStoreRefused(t *testing.T) {
	ctx := context.Background()
	var calls [][]ErrorGroup
	c := NewCollector(func(_ context.Context, groups []ErrorGroup) error {
		calls = append(calls, groups)
		if len(calls) > 1 {
			return nil
		}
		var retry []ErrorGroup
		for _, g := range groups {
			if g.Message == "transient" {
				retry = append(retry, g)
			}
		}
		return &FlushError{Retry: retry, Dropped: len(groups) - len(retry), Err: errors.New("connection reset")}
	}, "dev")
	c.Report(ctx, problem(SourceRequest, "GET /x", "transient"))
	c.Report(ctx, problem(SourceRequest, "GET /x", "bad data"))
	if err := c.Flush(ctx); err == nil || c.Len() != 1 {
		t.Fatalf("after a partial failure: %v, %d kept", err, c.Len())
	}
	if err := c.Flush(ctx); err != nil || len(calls[1]) != 1 || calls[1][0].Message != "transient" {
		t.Fatalf("the retry: %v %+v", err, calls)
	}
}
