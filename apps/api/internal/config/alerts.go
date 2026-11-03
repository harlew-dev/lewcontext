package config

import (
	"errors"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// Alerts holds the one setting alerts still read from the environment.
// Where alerts go (webhooks and heartbeats) is managed in the admin
// dashboard, sealed in the database (docs/api/monitoring.md#alerts), never
// configured here: a URL can be a secret.
type Alerts struct {
	// AlertAllowPrivateTargets (ALERT_ALLOW_PRIVATE_TARGETS) lets a webhook
	// or heartbeat reach a loopback, private or link-local address in
	// production (an internal relay). Off, the default, refuses them there,
	// checked when an operator saves one and on the address dialled, after DNS.
	AlertAllowPrivateTargets bool
}

// maxAlertURL bounds a receiver URL's length.
const maxAlertURL = 2048

func (c *Config) loadAlerts() error {
	switch os.Getenv("ALERT_ALLOW_PRIVATE_TARGETS") {
	case "", "false":
	case "true":
		c.AlertAllowPrivateTargets = true
	default:
		return errors.New("ALERT_ALLOW_PRIVATE_TARGETS must be true or false")
	}
	return nil
}

// ValidateAlertURL checks a receiver URL when an operator saves it and again
// before each use. It performs
// no network request; the outbound dialer separately guards resolved IPs.
func ValidateAlertURL(raw string, production, allowPrivate bool) error {
	const name = "Destination"
	u, err := url.Parse(raw)
	switch {
	case err != nil || len(raw) > maxAlertURL || u.Host == "" || u.Hostname() == "" || u.Opaque != "":
		return errors.New(name + " must be an absolute https URL")
	case u.Scheme != "https" && (production || u.Scheme != "http"):
		return errors.New(name + " must be an https URL (http only outside production)")
	case u.User != nil:
		return errors.New(name + " must not hold a user name or password")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return errors.New(name + " must not have a fragment (#)")
	case strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return errors.New(name + " must not hold spaces or line breaks")
	}
	if u.Port() != "" {
		n, err := strconv.Atoi(u.Port())
		if err != nil || n < 1 || n > 65535 {
			return errors.New("Destination has an invalid port")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return errors.New("Destination has an invalid port")
	}
	if production && !allowPrivate && !publicHost(u.Hostname()) {
		return errors.New(name + " points at a loopback, private or link-local address: production refuses that unless ALERT_ALLOW_PRIVATE_TARGETS=true")
	}
	return nil
}

// publicHost reports whether host may be a public receiver: a name other
// than localhost (checked again when dialled), or a public unicast
// address.
func publicHost(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		name := strings.TrimSuffix(strings.ToLower(host), ".")
		return name != "localhost" && !strings.HasSuffix(name, ".localhost")
	}
	addr = addr.Unmap()
	return addr.Zone() == "" && addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast()
}
