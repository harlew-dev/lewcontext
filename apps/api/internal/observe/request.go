package observe

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
)

// Request is one request as its access line reports it, filled in as the
// request passes through the server: its ID and client address when it
// arrives (httpapi.ClientAddress), the route once one matches (SetRoute),
// the user once a session is found (SetUser), and the status and size as
// the answer is written. It is also the response writer every handler
// writes to, which is how it sees them. One per request, in the request's
// context (WithRequest); only the request's own goroutine touches it.
type Request struct {
	http.ResponseWriter
	// ID is the request's ID, always ours (NewRequestID): sent back in
	// X-Request-ID, on every log line and in events.request_id.
	ID string
	// ProxyID is the X-Request-ID a trusted proxy sent, in the safe shape
	// (ValidRequestID), or "". It goes only on the access line, as
	// proxyRequestId, to find the proxy's own line: a proxy that passes a
	// client's header on unchanged (most do) would let the client choose
	// it, so it is never our ID.
	ProxyID string
	// IP is the client's address (httpapi.ClientIP).
	IP string
	// Route is the matched route's pattern ("GET /api/workspaces/{workspace}")
	// or a page's fixed label ("page /app/*"), never the path: paths hold
	// reset and invitation tokens and the admin key. Unmatched until set.
	Route string
	// User is the signed-in account's usr_ ID, "admin" for an admin
	// session, or "" signed out.
	User string
	// Status is the status written, 0 until the handler writes one.
	Status int
	// Bytes is how much of the body was written.
	Bytes int64
	// Aborted is set when a handler ended the request with
	// http.ErrAbortHandler: the connection is dropped with no answer.
	Aborted bool
	// TimedOut is set when the request's 10 s deadline (httpapi.Security)
	// had passed by the time its handler returned: on a write, the change
	// may have committed though the client saw an error.
	TimedOut bool
	// idHeader backs the X-Request-ID header's value, so setting it costs
	// nothing more.
	idHeader [1]string
	// idBytes is where NewRequestID's characters are made.
	idBytes [len(requestIDPrefix) + requestIDLength]byte
}

// AdminUser is the access line's user for an admin session.
const AdminUser = "admin"

// Unmatched is the route of a request no route or page matched.
const Unmatched = "unmatched"

// Begin starts a request's record around w, with its client address. Give
// it its ID with NewID before anything is written.
func Begin(w http.ResponseWriter, ip string) *Request {
	return &Request{ResponseWriter: w, IP: ip, Route: Unmatched}
}

// NewID gives the request a new ID (NewRequestID) and returns it.
func (q *Request) NewID() string {
	return q.setID(newRequestID(&q.idBytes))
}

func (q *Request) setID(id string) string {
	q.ID = id
	q.idHeader[0] = id
	return id
}

// IDHeader is the X-Request-ID header's value: the ID, without allocating
// a slice per request.
func (q *Request) IDHeader() []string {
	return q.idHeader[:]
}

// Written reports whether the status line has been written, after which no
// other answer can be.
func (q *Request) Written() bool {
	return q.Status != 0
}

// WriteHeader records the status, then writes it. Informational (1xx)
// statuses come before the real one, so they aren't recorded.
func (q *Request) WriteHeader(status int) {
	if q.Status == 0 && status >= 200 {
		q.Status = status
	}
	q.ResponseWriter.WriteHeader(status)
}

func (q *Request) Write(body []byte) (int, error) {
	if q.Status == 0 {
		q.Status = http.StatusOK
	}
	n, err := q.ResponseWriter.Write(body)
	q.Bytes += int64(n)
	return n, err
}

// ReadFrom keeps the writer's own ReadFrom (http.ServeContent's fast path)
// and counts what it wrote.
func (q *Request) ReadFrom(src io.Reader) (int64, error) {
	if q.Status == 0 {
		q.Status = http.StatusOK
	}
	var n int64
	var err error
	if from, ok := q.ResponseWriter.(io.ReaderFrom); ok {
		n, err = from.ReadFrom(src)
	} else {
		n, err = io.Copy(struct{ io.Writer }{q.ResponseWriter}, src)
	}
	q.Bytes += n
	return n, err
}

