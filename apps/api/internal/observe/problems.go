package observe

import (
	"context"
	"sync/atomic"
)

// Problem is a failure worth counting and grouping: a request's
// InternalError, a handler's or listener's panic. Each is logged where it
// happens; it is also handed to the Reporter, which plan A3's second phase
// (grouped errors, app_errors) will set. Until then nothing receives it.
type Problem struct {
	// Source is SourceRequest, SourcePanic or SourceWorker.
	Source string
	// Route is the request's route (a pattern or page label) or the
	// listener's name: never a path.
	Route string
	// RequestID is the request's ID, or "" outside one.
	RequestID string
	// Err is what failed; for a panic, the panic value as an error.
	Err error
	// Stack is the goroutine's stack, for a panic.
	Stack []byte
}

// Problem sources.
const (
	SourceRequest = "request"
	SourcePanic   = "panic"
	SourceWorker  = "worker"
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
