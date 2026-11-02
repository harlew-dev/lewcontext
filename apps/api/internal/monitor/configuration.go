package monitor

import (
	"context"
	"fmt"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/store"
	"time"
)

// The Configuration page (docs/api/monitoring.md#configuration): which
// servers and versions are live, and is anything set unsafely? From each
// server's configuration summary (server_instances.config, config.Summary:
// which settings are set and their non-secret values, never a secret's
// value or digest). The page shows only the settings listed here, read
// field by field from config.Summary, so nothing else a row might hold
// (an older or newer server's summary) is ever shown.

// PageConfiguration is the Configuration page's name.
const PageConfiguration = "configuration"

// ConfigurationSignals are the Configuration page's signals.
var ConfigurationSignals = []Signal{
	{
		Name: "servers.live", Page: PageConfiguration, Label: "Live servers", Unit: UnitCount,
		// Heard from in the last 2 minutes. A dashboard can't say there
		// are none: the external uptime check does.
		Window: store.ServerLiveFor,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			live, err := env.Store.FindLiveServers(ctx)
			return Reading{Value: float64(live.Count), Sample: live.Count}, err
		},
	},
	{
		// More than one version live: a release in progress, or stopped
		// halfway.
		Name: "servers.versions", Page: PageConfiguration, Label: "Versions live", Unit: UnitCount,
		Window: store.ServerLiveFor, Warn: 2,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			live, err := env.Store.FindLiveServers(ctx)
			return Reading{Value: float64(len(live.Versions)), Sample: live.Count}, err
		},
	},
}

