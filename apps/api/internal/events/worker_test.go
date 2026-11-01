package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"lewcontext/api/internal/observe"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeQueue hands out its deliveries once and records what happened to each.
type fakeQueue struct {
	mu        sync.Mutex
	pending   []Delivery
	delivered []string
	retried   map[string]time.Time
	failed    map[string]error
}

func (q *fakeQueue) Claim(_ context.Context, limit int, _ time.Duration) ([]Delivery, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := min(limit, len(q.pending))
	batch := q.pending[:n]
	q.pending = q.pending[n:]
	return batch, nil
}
func (q *fakeQueue) Delivered(_ context.Context, d Delivery) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.delivered = append(q.delivered, d.Listener)
	return nil
}
func (q *fakeQueue) Retry(_ context.Context, d Delivery, at time.Time, _ error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retried[d.Listener] = at
	return nil
}
func (q *fakeQueue) Fail(_ context.Context, d Delivery, cause error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed[d.Listener] = cause
	return nil
}

func queueOf(deliveries ...Delivery) *fakeQueue {
	return &fakeQueue{pending: deliveries, retried: map[string]time.Time{}, failed: map[string]error{}}
}

// delivery queues a ProjectCreated for listener, on its attempt-th try.
func delivery(listener string, attempt int) Delivery {
	payload, _ := json.Marshal(ProjectCreated{Project: "prj_00000000000001", Name: "Launch"})
	return Delivery{EventID: 1, PublicID: "evt_00000000000001", Event: "project.created", Listener: listener, Payload: payload, CreatedAt: time.Unix(0, 0), Attempts: attempt}
}

func worker(bus *Bus, q Queue) *Worker {
	return &Worker{Bus: bus, Queue: q, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Timeout: 200 * time.Millisecond}
}

func TestListenDecodesItsOwnEventType(t *testing.T) {
	bus := NewBus()
	var got ProjectCreated
	var meta Delivered
	Listen(bus, "test.created", func(_ context.Context, e ProjectCreated, d Delivered) error {
		got, meta = e, d
		return nil
	})
	Listen(bus, "test.updated", func(context.Context, ProjectUpdated, Delivered) error { return nil })
	if names := bus.ListenersFor("project.created"); len(names) != 1 || names[0] != "test.created" {
		t.Fatalf("listeners for project.created: %v", names)
	}
	if names := bus.ListenersFor("auth.logged_in"); len(names) != 0 {
		t.Fatalf("listeners for an event nobody listens to: %v", names)
	}
	if (*Bus)(nil).ListenersFor("project.created") != nil {
		t.Fatal("a nil bus has listeners")
	}
	q := queueOf(delivery("test.created", 1))
	if n, err := worker(bus, q).RunOnce(context.Background()); n != 1 || err != nil {
		t.Fatalf("RunOnce: %d, %v", n, err)
	}
	if got.Project != "prj_00000000000001" || got.Name != "Launch" || meta.EventID != "evt_00000000000001" || meta.Attempt != 1 {
		t.Fatalf("listener got %+v %+v", got, meta)
	}
	if len(q.delivered) != 1 {
		t.Fatalf("not marked delivered: %+v", q)
	}
}

func TestListenRefusesDuplicateAndEmptyNames(t *testing.T) {
	for _, name := range []string{"", "test.twice"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Listen(%q) did not panic", name)
				}
			}()
			bus := NewBus()
			noop := func(context.Context, LoggedIn, Delivered) error { return nil }
			if name != "" {
				Listen(bus, name, noop)
			}
			Listen(bus, name, noop)
		}()
	}
}

func TestFailuresRetryWithBackoffThenFail(t *testing.T) {
	bus := NewBus()
	failing := func(context.Context, ProjectCreated, Delivered) error { return errors.New("smtp down") }
	Listen(bus, "test.first", failing)
	Listen(bus, "test.last", failing)
	Listen(bus, "test.ok", func(context.Context, ProjectCreated, Delivered) error { return nil })
	q := queueOf(delivery("test.first", 1), delivery("test.last", len(Backoff)+1), delivery("test.ok", 1))
	before := time.Now()
	if _, err := worker(bus, q).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// One listener failing doesn't stop the others.
	if len(q.delivered) != 1 || q.delivered[0] != "test.ok" {
		t.Fatalf("delivered: %v", q.delivered)
	}
	if at := q.retried["test.first"]; at.Before(before.Add(Backoff[0])) || at.After(time.Now().Add(Backoff[0])) {
		t.Fatalf("first failure retries after %v, at %v", Backoff[0], at)
	}
	if cause := q.failed["test.last"]; cause == nil || cause.Error() != "smtp down" {
		t.Fatalf("last attempt not failed: %v", q.failed)
	}
	// Each outcome is counted by listener, and a first attempt's lag.
	var metrics bytes.Buffer
	if err := observe.Metrics.WritePrometheus(&metrics); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`deliveries_total{listener="test.first",outcome="retried"}`,
		`deliveries_total{listener="test.last",outcome="failed"}`,
		`deliveries_total{listener="test.ok",outcome="delivered"}`,
		`delivery_lag_ms_count{listener="test.ok"}`,
	} {
		if !strings.Contains(metrics.String(), line) {
			t.Errorf("no %s", line)
		}
	}
	if strings.Contains(metrics.String(), `delivery_lag_ms_count{listener="test.last"}`) {
		t.Error("a retry's lag counted")
	}
}

