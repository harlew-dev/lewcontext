package config

import (
	"errors"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

// Alerts is where alerts go besides admins' email
// (docs/api/monitoring.md#alerts): a webhook and a heartbeat URL, both
// optional. Both URLs can be secrets (a Slack webhook is its URL), so
// they're never logged, and the summary says only whether each is set.
type Alerts struct {
	// AlertWebhookURL (ALERT_WEBHOOK_URL) gets every alert event as a
	// POST; empty: none.
	AlertWebhookURL string
	// AlertWebhookFormat (ALERT_WEBHOOK_FORMAT) is json (the default) or
	// slack.
	AlertWebhookFormat string
	// AlertWebhookSecret (ALERT_WEBHOOK_SECRET), when set, signs each body
	// (an HMAC-SHA256 in X-Lewcontext-Signature).
	AlertWebhookSecret string
	// AlertHeartbeatURL (ALERT_HEARTBEAT_URL) is pinged each minute after
	// alerts were evaluated; empty: none.
	AlertHeartbeatURL string
	// AlertAllowPrivateTargets (ALERT_ALLOW_PRIVATE_TARGETS) lets either
	// URL reach a loopback, private or link-local address in production
	// (an internal relay). Off, the default, refuses them there, checked
	// on the address dialled, after DNS.
	AlertAllowPrivateTargets bool
}

// Webhook formats.
const (
	AlertWebhookJSON  = "json"
	AlertWebhookSlack = "slack"
)

// minWebhookSecret is ALERT_WEBHOOK_SECRET's shortest length.
const minWebhookSecret = 32

// maxAlertURL bounds either URL's length.
const maxAlertURL = 2048

func (c *Config) loadAlerts() error {
	switch os.Getenv("ALERT_ALLOW_PRIVATE_TARGETS") {
	case "", "false":
	case "true":
		c.AlertAllowPrivateTargets = true
	default:
		return errors.New("ALERT_ALLOW_PRIVATE_TARGETS must be true or false")
	}
	var err error
	if c.AlertWebhookURL, err = c.alertURL("ALERT_WEBHOOK_URL"); err != nil {
		return err
	}
	if c.AlertHeartbeatURL, err = c.alertURL("ALERT_HEARTBEAT_URL"); err != nil {
		return err
	}
	c.AlertWebhookFormat = env("ALERT_WEBHOOK_FORMAT", AlertWebhookJSON)
	if c.AlertWebhookFormat != AlertWebhookJSON && c.AlertWebhookFormat != AlertWebhookSlack {
		return errors.New("ALERT_WEBHOOK_FORMAT must be json or slack")
	}
	c.AlertWebhookSecret = os.Getenv("ALERT_WEBHOOK_SECRET")
	if c.AlertWebhookSecret != "" && c.AlertWebhookURL == "" {
		return errors.New("ALERT_WEBHOOK_SECRET needs ALERT_WEBHOOK_URL")
	}
	if c.AlertWebhookSecret != "" && len(c.AlertWebhookSecret) < minWebhookSecret {
		return errors.New("ALERT_WEBHOOK_SECRET must be at least 32 characters (openssl rand -hex 32)")
	}
	return nil
}

// alertURL reads and checks one of the alerts' URLs: an absolute http(s)
// URL (https in production) with a host, no user or password in it (a
// receiver's credentials go in its path or query, as such services
// issue them) and no fragment. In production a host that is a loopback,
// private or link-local address, or localhost, is refused unless
// ALERT_ALLOW_PRIVATE_TARGETS is true; a name is checked again on each
// request, on the address it resolves to. Messages never repeat the value.
func (c *Config) alertURL(name string) (string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || len(raw) > maxAlertURL || u.Host == "" || u.Hostname() == "" || u.Opaque != "":
		return "", errors.New(name + " must be an absolute https URL")
	case u.Scheme != "https" && (c.Production || u.Scheme != "http"):
		return "", errors.New(name + " must be an https URL (http only outside production)")
	case u.User != nil:
		return "", errors.New(name + " must not hold a user name or password")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return "", errors.New(name + " must not have a fragment (#)")
	case strings.ContainsAny(raw, " \t\r\n"):
		return "", errors.New(name + " must not hold spaces or line breaks")
	}
	if c.Production && !c.AlertAllowPrivateTargets && !publicHost(u.Hostname()) {
		return "", errors.New(name + " points at a loopback, private or link-local address: production refuses that unless ALERT_ALLOW_PRIVATE_TARGETS=true")
	}
	return raw, nil
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
