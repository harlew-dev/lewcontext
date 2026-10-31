// Package clientip works out a request's client address: the connection's,
// or, on a connection from a trusted proxy, the address the proxy put in a
// header (CLIENT_IP_HEADER, TRUSTED_PROXIES).
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// maxForwardedHops is how many X-Forwarded-For entries are read, from the
// right.
const maxForwardedHops = 20

// Resolve is the connection's address, or, on a connection from one
// of proxies, the client address the proxy put in header.
func Resolve(r *http.Request, header string, proxies []netip.Prefix) string {
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
