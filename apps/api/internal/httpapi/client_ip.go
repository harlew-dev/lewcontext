package httpapi

import (
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/clientip"
	"lewcontext/api/internal/observe"
	"net/http"
	"time"
)

// ClientAddress is the outermost middleware, for the API and the pages
// alike. It works out the request's client address once (the header is
// believed only when the connection comes from a trusted proxy) and gives
// the request its ID (requestID, always ours), both kept in the context for limits, the
// events.ip and events.request_id columns (events.WithRequest) and every log
// line; sends the ID back in X-Request-ID; and logs the request's access
// line when it's done, catching a panic nothing inside caught
// (finishRequest, observability.go).
func (s *Server) ClientAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ip := clientip.Resolve(r, s.Config.ClientIPHeader, s.Config.TrustedProxies)
		request := observe.Begin(w, ip)
		id := s.requestID(r, request)
		w.Header()[requestIDHeader] = request.IDHeader()
		ctx := events.WithRequest(observe.WithRequest(r.Context(), request), ip, id)
		r = r.WithContext(ctx)
		defer s.finishRequest(request, r, request, start)
		next.ServeHTTP(request, r)
	})
}

// ClientIP is the address every limit and event uses: the one ClientAddress
// worked out, or the connection's outside it.
func ClientIP(r *http.Request) string {
	if request := observe.FromContext(r.Context()); request != nil {
		return request.IP
	}
	return clientip.Resolve(r, "", nil)
}
