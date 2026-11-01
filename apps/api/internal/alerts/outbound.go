package alerts

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// The alerts' outbound requests (the webhook, the heartbeat) go only to
// URLs from the server's configuration (ALERT_WEBHOOK_URL,
// ALERT_HEARTBEAT_URL), never from a request. Even so the client is
// careful: no redirect is followed, no proxy from the environment is used,
// every step has a timeout, at most a little of an answer is read, and
// with Guard on (production, unless ALERT_ALLOW_PRIVATE_TARGETS) it won't
// connect to a loopback, private, link-local or otherwise non-public
// address, checked on the address it actually dials, after DNS, so a name
// that resolves to one (or starts to) can't reach the server's own
// network. Errors never hold the URL: it can be a secret (a Slack webhook
// is its URL).

// Outbound request bounds.
const (
	dialTimeout    = 5 * time.Second
	maxAnswerBytes = 4 << 10
)

// ErrPrivateTarget: the URL's host is, or resolves to, an address that
// isn't public, and Guard refused it.
var ErrPrivateTarget = errors.New("refused to connect to a non-public address (set ALERT_ALLOW_PRIVATE_TARGETS=true for an internal receiver)")

// NewClient is the alerts' HTTP client: timeout for the whole request,
// with guard refusing non-public addresses.
func NewClient(timeout time.Duration, guard bool) *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout}
	if guard {
		dialer.Control = refusePrivate
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    dialTimeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 16 << 10,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           2,
		IdleConnTimeout:        90 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// refusePrivate is the dialer's check of the address it's about to
// connect to.
func refusePrivate(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !PublicAddress(ap.Addr()) {
		return ErrPrivateTarget
	}
	return nil
}

// notPublic are ranges netip doesn't flag that no receiver should be in:
// shared address space (carrier-grade NAT), IETF protocol assignments,
// benchmarking, reserved, and NAT64 and 6to4, which can map to any IPv4
// address, private ones included.
var notPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
}

// PublicAddress reports whether addr is a public unicast address: not
// loopback, private, link-local, multicast, unspecified, nor in notPublic.
// An IPv4-mapped IPv6 address is judged as its IPv4 address.
func PublicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.Zone() != "" || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// send makes req with client and checks the answer is a 2xx, reading at
// most maxAnswerBytes of it. Its error never holds the URL.
func send(client *http.Client, req *http.Request) error {
	res, err := client.Do(req)
	if err != nil {
		return withoutURL(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxAnswerBytes))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("answered %d", res.StatusCode)
	}
	return nil
}

// withoutURL is err without the URL net/http puts in it.
func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, ErrPrivateTarget) {
		return ErrPrivateTarget
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("timed out")
	}
	return err
}
