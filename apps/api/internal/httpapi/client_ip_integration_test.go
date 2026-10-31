package httpapi

import (
	"context"
	"lewcontext/api/internal/factory"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

const unknownLogin = `{"email":"nobody@example.com","password":"a wrong guess here"}`

// behindProxy is an integration server that believes X-Forwarded-For from
// 127.0.0.1, as behind nginx on the same machine.
func behindProxy(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	s, _ := integrationServer(t)
	s.Config.ClientIPHeader = "X-Forwarded-For"
	s.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	return s.Routes(), s
}

// loginFrom sends an unknown login from conn with X-Forwarded-For set to
// forwarded, and returns the status.
func loginFrom(h http.Handler, conn, forwarded string) int {
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(unknownLogin))
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-For", forwarded)
	r.RemoteAddr = conn
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// Behind a trusted proxy, each client address has its own auth:per-ip budget,
// and events record the client's address, not the proxy's.
func TestClientIPBehindATrustedProxy(t *testing.T) {
	h, s := behindProxy(t)
	f := factory.TestFaker(t)
	must(factory.RateLimits.State(factory.ForIP("203.0.113.7")).State(factory.Exhausted(60)).CreateOne(context.Background(), s.Store.DB, f))(t)
	if got := loginFrom(h, "127.0.0.1:1234", "6.6.6.6, 203.0.113.7"); got != 429 {
		t.Fatalf("exhausted client: %d", got)
	}
	if got := loginFrom(h, "127.0.0.1:1234", "203.0.113.8"); got != 401 {
		t.Fatalf("another client behind the same proxy: %d", got)
	}
	if e := onlyEvent(t, s.Store.DB, "rate_limit.reached"); e.Payload["ip"] != "203.0.113.7" {
		t.Errorf("rate limit recorded %v", e.Payload["ip"])
	}
	if e := onlyEvent(t, s.Store.DB, "auth.login_attempted"); e.Payload["ip"] != "203.0.113.8" {
		t.Errorf("login attempt recorded %v", e.Payload["ip"])
	}
}

// From a connection that isn't a trusted proxy, the header is ignored, so
// changing it doesn't reset the budget.
func TestClientIPFromAnUntrustedConnection(t *testing.T) {
	h, s := behindProxy(t)
	f := factory.TestFaker(t)
	must(factory.RateLimits.State(factory.ForIP("198.51.100.1")).State(factory.Exhausted(60)).CreateOne(context.Background(), s.Store.DB, f))(t)
	for _, forged := range []string{"203.0.113.1", "203.0.113.2", "127.0.0.1"} {
		if got := loginFrom(h, "198.51.100.1:1234", forged); got != 429 {
			t.Fatalf("forged %s reset the budget: %d", forged, got)
		}
	}
	if e := onlyEvent(t, s.Store.DB, "rate_limit.reached"); e.Payload["ip"] != "198.51.100.1" {
		t.Errorf("rate limit recorded %v", e.Payload["ip"])
	}
}

// IPv6 clients are counted by their /64; events keep the full address.
func TestIPv6CountedByItsSlash64(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	f := factory.TestFaker(t)
	must(factory.RateLimits.State(factory.ForIP("2001:db8:1:2::/64")).State(factory.Exhausted(60)).CreateOne(context.Background(), db, f))(t)
	if got := loginFrom(h, "[2001:db8:1:2:abcd::99]:1234", ""); got != 429 {
		t.Fatalf("an address in the exhausted /64: %d", got)
	}
	if got := loginFrom(h, "[2001:db8:1:3::99]:1234", ""); got != 401 {
		t.Fatalf("the next /64: %d", got)
	}
	if e := onlyEvent(t, db, "rate_limit.reached"); e.Payload["ip"] != "2001:db8:1:2:abcd::99" {
		t.Errorf("rate limit recorded %v", e.Payload["ip"])
	}
}
