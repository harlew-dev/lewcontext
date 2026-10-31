package httpapi

import (
	"context"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/clientip"
	"net/http"
)

type clientIPKey struct{}

// ClientAddress works out the request's client address once, before any
// other handler, and stores it in the context, for limits and for the
// events.ip column (events.WithIP). The header is believed only when the
// connection comes from a trusted proxy.
func (s *Server) ClientAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientip.Resolve(r, s.Config.ClientIPHeader, s.Config.TrustedProxies)
		ctx := events.WithIP(context.WithValue(r.Context(), clientIPKey{}, ip), ip)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClientIP is the address every limit and event uses: the one ClientAddress
// worked out, or the connection's outside it.
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok {
		return ip
	}
	return clientip.Resolve(r, "", nil)
}
