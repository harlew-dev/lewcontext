package config

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL, Origin, Address string
	Production                   bool
	// Mail is how email is delivered and who it comes from (mail.go).
	Mail
	// RateLimits is RateLimitsOff to switch every rate limit off, for the
	// end-to-end tests; quotas and the hashing slots stay. Production
	// refuses it.
	RateLimits string
	// ClientIPHeader is the header a proxy puts the client's address in, in
	// canonical form, believed only on connections from TrustedProxies. Both
	// are empty, the default, to use the connection's address.
	ClientIPHeader string
	TrustedProxies []netip.Prefix
}

const (
	RateLimitsOn  = "on"
	RateLimitsOff = "off"
)

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Origin: env("APP_ORIGIN", "http://localhost:3000"), Address: env("API_ADDR", "127.0.0.1:8080"), Production: os.Getenv("APP_ENV") == "production"}
	if err := c.loadMail(); err != nil {
		return c, err
	}
	c.RateLimits = env("RATE_LIMITS", RateLimitsOn)
	if c.RateLimits != RateLimitsOn && c.RateLimits != RateLimitsOff {
		return c, errors.New("RATE_LIMITS must be on or off")
	}
	if c.Production && c.RateLimits == RateLimitsOff {
		return c, errors.New("RATE_LIMITS=off is for tests only; production keeps its rate limits")
	}
	if err := c.loadClientIP(); err != nil {
		return c, err
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return c, errors.New("APP_ORIGIN must be an exact http(s) origin without trailing slash")
	}
	if c.Production && u.Scheme != "https" {
		return c, errors.New("production APP_ORIGIN requires HTTPS")
	}
	if c.Production {
		db, err := url.Parse(c.DatabaseURL)
		if err != nil || db.Query().Get("sslmode") != "verify-full" {
			return c, errors.New("production DATABASE_URL requires sslmode=verify-full")
		}
	}
	return c, nil
}

// clientIPHeaders are the headers CLIENT_IP_HEADER may name. The RFC 7239
// Forwarded header isn't one: few proxies send it.
var clientIPHeaders = []string{"X-Forwarded-For", "X-Real-Ip", "Cf-Connecting-Ip", "True-Client-Ip"}

// loadClientIP reads CLIENT_IP_HEADER and TRUSTED_PROXIES, which only make
// sense together. A header believed from everyone would let any client pick
// its own address, so ranges that cover everyone are refused.
func (c *Config) loadClientIP() error {
	header := os.Getenv("CLIENT_IP_HEADER")
	proxies, err := parseProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	if header == "" && len(proxies) == 0 {
		return nil
	}
	if header == "" || len(proxies) == 0 {
		return errors.New("CLIENT_IP_HEADER and TRUSTED_PROXIES must be set together")
	}
	c.ClientIPHeader = http.CanonicalHeaderKey(header)
	for _, known := range clientIPHeaders {
		if c.ClientIPHeader == known {
			c.TrustedProxies = proxies
			return nil
		}
	}
	return errors.New("CLIENT_IP_HEADER must be X-Forwarded-For, X-Real-IP, CF-Connecting-IP or True-Client-IP")
}

// parseProxies parses a comma-separated list of addresses and CIDR ranges.
func parseProxies(list string) ([]netip.Prefix, error) {
	var proxies []netip.Prefix
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		proxy, err := parseProxy(entry)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, proxy)
	}
	return proxies, nil
}

// parseProxy parses one TRUSTED_PROXIES entry: a CIDR range, or a single
// address as a range of one.
func parseProxy(entry string) (netip.Prefix, error) {
	if !strings.Contains(entry, "/") {
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("TRUSTED_PROXIES: %q isn't an address or CIDR range", entry)
		}
		addr = addr.Unmap().WithZone("")
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	proxy, err := netip.ParsePrefix(entry)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("TRUSTED_PROXIES: %q isn't an address or CIDR range", entry)
	}
	// Client addresses are unmapped before they're compared, so an
	// IPv4-mapped range would never match.
	if proxy.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("TRUSTED_PROXIES: write %q as an IPv4 range", entry)
	}
	if proxy.Bits() == 0 {
		return netip.Prefix{}, fmt.Errorf("TRUSTED_PROXIES: %q trusts every address, which is the same as believing the header from anyone", entry)
	}
	return proxy.Masked(), nil
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
