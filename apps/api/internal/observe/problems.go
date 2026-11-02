package observe

import (
	"context"
	"sync/atomic"
)

// Problem is a failure worth counting and grouping: a request's
// InternalError, a handler's or listener's panic, a listener's final
// failure, a background job's failure, a page gate's failed query, or an
// error a browser reported. Each is logged where it happens; it is also
// handed to the Reporter, the error collector (Collector) in the server.
type Problem struct {
	// Source is SourceRequest, SourcePanic, SourceWorker, SourceBackground
	// or SourceClient.
	Source string
	// Route is the request's route (a pattern or page label), the
	// listener's name, the job's name, or a browser page's route pattern:
	// never a path.
	Route string
	// RequestID is the request's ID, or "" outside one.
	RequestID string
	// Err is what failed; for a panic, the panic value as an error. Its
	// message is scrubbed (Scrub) before it is grouped or stored.
	Err error
	// Stack is the goroutine's stack, for a panic, or a browser's stack;
	// only its function names and lines are kept (ScrubStack).
	Stack []byte
	// Release is the web build a browser's error came from (SourceClient
	// only), in ValidRequestID's shape; otherwise the server's own Version.
	Release string
}

// Problem sources: the app_errors.source CHECK.
const (
	SourceRequest    = "request"
	SourcePanic      = "panic"
	SourceWorker     = "worker"
	SourceBackground = "background"
	SourceClient     = "client"
)

// Reporter receives every Problem. Report must return quickly and never
// block: it runs on the request's goroutine.
type Reporter interface {
	Report(ctx context.Context, p Problem)
}

var reporter atomic.Pointer[Reporter]

// SetReporter makes r receive every Problem from now on; nil stops it. The
// server sets it once at startup.
func SetReporter(r Reporter) {
	if r == nil {
		reporter.Store(nil)
		return
	}
	reporter.Store(&r)
}

// Report hands p to the Reporter, if one is set.
func Report(ctx context.Context, p Problem) {
	if r := reporter.Load(); r != nil {
		(*r).Report(ctx, p)
	}
}