// Flush sends what's written so far, which starts the answer: a status
// written implicitly is recorded first, so nothing can be answered after it
// (http.Flusher).
func (q *Request) Flush() {
	_ = q.FlushError()
}

// FlushError is Flush, with the error (http.ResponseController).
func (q *Request) FlushError() error {
	if q.Status == 0 {
		q.Status = http.StatusOK
	}
	return http.NewResponseController(q.ResponseWriter).Flush()
}

// Unwrap is for http.ResponseController (flushing, deadlines).
func (q *Request) Unwrap() http.ResponseWriter {
	return q.ResponseWriter
}

type requestKey struct{}

// WithRequest puts q in ctx, for the functions below.
func WithRequest(ctx context.Context, q *Request) context.Context {
	return context.WithValue(ctx, requestKey{}, q)
}

// FromContext is the request in ctx, or nil outside one (the CLI, the event
// worker, a handler tested without httpapi.ClientAddress).
func FromContext(ctx context.Context) *Request {
	q, _ := ctx.Value(requestKey{}).(*Request)
	return q
}

// RequestID is the ID of the request in ctx, or "" outside one.
func RequestID(ctx context.Context) string {
	if q := FromContext(ctx); q != nil {
		return q.ID
	}
	return ""
}

// Route is the route of the request in ctx so far, or "" outside one.
func Route(ctx context.Context) string {
	if q := FromContext(ctx); q != nil {
		return q.Route
	}
	return ""
}

// SetRoute names the request's route: a registered pattern or a fixed page
// label, never anything from the request itself. A later call replaces an
// earlier one (the page gate names /admin/*, then the 404 it answers a
// wrong key with names itself).
func SetRoute(ctx context.Context, route string) {
	if q := FromContext(ctx); q != nil {
		q.Route = route
	}
}

// SetUser names who is signed in: a usr_ public ID, or "admin" for an
// admin session. Never an email or an internal ID.
func SetUser(ctx context.Context, user string) {
	if q := FromContext(ctx); q != nil {
		q.User = user
	}
}

// Request IDs are req_ and 14 base62 characters (about 83 random bits), as
// the database's public IDs are, made in Go from crypto/rand.
const (
	requestIDPrefix = "req_"
	requestIDLength = 14
	base62          = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// NewRequestID is a new request ID: req_ and 14 base62 characters.
func NewRequestID() string {
	var b [len(requestIDPrefix) + requestIDLength]byte
	return newRequestID(&b)
}

// NewServerID is a new server's ID (server_instances.id): srv_ and 14
// base62 characters, made like a request ID, new at every start.
func NewServerID() string {
	var b [len(serverIDPrefix) + requestIDLength]byte
	return newID(&b, serverIDPrefix)
}

// serverIDPrefix is a server ID's prefix; as long as requestIDPrefix.
const serverIDPrefix = "srv_"

// newRequestID makes a request ID in b.
func newRequestID(b *[len(requestIDPrefix) + requestIDLength]byte) string {
	return newID(b, requestIDPrefix)
}

// newID makes an ID with prefix in b: one 16-byte read from crypto/rand,
// more only if too many bytes were skipped. Bytes 248 and above are skipped
// so that each character is equally likely (248 = 4 × 62).
func newID(b *[len(requestIDPrefix) + requestIDLength]byte, prefix string) string {
	copy(b[:], prefix)
	n := len(prefix)
	var random [16]byte
	for n < len(b) {
		_, _ = rand.Read(random[:]) // never fails (crypto/rand)
		for _, r := range random {
			if r >= 248 {
				continue
			}
			b[n] = base62[r%62]
			n++
			if n == len(b) {
				break
			}
		}
	}
	return string(b[:])
}

// Method is r's method as logged (and, later, as a metric's label): one of
// the methods HTTP defines, or OTHER, so a client can't fill a line with an
// invented one.
func Method(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return method
	}
	return "OTHER"
}

// maxRequestID is the longest ID taken from a trusted proxy.
const maxRequestID = 64

// ValidRequestID reports whether a trusted proxy's X-Request-ID may be
// logged (as proxyRequestId): 1 to 64 of A–Z a–z 0–9 . _ -. Nothing else,
// so it can't break a log line.
func ValidRequestID(id string) bool {
	if len(id) == 0 || len(id) > maxRequestID {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
