package config

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"strconv"
	"time"
)

// Metrics is what the server counts, where it keeps it and who may read
// it (docs/api/observability.md#metrics).
type Metrics struct {
	// MetricsStore (METRICS_STORE) is whether the minute's metrics are
	// written to the database: MetricsStoreOn (the default) or
	// MetricsStoreOff. /metrics, logs and grouped errors work either way.
	MetricsStore string
	// MetricsAddr (METRICS_ADDR) is the separate listener /metrics is
	// served on, such as 127.0.0.1:9090; empty (the default) serves none.
	MetricsAddr string
	// MetricsToken (METRICS_TOKEN), when set, is the bearer token /metrics
	// requires. Production refuses a METRICS_ADDR on every interface
	// without one.
	MetricsToken string
	// SlowQuery (SLOW_QUERY_MS, 500 by default) is how long a query may
	// take before it's logged as slow; 0 logs none.
	SlowQuery time.Duration
}

// METRICS_STORE's values.
const (
	MetricsStoreOn  = "on"
	MetricsStoreOff = "off"
)

// minMetricsToken is METRICS_TOKEN's shortest length: a token anyone can
// try against the listener as often as they like has to be long.
const minMetricsToken = 32

// maxSlowQuery bounds SLOW_QUERY_MS at a minute: past the request's own
// 10 s deadline nothing would be slow.
const maxSlowQuery = 60000

func (c *Config) loadMetrics() error {
	c.MetricsStore = env("METRICS_STORE", MetricsStoreOn)
	if c.MetricsStore != MetricsStoreOn && c.MetricsStore != MetricsStoreOff {
		return errors.New("METRICS_STORE must be on or off")
	}
	c.MetricsToken = os.Getenv("METRICS_TOKEN")
	if c.MetricsToken != "" && len(c.MetricsToken) < minMetricsToken {
		return errors.New("METRICS_TOKEN must be at least 32 characters (openssl rand -base64 32)")
	}
	if err := c.loadMetricsAddr(os.Getenv("METRICS_ADDR")); err != nil {
		return err
	}
	ms, err := strconv.Atoi(env("SLOW_QUERY_MS", "500"))
	if err != nil || ms < 0 || ms > maxSlowQuery {
		return errors.New("SLOW_QUERY_MS must be a number of milliseconds from 0 to 60000")
	}
	c.SlowQuery = time.Duration(ms) * time.Millisecond
	return nil
}

// loadMetricsAddr checks METRICS_ADDR: a host and port, never the API's
// own address. In production, a listener on every interface (no host,
// 0.0.0.0 or ::) needs METRICS_TOKEN: /metrics would otherwise be open to
// anyone who can reach the server.
func (c *Config) loadMetricsAddr(addr string) error {
	if addr == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if n, perr := strconv.Atoi(port); err != nil || perr != nil || n < 1 || n > 65535 {
		return errors.New("METRICS_ADDR must be a host and port, such as 127.0.0.1:9090")
	}
	if addr == c.Address {
		return errors.New("METRICS_ADDR must differ from API_ADDR: /metrics is never on the public listener")
	}
	if c.Production && c.MetricsToken == "" && !privateHost(host) {
		return errors.New("METRICS_ADDR isn't a loopback or private address: set METRICS_TOKEN, or listen on a private address such as 127.0.0.1:9090")
	}
	c.MetricsAddr = addr
	return nil
}

// privateHost reports whether a listener on host takes connections only
// from this machine or a private network: a loopback or private address
// (127.0.0.1, ::1, 10.0.0.5, fd00::5), or localhost. Anything else could be
// reachable from anywhere: no host, an unspecified address (0.0.0.0, ::,
// with a zone or not, ::ffff:0.0.0.0), a public or link-local address, or
// another name, which can't be told apart from one. A zone is refused
// outright: [::%eth0] listens on every interface.
func privateHost(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate()
}
