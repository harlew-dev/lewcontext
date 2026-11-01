package events

import (
	"context"
	"errors"
	"fmt"
	"lewcontext/api/internal/observe"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Delivery is one event queued for one listener, as the queue hands it out.
type Delivery struct {
	EventID   int64 // internal; identifies the row with Listener
	PublicID  string
	Event     string
	Listener  string
	Payload   []byte
	CreatedAt time.Time
	Attempts  int // including the one being made
}

// Queue is where deliveries wait: the event_deliveries table, implemented
// by internal/store.
type Queue interface {
	// Claim leases up to limit due deliveries for lease, so other workers
	// skip them until it runs out.
	Claim(ctx context.Context, limit int, lease time.Duration) ([]Delivery, error)
	Delivered(ctx context.Context, d Delivery) error
	Retry(ctx context.Context, d Delivery, at time.Time, cause error) error
	Fail(ctx context.Context, d Delivery, cause error) error
}

// Backoff is how long a failed delivery waits before each retry; after the
// last one it is marked failed.
var Backoff = []time.Duration{time.Second, 10 * time.Second, time.Minute, 10 * time.Minute, time.Hour}

// Worker delivers queued events to their listeners. Several workers (one per
// server process) can share a queue: each claims different rows.
type Worker struct {
	Bus   *Bus
	Queue Queue
	Log   *slog.Logger
	// Zero values use the defaults in the comments.
	Poll    time.Duration // between empty claims: 1 s
	Batch   int           // deliveries claimed at once: 10
	Lease   time.Duration // how long a claim lasts: 5 min
	Timeout time.Duration // per listener run: 30 s
	Backoff []time.Duration
}

func (w *Worker) defaults() {
	if w.Poll == 0 {
		w.Poll = time.Second
	}
	if w.Batch == 0 {
		w.Batch = 10
	}
	if w.Lease == 0 {
		w.Lease = 5 * time.Minute
	}
	if w.Timeout == 0 {
		w.Timeout = 30 * time.Second
	}
	if w.Backoff == nil {
		w.Backoff = Backoff
	}
	if w.Log == nil {
		w.Log = slog.Default()
	}
}

// Run delivers until ctx is done, then returns once the batch in hand has
// finished. Listeners run with their own timeout, not ctx, so a shutdown
// doesn't cut one off halfway; what's left is retried when its lease ends.
func (w *Worker) Run(ctx context.Context) {
	w.defaults()
	for ctx.Err() == nil {
		n, err := w.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			w.Log.Error("event delivery failed", observe.ErrorAttr(err))
			observe.Report(ctx, observe.Problem{Source: observe.SourceBackground, Route: "event worker", Err: err})
		}
		if n == w.Batch {
			continue // more may be waiting
		}
		select {
		case <-ctx.Done():
		case <-time.After(w.Poll):
		}
	}
}

// RunOnce claims one batch, runs its listeners concurrently and records each
// result. It returns how many deliveries it claimed.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	w.defaults()
	batch, err := w.Queue.Claim(ctx, w.Batch, w.Lease)
	if err != nil {
		return 0, err
	}
	results := make(chan error, len(batch))
	var wg sync.WaitGroup
	for _, d := range batch {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- w.deliver(d)
		}()
	}
	wg.Wait()
	close(results)
	var errs []error
	for err := range results {
		errs = append(errs, err)
	}
	return len(batch), errors.Join(errs...)
}

// deliver runs one listener and records the outcome; the error is only for
// recording failures, not the listener's. A listener's error is logged
// scrubbed (observe.ErrorAttr: an SMTP refusal names the recipient), and
// its final failure, after the last retry, is reported (observe.Report).
func (w *Worker) deliver(d Delivery) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.Timeout)
	defer cancel()
	handle := w.Bus.handler(d.Listener, d.Event)
	if handle == nil {
		// Not a registered name, so not a label: counted as unmatched.
		observe.Deliveries.Inc(observe.Unmatched, observe.DeliveryFailed)
		cause := fmt.Errorf("no listener %q for %s", d.Listener, d.Event)
		w.Log.Error("event listener not registered", "listener", d.Listener, "event", d.Event, "id", d.PublicID)
		observe.Report(ctx, observe.Problem{Source: observe.SourceWorker, Route: d.Listener, Err: cause})
		return w.Queue.Fail(ctx, d, cause)
	}
	if d.Attempts == 1 {
		observe.DeliveryLag.Observe(time.Since(d.CreatedAt), d.Listener)
	}
	cause := w.run(ctx, handle, d)
	if cause == nil {
		observe.Deliveries.Inc(d.Listener, observe.DeliveryDelivered)
		return w.Queue.Delivered(ctx, d)
	}
	attrs := []any{"listener", d.Listener, "event", d.Event, "id", d.PublicID, "attempt", d.Attempts, observe.ErrorAttr(cause)}
	if d.Attempts > len(w.Backoff) {
		w.Log.Error("event listener failed for good", attrs...)
		// A panic was reported as it happened, with its stack.
		if !errors.As(cause, new(listenerPanic)) {
			observe.Report(ctx, observe.Problem{Source: observe.SourceWorker, Route: d.Listener, Err: cause})
		}
		observe.Deliveries.Inc(d.Listener, observe.DeliveryFailed)
		return w.Queue.Fail(ctx, d, cause)
	}
	observe.Deliveries.Inc(d.Listener, observe.DeliveryRetried)
	w.Log.Warn("event listener failed; will retry", attrs...)
	return w.Queue.Retry(ctx, d, time.Now().Add(w.Backoff[d.Attempts-1]), cause)
}

// run calls a listener, turning a panic into an error, which fails the
// delivery like any other (it's retried). The panic is logged at error with
// its stack, as a request's is, and handed to observe.Report; its value only
// as observe.PanicValue allows, in the log and in the delivery's stored
// error alike.
func (w *Worker) run(ctx context.Context, handle handler, d Delivery) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = listenerPanic{fmt.Errorf("listener panicked: %s", observe.PanicValue(p))}
			stack := debug.Stack()
			w.Log.ErrorContext(ctx, "event listener panicked", "listener", d.Listener, "event", d.Event, "id", d.PublicID, "panic", observe.ScrubError(err), "stack", observe.ScrubStack(string(stack)))
			observe.Report(ctx, observe.Problem{Source: observe.SourceWorker, Route: d.Listener, Err: err, Stack: stack})
		}
	}()
	return handle(ctx, d.Payload, Delivered{EventID: d.PublicID, OccurredAt: d.CreatedAt, Attempt: d.Attempts})
}

// listenerPanic is the error a listener's panic fails its delivery with.
type listenerPanic struct{ error }
