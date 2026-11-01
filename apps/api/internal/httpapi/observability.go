package httpapi

// What the server says about each request (docs/api/observability.md): its
// ID, its access line, a panic turned into a 500, and readiness while the
// server shuts down. ClientAddress (client_ip.go) runs the first two.

import (
	"context"
	"errors"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/httpapi/clientip"
	"lewcontext/api/internal/observe"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// requestIDHeader is X-Request-ID in Go's canonical form: the request's ID
// on every response, and a trusted proxy's own ID on the way in.
const requestIDHeader = "X-Request-Id"

// requestID gives request its ID, always a new one of ours (req_ and 14
// base62). A trusted proxy's X-Request-ID is kept apart, for the access
// line only (proxyRequestId): Cloudflare, load balancers and nginx pass a
// client's own header on unchanged, so believing it as our ID would let a
// client put another request's ID on its lines and events. Even as that, it
// is taken only from TRUSTED_PROXIES, sent once, in the safe shape
// (observe.ValidRequestID).
func (s *Server) requestID(r *http.Request, request *observe.Request) string {
	if inbound := r.Header[requestIDHeader]; len(inbound) == 1 && observe.ValidRequestID(inbound[0]) && clientip.FromTrustedProxy(r, s.Config.TrustedProxies) {
		request.ProxyID = inbound[0]
	}
	return request.NewID()
}

// slowRequest is how long a request may take before ACCESS_LOG=errors logs
// it as well as failures.
const slowRequest = time.Second

// finishRequest runs when ClientAddress is done with a request: a panic
// that got past RecoverPanics (in ClientAddress itself, Security or the
// dispatch to the API or the pages) is logged and answered as RecoverPanics
// would, or the connection cut once the answer has started; then the access
// line is written, a panic of its own caught too. Deferred, so it runs
// however the request ended.
func (s *Server) finishRequest(w http.ResponseWriter, r *http.Request, request *observe.Request, start time.Time) {
	if p := recover(); p != nil {
		if p != http.ErrAbortHandler {
			logPanic(r, p)
		}
		if p == http.ErrAbortHandler || request.Written() {
			request.Aborted = true
			s.safeLogRequest(r, request, start)
			panic(http.ErrAbortHandler)
		}
		answerPanic(w, r, request)
	}
	s.safeLogRequest(r, request, start)
}

// safeLogRequest is logRequest, a panic in it logged and dropped: the
// answer has been written by then.
func (s *Server) safeLogRequest(r *http.Request, request *observe.Request, start time.Time) {
	defer func() {
		if p := recover(); p != nil {
			logPanic(r, p)
		}
	}()
	s.logRequest(r, request, start)
}

// logRequest writes the request's access line (msg "request"): its ID, the
// method (observe.Method), the route (the matched pattern or a page's
// label, never the path or query, which hold tokens, the admin key and
// searches), the status, how long it took, the bytes written, who was
// signed in and the client's address; then a trusted proxy's own request ID
// (proxyRequestId) and aborted, when there are. Health checks log at debug,
// so probes don't fill the log; what ACCESS_LOG says is left out (never an
// aborted request). Status 0 is a request aborted before any answer.
func (s *Server) logRequest(r *http.Request, request *observe.Request, start time.Time) {
	if s.Config.AccessLog == config.AccessLogOff {
		return
	}
	elapsed := time.Since(start)
	status := request.Status
	if status == 0 && !request.Aborted {
		status = http.StatusOK // the handler wrote nothing: net/http sends 200
	}
	if s.Config.AccessLog == config.AccessLogErrors && !request.Aborted && status < 400 && elapsed < slowRequest {
		return
	}
	level := slog.LevelInfo
	if strings.HasPrefix(request.Route, "GET /api/health") {
		level = slog.LevelDebug
	}
	logger, ctx := slog.Default(), r.Context()
	if !logger.Enabled(ctx, level) {
		return
	}
	attrs := [10]slog.Attr{
		slog.String(observe.RequestIDKey, request.ID),
		slog.String("method", observe.Method(r.Method)),
		slog.String("route", request.Route),
		slog.Int("status", status),
		// Whole milliseconds: slog's JSON handler marshals a float through
		// encoding/json, two allocations more per request.
		slog.Int64("durationMs", elapsed.Round(time.Millisecond).Milliseconds()),
		slog.Int64("bytes", request.Bytes),
		slog.String("user", request.User),
		slog.String("ip", request.IP),
	}
	n := 8
	if request.ProxyID != "" {
		attrs[n] = slog.String(observe.ProxyRequestIDKey, request.ProxyID)
		n++
	}
	if request.Aborted {
		attrs[n] = slog.Bool("aborted", true)
		n++
	}
	logger.LogAttrs(ctx, level, "request", attrs[:n]...)
}

// RecoverPanics turns a handler's panic into the generic 500, with the
// request's ID, once it's logged at error with the stack (and handed to
// observe.Report): never the panic's value, which stays in the log, and
// even there only as observe.PanicValue allows. It runs just inside
// Security for the API (whose headers it keeps), and around the pages.
// Headers a handler set before panicking (a cookie) are dropped. If the
// answer had already started (a status written, or a flush), nothing more
// can be written: the connection is cut instead (http.ErrAbortHandler), so
// a client never takes half an answer for a whole one.
// http.ErrAbortHandler itself passes through, as net/http expects.
// ClientAddress catches what gets past it (finishRequest).
func RecoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := observe.FromContext(r.Context())
		if request == nil {
			// Outside ClientAddress (a handler under test): track writes here.
			request = observe.Begin(w, ClientIP(r))
			w = request
		}
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if p == http.ErrAbortHandler {
				request.Aborted = true
				panic(p)
			}
			logPanic(r, p)
			if request.Written() {
				request.Aborted = true
				panic(http.ErrAbortHandler)
			}
			answerPanic(w, r, request)
		}()
		next.ServeHTTP(w, r)
	})
}