func TestPanicsTimeoutsAndUnknownListeners(t *testing.T) {
	bus := NewBus()
	Listen(bus, "test.panics", func(context.Context, ProjectCreated, Delivered) error { panic("boom") })
	Listen(bus, "test.slow", func(ctx context.Context, _ ProjectCreated, _ Delivered) error {
		<-ctx.Done() // a listener must give up when its context ends
		return ctx.Err()
	})
	q := queueOf(delivery("test.panics", 1), delivery("test.slow", 1), delivery("test.renamed", 1))
	if _, err := worker(bus, q).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := q.retried["test.panics"]; !ok {
		t.Error("a panicking listener wasn't retried")
	}
	if _, ok := q.retried["test.slow"]; !ok {
		t.Error("a listener past its timeout wasn't retried")
	}
	if cause := q.failed["test.renamed"]; cause == nil || !strings.Contains(cause.Error(), "no listener") {
		t.Errorf("a delivery for an unregistered listener: %v", cause)
	}
	// A name nobody registered never becomes a label.
	var metrics bytes.Buffer
	if err := observe.Metrics.WritePrometheus(&metrics); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(metrics.String(), "test.renamed") {
		t.Error("an unregistered listener's name is a label")
	}
}

func TestRunStopsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := worker(NewBus(), queueOf())
		w.Poll = time.Millisecond
		w.Run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run kept going after its context ended")
	}
}

// A listener's panic is logged at error with its stack, as a request's is,
// and handed to observe.Report; the delivery is retried.
func TestListenerPanicsAreLogged(t *testing.T) {
	bus := NewBus()
	Listen(bus, "test.panics", func(context.Context, ProjectCreated, Delivered) error { panic("boom") })
	q := queueOf(delivery("test.panics", 1))
	var out bytes.Buffer
	w := worker(bus, q)
	w.Log = slog.New(slog.NewJSONHandler(&out, nil))
	var reported []observe.Problem
	observe.SetReporter(problems(func(p observe.Problem) { reported = append(reported, p) }))
	defer observe.SetReporter(nil)
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(out.String(), "\n", 2)[0]), &first); err != nil {
		t.Fatalf("log: %v %q", err, out.String())
	}
	if first["level"] != "ERROR" || first["msg"] != "event listener panicked" || first["listener"] != "test.panics" ||
		!strings.Contains(first["panic"].(string), "boom") || !strings.Contains(first["stack"].(string), "TestListenerPanicsAreLogged") {
		t.Fatalf("panic line: %v", first)
	}
	if len(reported) != 1 || reported[0].Source != observe.SourceWorker || reported[0].Route != "test.panics" || len(reported[0].Stack) == 0 {
		t.Fatalf("reported: %+v", reported)
	}
	if _, ok := q.retried["test.panics"]; !ok {
		t.Fatal("not retried")
	}
}

type problems func(observe.Problem)

func (f problems) Report(_ context.Context, p observe.Problem) { f(p) }

// A listener's panic value is stored and logged only as observe.PanicValue
// allows: a URL (which may hold a token) is redacted.
func TestListenerPanicValuesAreRedacted(t *testing.T) {
	bus := NewBus()
	link, _ := url.Parse("https://app.example.com/auth/forgot-password/s3cr3t-token")
	Listen(bus, "test.panics", func(context.Context, ProjectCreated, Delivered) error { panic(link) })
	q := queueOf(delivery("test.panics", len(Backoff)+1))
	var out bytes.Buffer
	w := worker(bus, q)
	w.Log = slog.New(slog.NewJSONHandler(&out, nil))
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	cause := q.failed["test.panics"]
	if cause == nil || !strings.Contains(cause.Error(), observe.Redacted) || strings.Contains(cause.Error(), "s3cr3t") || strings.Contains(out.String(), "s3cr3t") {
		t.Fatalf("stored %v, logged %s", cause, out.String())
	}
}

// A listener's error is logged scrubbed (an SMTP refusal names the
// recipient), and reported only once it has failed for good: a retry isn't
// a failure yet. A panic, reported as it happens, isn't reported twice on
// the last attempt; a listener nobody registered is reported too.
func TestFinalListenerFailuresAreReported(t *testing.T) {
	refusal := errors.New("gomail: could not send email 1: 550 5.1.1 <ada@example.com>: Recipient address rejected")
	bus := NewBus()
	Listen(bus, "test.fails", func(context.Context, ProjectCreated, Delivered) error { return refusal })
	Listen(bus, "test.panics", func(context.Context, ProjectCreated, Delivered) error { panic("boom") })
	var reported []observe.Problem
	var mu sync.Mutex
	observe.SetReporter(problems(func(p observe.Problem) { mu.Lock(); reported = append(reported, p); mu.Unlock() }))
	defer observe.SetReporter(nil)
	var out bytes.Buffer
	run := func(deliveries ...Delivery) {
		t.Helper()
		w := worker(bus, queueOf(deliveries...))
		w.Log = slog.New(slog.NewJSONHandler(&out, nil))
		if _, err := w.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	run(delivery("test.fails", 1))
	if len(reported) != 0 {
		t.Fatalf("a retry was reported: %+v", reported)
	}
	run(delivery("test.fails", len(Backoff)+1))
	if len(reported) != 1 || reported[0].Source != observe.SourceWorker || reported[0].Route != "test.fails" || !errors.Is(reported[0].Err, refusal) {
		t.Fatalf("the final failure: %+v", reported)
	}
	if strings.Contains(out.String(), "ada@example.com") || !strings.Contains(out.String(), "<email>: Recipient address rejected") {
		t.Fatalf("the log isn't scrubbed: %s", out.String())
	}
	reported = nil
	run(delivery("test.panics", len(Backoff)+1))
	if len(reported) != 1 || len(reported[0].Stack) == 0 {
		t.Fatalf("a final panic: %+v", reported)
	}
	reported = nil
	run(delivery("test.nobody", 1))
	if len(reported) != 1 || reported[0].Route != "test.nobody" {
		t.Fatalf("an unregistered listener: %+v", reported)
	}
}
