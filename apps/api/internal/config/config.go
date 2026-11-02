package config

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL, Origin, Address string
	Production                   bool
	// AppName is the product name (APP_NAME), as in the web build: emails
	// open with it.
	AppName string
	// Mail is how email is delivered and who it comes from (mail.go).
	Mail
	// EmailVerification is whether emails are verified with a code
	// (verification.go).
	EmailVerification
	// Captcha is the CAPTCHA provider for rate limits' challenge levels
	// (captcha.go).
	Captcha
	// PerIPLimitsOff is PER_IP_LIMITS=off (on is the default, and the zero
	// value): every level of every per-IP limit is never, for a proxy that
	// can't send the client's address.
	PerIPLimitsOff bool
	// Policies (POLICIES=on) publishes the terms of service and privacy
	// policy and makes sign-up agree to them (policies.go). Off by default.
	Policies bool
	// AppKey (APP_KEY, 32 bytes) seals MFA secrets at rest
	// (encryption.go, auth.Keys).
	AppKey []byte
	// AdminRouteKey (ADMIN_ROUTE_KEY) is the admin dashboard's secret path
	// segment, and every admin API call's X-Admin-Key. Empty: admin is off.
	AdminRouteKey string
	// AdminRouteKeyPrevious (ADMIN_ROUTE_KEY_PREVIOUS) is the key being
	// retired: admin sessions made under it keep working, but it opens no
	// sign-in (admin_key.go). Empty: none.
	AdminRouteKeyPrevious string
	// AdminAllowedIPs (ADMIN_ALLOWED_IPS) are the only client addresses the
	// admin dashboard answers; empty allows any.
	AdminAllowedIPs []netip.Prefix
	// RateLimits is RateLimitsOff to switch every rate limit off, for the
	// end-to-end tests; quotas and the hashing slots stay. Production
	// refuses it.
	RateLimits string
	// ClientIPHeader is the header a proxy puts the client's address in, in
	// canonical form, believed only on connections from TrustedProxies. Both
	// are empty, the default, to use the connection's address.
	ClientIPHeader string
	TrustedProxies []netip.Prefix
	// Logging is the log's format and level, and which requests get an
	// access line (logging.go).
	Logging
	// Metrics is what the server counts, where it keeps it and who may
	// read it (metrics.go).
	Metrics
	// Alerts is where alerts go besides admins' email (alerts.go).
	Alerts
	// ShutdownDelay (SHUTDOWN_DELAY) is how long the server keeps answering,
	// with readiness already 503, before it stops taking connections: time
	// for a load balancer polling GET /api/health to take it out.
	ShutdownDelay time.Duration
}

const (
	RateLimitsOn  = "on"
	RateLimitsOff = "off"
)

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Origin: env("APP_ORIGIN", "http://localhost:3000"), Address: env("API_ADDR", "127.0.0.1:8080"), Production: os.Getenv("APP_ENV") == "production", AppName: env("APP_NAME", "lewcontext")}
	if err := c.loadMail(); err != nil {
		return c, err
	}
	if err := c.loadEmailVerification(); err != nil {
		return c, err
	}
	if err := c.loadCaptcha(); err != nil {
		return c, err
	}
	if err := c.loadPolicies(); err != nil {
		return c, err
	}
	if err := c.loadAppKey(); err != nil {
		return c, err
	}
	if err := c.loadAdminRouteKey(); err != nil {
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
	if c.Production && c.PerIPLimitsOff && c.ClientIPHeader != "" {
		return c, errors.New("PER_IP_LIMITS=off with CLIENT_IP_HEADER set: the proxy sends each client's address, so per-IP limits can count it; remove PER_IP_LIMITS=off (it's only for a proxy that can't send the address)")
	}
	logging, err := LoadLogging()
	if err != nil {
		return c, err
	}
	c.Logging = logging
	if c.ShutdownDelay, err = loadShutdownDelay(c.Production); err != nil {
		return c, err
	}
	if err := c.loadMetrics(); err != nil {
		return c, err
	}
	if err := c.loadAlerts(); err != nil {
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
	proxies, err := parseRanges("TRUSTED_PROXIES", os.Getenv("TRUSTED_PROXIES"))
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

// parseRanges parses a comma-separated list of addresses and CIDR ranges,
// the value of the variable name (TRUSTED_PROXIES, ADMIN_ALLOWED_IPS).
func parseRanges(name, list string) ([]netip.Prefix, error) {
	var ranges []netip.Prefix
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		r, err := parseRange(name, entry)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	return ranges, nil
}

// parseRange parses one entry: a CIDR range, or a single address as a range
// of one. A range covering every address is refused: in TRUSTED_PROXIES it
// would believe the header from anyone, in ADMIN_ALLOWED_IPS it would allow
// everyone while looking like a restriction.
func parseRange(name, entry string) (netip.Prefix, error) {
	if !strings.Contains(entry, "/") {
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("%s: %q isn't an address or CIDR range", name, entry)
		}
		addr = addr.Unmap().WithZone("")
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	r, err := netip.ParsePrefix(entry)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%s: %q isn't an address or CIDR range", name, entry)
	}
	// Client addresses are unmapped before they're compared, so an
	// IPv4-mapped range would never match.
	if r.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("%s: write %q as an IPv4 range", name, entry)
	}
	if r.Bits() == 0 {
		return netip.Prefix{}, fmt.Errorf("%s: %q covers every address", name, entry)
	}
	return r.Masked(), nil
}

// maxShutdownDelay bounds SHUTDOWN_DELAY: past it, platforms kill the
// server before its 10 s drain could finish anyway.
const maxShutdownDelay = time.Minute

// loadShutdownDelay reads SHUTDOWN_DELAY, a Go duration ("5s") from 0 to a
// minute: 5 s by default in production, 0 elsewhere.
func loadShutdownDelay(production bool) (time.Duration, error) {
	fallback := "0s"
	if production {
		fallback = "5s"
	}
	delay, err := time.ParseDuration(env("SHUTDOWN_DELAY", fallback))
	if err != nil || delay < 0 || delay > maxShutdownDelay {
		return 0, errors.New("SHUTDOWN_DELAY must be a duration from 0s to 60s, such as 5s")
	}
	return delay, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
