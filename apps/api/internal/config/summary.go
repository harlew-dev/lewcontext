package config

// Summary is what a server says about its configuration in
// server_instances.config (docs/api/observability.md#servers-and-versions),
// for the admin dashboard to compare servers and spot a change. It names
// which settings are set and their non-secret values; it never holds a
// secret's value, part of one, or a digest of one (APP_KEY, the admin keys,
// METRICS_TOKEN, the SMTP credentials, DATABASE_URL). Fields are copied one
// by one, never by walking Config, so a new secret can't slip in.
type Summary struct {
	Production        bool         `json:"production"`
	MailDriver        string       `json:"mailDriver"`
	SMTP              *SMTPSummary `json:"smtp,omitempty"`
	EmailVerification string       `json:"emailVerification"`
	RateLimits        string       `json:"rateLimits"`
	ClientIPHeader    string       `json:"clientIpHeader"`
	TrustedProxies    int          `json:"trustedProxies"`
	AdminRouteKey     bool         `json:"adminRouteKey"`
	AdminRouteKeyPrev bool         `json:"adminRouteKeyPrevious"`
	AdminAllowedIPs   int          `json:"adminAllowedIps"`
	LogFormat         string       `json:"logFormat"`
	LogLevel          string       `json:"logLevel"`
	AccessLog         string       `json:"accessLog"`
	MetricsStore      string       `json:"metricsStore"`
	MetricsAddr       bool         `json:"metricsAddr"`
	MetricsToken      bool         `json:"metricsToken"`
	SlowQueryMs       int64        `json:"slowQueryMs"`
	ShutdownDelayMs   int64        `json:"shutdownDelayMs"`
	// The alerts' webhook and heartbeat: whether each URL is set (never
	// the URL: it can be a secret), the format, whether bodies are signed.
	AlertWebhook       bool   `json:"alertWebhook"`
	AlertWebhookFormat string `json:"alertWebhookFormat"`
	AlertWebhookSigned bool   `json:"alertWebhookSigned"`
	AlertHeartbeat     bool   `json:"alertHeartbeat"`
	AlertPrivate       bool   `json:"alertAllowPrivateTargets"`
	// Migrations are the migrations the database has applied and those
	// built into this binary: a server built ahead of its database shows.
	Migrations MigrationsSummary `json:"migrations"`
}

// SMTPSummary is where mail goes: the host, port and TLS mode, never the
// username or password.
type SMTPSummary struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	TLS  string `json:"tls"`
}

// MigrationsSummary counts the migrations applied and embedded.
type MigrationsSummary struct {
	Applied  int `json:"applied"`
	Embedded int `json:"embedded"`
}

// Summary is c's summary, with the migration counts.
func (c Config) Summary(migrations MigrationsSummary) Summary {
	s := Summary{
		Production:         c.Production,
		MailDriver:         c.MailDriver,
		EmailVerification:  c.VerificationMode(),
		RateLimits:         c.RateLimits,
		ClientIPHeader:     c.ClientIPHeader,
		TrustedProxies:     len(c.TrustedProxies),
		AdminRouteKey:      c.AdminRouteKey != "",
		AdminRouteKeyPrev:  c.AdminRouteKeyPrevious != "",
		AdminAllowedIPs:    len(c.AdminAllowedIPs),
		LogFormat:          c.LogFormat,
		LogLevel:           c.LogLevel.String(),
		AccessLog:          c.AccessLog,
		MetricsStore:       c.MetricsStore,
		MetricsAddr:        c.MetricsAddr != "",
		MetricsToken:       c.MetricsToken != "",
		SlowQueryMs:        c.SlowQuery.Milliseconds(),
		ShutdownDelayMs:    c.ShutdownDelay.Milliseconds(),
		AlertWebhook:       c.AlertWebhookURL != "",
		AlertWebhookFormat: c.AlertWebhookFormat,
		AlertWebhookSigned: c.AlertWebhookSecret != "",
		AlertHeartbeat:     c.AlertHeartbeatURL != "",
		AlertPrivate:       c.AlertAllowPrivateTargets,
		Migrations:         migrations,
	}
	if c.MailDriver == MailSMTP {
		s.SMTP = &SMTPSummary{Host: c.SMTP.Host, Port: c.SMTP.Port, TLS: c.SMTP.TLS}
	}
	return s
}