// answerPanic answers a panic before anything was written: the headers a
// handler set dropped, the API's security headers and the request's ID set
// again, and the generic 500.
func answerPanic(w http.ResponseWriter, r *http.Request, request *observe.Request) {
	header := w.Header()
	clear(header)
	apiHeaders(header)
	if request.ID != "" {
		header[requestIDHeader] = request.IDHeader()
	}
	answerInternalError(w, r)
}

// logPanic logs a panic at error with its stack and reports it. Its value
// is logged only as observe.PanicValue allows (a request, headers or a URL
// redacted; a message cut to 512 bytes; anything else by its type), then
// scrubbed (observe.Scrub); the stack only as function names and lines
// (observe.ScrubStack).
func logPanic(r *http.Request, p any) {
	ctx := r.Context()
	stack := debug.Stack()
	value := observe.PanicValue(p)
	route := observe.Route(ctx)
	slog.ErrorContext(ctx, "request panicked", "route", route, "panic", observe.Scrub(value), "stack", observe.ScrubStack(string(stack)))
	observe.Report(ctx, observe.Problem{Source: observe.SourcePanic, Route: route, RequestID: observe.RequestID(ctx), Err: errors.New(value), Stack: stack})
}

// reportFailure logs err at error as msg, scrubbed (observe.ErrorAttr), with
// the request's ID and route, and hands it to observe.Report as the
// request's problem. InternalError answers it with a 500; the page gates
// serve the page anyway.
func reportFailure(r *http.Request, msg string, err error) {
	ctx := r.Context()
	route := observe.Route(ctx)
	slog.ErrorContext(ctx, msg, "route", route, observe.ErrorAttr(err))
	observe.Report(ctx, observe.Problem{Source: observe.SourceRequest, Route: route, RequestID: observe.RequestID(ctx), Err: err})
}

// ClientGone reports whether err is only the request's context ending
// because the client went away (closed the tab, navigated, timed out on its
// side): context.Canceled, with the request's own context canceled. Nothing
// failed on our side, and nobody is there to answer, so it's no 500 and no
// grouped error. A deadline (the 10 s request timeout) isn't this: that is
// a failure.
func ClientGone(r *http.Request, err error) bool {
	return errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled)
}

// abortedByClient records a request ClientGone ended: logged at debug, and
// its access line says aborted (status 0 when nothing was written).
func abortedByClient(r *http.Request, err error) {
	ctx := r.Context()
	if request := observe.FromContext(ctx); request != nil {
		request.Aborted = true
	}
	slog.DebugContext(ctx, "request aborted by the client", "route", observe.Route(ctx), observe.ErrorAttr(err))
}

// somethingWentWrong is every 500's message: what failed is in the log, under
// the request's ID, never in the answer.
const somethingWentWrong = "Something went wrong. Please try again."

// internalErrorBody is a 500's answer: the generic message and the
// request's ID, which the web app shows as its reference.
type internalErrorBody struct {
	Error     string `json:"error"`
	RequestID string `json:"requestId,omitempty"`
}

// answerInternalError writes the generic 500 with the request's ID.
func answerInternalError(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusInternalServerError, internalErrorBody{Error: somethingWentWrong, RequestID: observe.RequestID(r.Context())})
}

// ErrDraining is readiness's answer once shutdown has begun.
var ErrDraining = errors.New("shutting down")

// Drain marks the server as shutting down: from now on GET /api/health
// answers 503, so a load balancer stops sending it requests while those in
// flight finish. cmd/server calls it before http.Server.Shutdown.
func (s *Server) Drain() {
	s.draining.Store(true)
}

// Ready is readiness (GET /api/health): nil when the server isn't shutting
// down and its database answers.
func (s *Server) Ready(r *http.Request) error {
	if s.draining.Load() {
		return ErrDraining
	}
	return s.Repo.DB.PingContext(r.Context())
}