// ServerInfo is a server as the Configuration page lists it.
type ServerInfo struct {
	ID         string    `json:"id"`
	Hostname   string    `json:"hostname"`
	Version    string    `json:"version"`
	StartedAt  time.Time `json:"startedAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Live       bool      `json:"live"`
}

// Setting is one setting across the servers compared (Servers' order):
// its variable's name and each server's value in words. Differs: not every
// server has the same.
type Setting struct {
	Name    string   `json:"name"`
	Values  []string `json:"values"`
	Differs bool     `json:"differs"`
}

// Warning is something set unsafely, by its ID (the page says what it
// means and what to do), with how many servers or sessions it's about.
type Warning struct {
	ID    string  `json:"id"`
	Count float64 `json:"count,omitempty"`
}

// The warnings, by ID.
const (
	WarnMailOff         = "mail_off"
	WarnAllowlistNoIP   = "allowlist_without_client_ip"
	WarnPreviousKey     = "previous_key"
	WarnMigrations      = "migrations_pending"
	WarnVersions        = "versions"
	WarnNoAdminMFA      = "no_admin_with_mfa"
	WarnSharedAddress   = "shared_address"
	WarnMetricsTokenOff = "metrics_without_token"
	WarnNotProduction   = "not_production"
	WarnPruneNotRunning = "prune_not_running"
	WarnCaptchaOff      = "captcha_off"
)

// configurationSections are the Configuration page's tables.
type configurationSections struct {
	Servers []ServerInfo `json:"servers"`
	// Compared are the servers whose settings are listed: the live ones,
	// or the last heard from when none is.
	Compared []string  `json:"compared"`
	Settings []Setting `json:"settings"`
	Warnings []Warning `json:"warnings"`
}

// configurationPage reads the servers and their settings, then the
// warnings.
func configurationPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &configurationSections{Servers: []ServerInfo{}, Compared: []string{}, Settings: []Setting{}, Warnings: []Warning{}}
	var compared []store.ServerInstance
	return readSections(ctx, env, out, []section{
		{"servers", func(ctx context.Context) error {
			servers, err := s.IndexServerInstances(ctx)
			if err != nil {
				return err
			}
			live := liveServers(servers, env.Now)
			for _, si := range servers {
				out.Servers = append(out.Servers, ServerInfo{ID: si.ID, Hostname: si.Hostname, Version: si.Version,
					StartedAt: si.StartedAt, LastSeenAt: si.LastSeenAt, Live: si.LastSeenAt.After(store.ServerLiveSince(env.Now))})
			}
			compared = live
			if len(compared) == 0 && len(servers) > 0 {
				compared = servers[:1]
			}
			for _, si := range compared {
				out.Compared = append(out.Compared, si.ID)
			}
			out.Settings = settingsOf(compared)
			out.Warnings = append(out.Warnings, serverWarnings(compared, live)...)
			return nil
		}},
		{"warnings", func(ctx context.Context) error {
			if !anyServer(compared, func(c config.Summary) bool { return c.AdminRouteKeyPrev }) && env.AdminKeys.Previous == "" {
				return nil
			}
			n, err := s.CountAdminSessionsUnder(ctx, env.AdminKeys.Previous)
			if err != nil {
				return err
			}
			out.Warnings = append(out.Warnings, Warning{ID: WarnPreviousKey, Count: float64(n)})
			return nil
		}},
		{"warnings", func(ctx context.Context) error {
			n, err := s.CountAdminsWithMFA(ctx)
			if err == nil && n == 0 {
				out.Warnings = append(out.Warnings, Warning{ID: WarnNoAdminMFA})
			}
			return err
		}},
		{"warnings", func(ctx context.Context) error {
			// data.prune_not_running's measure: the Data page lists which.
			targets, err := readRetention(ctx, env)
			if n := countTargets(targets, func(t RetentionTarget) bool { return t.NotRunning }); err == nil && n > 0 {
				out.Warnings = append(out.Warnings, Warning{ID: WarnPruneNotRunning, Count: float64(n)})
			}
			return err
		}},
		{"warnings", func(ctx context.Context) error {
			// ips.busiest_share's measure: the proxy's address everywhere.
			b, err := s.FindBusiestAddress(ctx, env.Now.Add(-24*time.Hour), env.Now)
			r := share(b.Busiest, b.Events)
			if err == nil && r.Sample >= busiestSample && r.Value >= busiestShare {
				out.Warnings = append(out.Warnings, Warning{ID: WarnSharedAddress, Count: r.Value})
			}
			return err
		}},
	})
}

// anyServer reports whether any of servers' summaries has is.
func anyServer(servers []store.ServerInstance, is func(config.Summary) bool) bool {
	for _, si := range servers {
		if is(configOf(si)) {
			return true
		}
	}
	return false
}

// countServers counts servers whose summaries have is.
func countServers(servers []store.ServerInstance, is func(config.Summary) bool) float64 {
	n := 0
	for _, si := range servers {
		if is(configOf(si)) {
			n++
		}
	}
	return float64(n)
}

// serverWarnings are the warnings the summaries alone show, each with how
// many of the servers compared it's about.
func serverWarnings(compared, live []store.ServerInstance) []Warning {
	var out []Warning
	for _, w := range []struct {
		id string
		is func(config.Summary) bool
	}{
		{WarnNotProduction, func(c config.Summary) bool { return !c.Production }},
		{WarnMailOff, func(c config.Summary) bool { return c.MailDriver == config.MailOff }},
		{WarnAllowlistNoIP, func(c config.Summary) bool { return c.AdminAllowedIPs > 0 && c.ClientIPHeader == "" }},
		{WarnMetricsTokenOff, func(c config.Summary) bool { return c.MetricsAddr && !c.MetricsToken }},
		{WarnCaptchaOff, func(c config.Summary) bool { return c.Production && c.CaptchaProvider == config.CaptchaOff }},
		{WarnMigrations, func(c config.Summary) bool { return c.Migrations.Applied < c.Migrations.Embedded }},
	} {
		if n := countServers(compared, w.is); n > 0 {
			out = append(out, Warning{ID: w.id, Count: n})
		}
	}
	versions := map[string]bool{}
	for _, si := range live {
		versions[si.Version] = true
	}
	if len(versions) > 1 {
		out = append(out, Warning{ID: WarnVersions, Count: float64(len(versions))})
	}
	return out
}

// settings are the settings compared, each a variable's name and its
// value in words from a summary. Read field by field: a field the summary
// gains isn't shown until it's listed here.
var settings = []struct {
	name  string
	value func(config.Summary) string
}{
	{"APP_ENV", func(c config.Summary) string { return yesNo(c.Production, "production", "not production") }},
	{"MAIL_DRIVER", func(c config.Summary) string { return c.MailDriver }},
	{"SMTP", func(c config.Summary) string {
		if c.SMTP == nil {
			return "not used"
		}
		return fmt.Sprintf("%s:%d, %s", c.SMTP.Host, c.SMTP.Port, c.SMTP.TLS)
	}},
	{"EMAIL_VERIFICATION", func(c config.Summary) string { return c.EmailVerification }},
	{"RATE_LIMITS", func(c config.Summary) string { return c.RateLimits }},
	{"PER_IP_LIMITS", func(c config.Summary) string { return c.PerIPLimits }},
	{"CAPTCHA_PROVIDER", func(c config.Summary) string { return c.CaptchaProvider }},
	{"CAPTCHA_SITE_KEY", func(c config.Summary) string { return yesNo(c.CaptchaSiteKey, "set", "not set") }},
	{"CAPTCHA_SECRET_KEY", func(c config.Summary) string { return yesNo(c.CaptchaSecretKey, "set", "not set") }},
	{"CAPTCHA test keys", func(c config.Summary) string { return yesNo(c.CaptchaTestKeys, "in use", "not used") }},
	{"POLICIES", func(c config.Summary) string { return c.Policies }},
	{"CLIENT_IP_HEADER", func(c config.Summary) string { return orNotSet(c.ClientIPHeader) }},
	{"TRUSTED_PROXIES", func(c config.Summary) string { return ranges(c.TrustedProxies) }},
	{"ADMIN_ALLOWED_IPS", func(c config.Summary) string { return ranges(c.AdminAllowedIPs) }},
	{"ADMIN_ROUTE_KEY", func(c config.Summary) string { return yesNo(c.AdminRouteKey, "set", "not set") }},
	{"ADMIN_ROUTE_KEY_PREVIOUS", func(c config.Summary) string { return yesNo(c.AdminRouteKeyPrev, "set", "not set") }},
	{"METRICS_STORE", func(c config.Summary) string { return c.MetricsStore }},
	{"METRICS_ADDR", func(c config.Summary) string { return yesNo(c.MetricsAddr, "set", "not set") }},
	{"METRICS_TOKEN", func(c config.Summary) string { return yesNo(c.MetricsToken, "set", "not set") }},
	{"ALERT_WEBHOOK_URL", func(c config.Summary) string { return yesNo(c.AlertWebhook, "set", "not set") }},
	{"ALERT_WEBHOOK_FORMAT", func(c config.Summary) string { return orNotSet(c.AlertWebhookFormat) }},
	{"ALERT_WEBHOOK_SECRET", func(c config.Summary) string { return yesNo(c.AlertWebhookSigned, "set", "not set") }},
	{"ALERT_HEARTBEAT_URL", func(c config.Summary) string { return yesNo(c.AlertHeartbeat, "set", "not set") }},
	{"ALERT_ALLOW_PRIVATE_TARGETS", func(c config.Summary) string { return yesNo(c.AlertPrivate, "allowed", "refused") }},
	{"LOG_FORMAT", func(c config.Summary) string { return c.LogFormat }},
	{"LOG_LEVEL", func(c config.Summary) string { return c.LogLevel }},
	{"ACCESS_LOG", func(c config.Summary) string { return c.AccessLog }},
	{"SLOW_QUERY_MS", func(c config.Summary) string { return fmt.Sprintf("%d ms", c.SlowQueryMs) }},
	{"SHUTDOWN_DELAY", func(c config.Summary) string { return fmt.Sprintf("%d ms", c.ShutdownDelayMs) }},
	{"Migrations", func(c config.Summary) string {
		return fmt.Sprintf("%d of %d applied", c.Migrations.Applied, c.Migrations.Embedded)
	}},
}

// settingsOf is each setting across servers.
func settingsOf(servers []store.ServerInstance) []Setting {
	summaries := make([]config.Summary, len(servers))
	for i, si := range servers {
		summaries[i] = configOf(si)
	}
	out := make([]Setting, len(settings))
	for i, s := range settings {
		row := Setting{Name: s.name, Values: make([]string, len(summaries))}
		for j, c := range summaries {
			row.Values[j] = s.value(c)
			row.Differs = row.Differs || row.Values[j] != row.Values[0]
		}
		out[i] = row
	}
	return out
}

func yesNo(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func orNotSet(s string) string {
	if s == "" {
		return "not set"
	}
	return s
}

// ranges is a count of address ranges in words.
func ranges(n int) string {
	switch n {
	case 0:
		return "not set"
	case 1:
		return "1 range"
	}
	return fmt.Sprintf("%d ranges", n)
}
