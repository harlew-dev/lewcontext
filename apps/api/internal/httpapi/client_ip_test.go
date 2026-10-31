package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Every handler sees the address clientAddress worked out.
func TestClientAddressMiddleware(t *testing.T) {
	s := &Server{}
	s.Config.ClientIPHeader = "X-Real-Ip"
	s.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	var seen string
	h := s.clientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = ClientIP(r) }))
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Real-IP", "198.51.100.1")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if seen != "198.51.100.1" {
		t.Fatalf("handler saw %q", seen)
	}
	// Outside the middleware, the connection's address.
	if got := ClientIP(r); got != "127.0.0.1" {
		t.Fatalf("without the middleware: %q", got)
	}
}
