package clientip

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestResolveClientIP(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	xff := "X-Forwarded-For"
	many := strings.Repeat("10.0.0.9,", 25) + "10.0.0.8"
	for _, c := range []struct {
		name, header, conn string
		values             []string
		want               string
	}{
		{"no header configured", "", "203.0.113.1:1234", []string{"198.51.100.1"}, "203.0.113.1"},
		{"untrusted connection ignores the header", xff, "203.0.113.1:1234", []string{"198.51.100.1"}, "203.0.113.1"},
		{"untrusted connection, single-value header", "Cf-Connecting-Ip", "203.0.113.1:1234", []string{"198.51.100.1"}, "203.0.113.1"},
		{"trusted connection without the header", xff, "10.0.0.1:1234", nil, "10.0.0.1"},

		{"one proxy", xff, "10.0.0.1:1234", []string{"198.51.100.1"}, "198.51.100.1"},
		{"the rightmost untrusted entry wins", xff, "10.0.0.1:1234", []string{"198.51.100.1, 198.51.100.2"}, "198.51.100.2"},
		{"a forged entry is never chosen", xff, "10.0.0.1:1234", []string{"6.6.6.6, 198.51.100.1"}, "198.51.100.1"},
		{"several proxy hops", xff, "10.0.0.1:1234", []string{"6.6.6.6, 198.51.100.1, 10.0.0.3, 10.0.0.2"}, "198.51.100.1"},
		{"every entry trusted gives the leftmost", xff, "10.0.0.1:1234", []string{"10.0.0.3, 10.0.0.2"}, "10.0.0.3"},
		{"an unparseable entry stops at the last trusted hop", xff, "10.0.0.1:1234", []string{"198.51.100.1, garbage, 10.0.0.2"}, "10.0.0.2"},
		{"an unparseable last entry gives the connection", xff, "10.0.0.1:1234", []string{"198.51.100.1:443"}, "10.0.0.1"},
		{"an empty entry stops too", xff, "10.0.0.1:1234", []string{"198.51.100.1,,10.0.0.2"}, "10.0.0.2"},
		{"several header lines are joined in order", xff, "10.0.0.1:1234", []string{"6.6.6.6, 198.51.100.1", "10.0.0.2"}, "198.51.100.1"},
		{"at most 20 entries are read", xff, "10.0.0.1:1234", []string{"198.51.100.1," + many}, "10.0.0.9"},
		{"IPv6 proxy and client", xff, "[::1]:1234", []string{"2001:db8::1"}, "2001:db8::1"},

		{"one valid value", "Cf-Connecting-Ip", "10.0.0.1:1234", []string{" 198.51.100.1 "}, "198.51.100.1"},
		{"two values fall back", "X-Real-Ip", "10.0.0.1:1234", []string{"198.51.100.1", "198.51.100.2"}, "10.0.0.1"},
		{"a list falls back", "True-Client-Ip", "10.0.0.1:1234", []string{"198.51.100.1, 198.51.100.2"}, "10.0.0.1"},
		{"a bad value falls back", "X-Real-Ip", "10.0.0.1:1234", []string{"not an address"}, "10.0.0.1"},

		{"IPv4-mapped connection is unmapped", "", "[::ffff:203.0.113.1]:1234", nil, "203.0.113.1"},
		{"IPv4-mapped connection matches an IPv4 proxy", xff, "[::ffff:10.0.0.1]:1234", []string{"198.51.100.1"}, "198.51.100.1"},
		{"IPv4-mapped header value is unmapped", "X-Real-Ip", "10.0.0.1:1234", []string{"::ffff:198.51.100.1"}, "198.51.100.1"},
		{"zones are dropped", xff, "[fe80::1%eth0]:1234", nil, "fe80::1"},
		{"zones are dropped from header values", xff, "10.0.0.1:1234", []string{"fe80::2%eth0"}, "fe80::2"},
		{"an address without a port is kept as it is", xff, "unix", []string{"198.51.100.1"}, "unix"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/health", nil)
			r.RemoteAddr = c.conn
			for _, v := range c.values {
				r.Header.Add(c.header, v)
			}
			if got := Resolve(r, c.header, proxies); got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
}
