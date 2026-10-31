package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

type clientIPKey struct{}

// maxForwardedHops is how many X-Forwarded-For entries are read, from the
// right.
const maxForwardedHops = 20

// clientAddress works out the request's client address once, before any
// other handler, and stores it in the context. The header is believed only
// when the connection comes from a trusted proxy.
func (s *Server) clientAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := resolveClientIP(r, s.Config.ClientIPHeader, s.Config.TrustedProxies)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
	})
}

// clientIP is the address every limit and event uses: the one clientAddress
// worked out, or the connection's outside it.
func clientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok {
		return ip
	}
	return resolveClientIP(r, "", nil)
}

// resolveClientIP is the connection's address, or, on a connection from one
// of proxies, the client address the proxy put in header.
func resolveClientIP(r *http.Request, header string, proxies []netip.Prefix) string {
	conn, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return ip
	}
	addr := normalise(conn.Addr())
	if header == "" || !trusted(addr, proxies) {
		return addr.String()
	}
	if header == "X-Forwarded-For" {
		return forwardedFor(r.Header.Values(header), addr, proxies).String()
	}
	return singleValue(r.Header.Values(header), addr).String()
}

// forwardedFor reads X-Forwarded-For from the right, where each proxy appends
// the address it received from: the first entry that isn't a trusted proxy
// is the client. The client controls everything to its left, so an entry
// that doesn't parse stops the search at the last trusted hop, and if every
// entry read is trusted, the leftmost is used.
func forwardedFor(lines []string, conn netip.Addr, proxies []netip.Prefix) netip.Addr {
	entries := strings.Split(strings.Join(lines, ","), ",")
	client := conn
	for i := len(entries) - 1; i >= max(0, len(entries)-maxForwardedHops); i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(entries[i]))
		if err != nil {
			return client
		}
		client = normalise(addr)
		if !trusted(client, proxies) {
			return client
		}
	}
	return client
}

// singleValue reads a header holding one address (X-Real-IP,
// CF-Connecting-IP, True-Client-IP). Anything else falls back to conn.
func singleValue(values []string, conn netip.Addr) netip.Addr {
	if len(values) != 1 {
		return conn
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil {
		return conn
	}
	return normalise(addr)
}
func trusted(addr netip.Addr, proxies []netip.Prefix) bool {
	return slices.ContainsFunc(proxies, func(p netip.Prefix) bool { return p.Contains(addr) })
}

// normalise unmaps IPv4-mapped IPv6 addresses and drops zones, so one client
// has one spelling.
func normalise(addr netip.Addr) netip.Addr {
	return addr.Unmap().WithZone("")
}

// limitSubject is what a per-IP limit counts for ip: the address itself for
// IPv4, and its /64 for IPv6, since one IPv6 user usually holds a whole /64.
func limitSubject(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is6() {
		return ip
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}
