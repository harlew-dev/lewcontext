package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
