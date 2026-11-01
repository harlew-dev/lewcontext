package config

// Summary is what a server says about its configuration in
// server_instances.config (docs/api/observability.md#servers-and-versions),
// for the admin dashboard to compare servers and spot a change. It names
// which settings are set and their non-secret values; it never holds a
// secret's value, part of one, or a digest of one (APP_KEY, the admin keys,
// METRICS_TOKEN, the SMTP credentials, DATABASE_URL, CAPTCHA_SECRET_KEY). Fields are copied one
// by one, never by walking Config, so a new secret can't slip in.
type Summary struct {
	Production        bool         `json:"production"`
	MailDriver        string       `json:"mailDriver"`
	SMTP              *SMTPSummary `json:"smtp,omitempty"`
	EmailVerification string       `json:"emailVerification"`
	RateLimits        string       `json:"rateLimits"`
	ClientIPHeader    string       `json:"clientIpHeader"`
	TrustedProxies    int          `json:"trustedProxies"`
	PerIPLimits       string       `json:"perIpLimits"`
	// The CAPTCHA provider (CAPTCHA_PROVIDER) and its site key, which is
	// public (every 428 sends it to the browser), so its value is kept to
	// compare servers; the secret key only as set or not, and whether
	// either is one of Cloudflare's published test keys.
	CaptchaProvider   string `json:"captchaProvider"`
	CaptchaSiteKey    string `json:"captchaSiteKey"`
	CaptchaSecretKey  bool   `json:"captchaSecretKey"`
	CaptchaTestKeys   bool   `json:"captchaTestKeys"`
	Policies          string `json:"policies"`
	AdminRouteKey     bool   `json:"adminRouteKey"`
	AdminRouteKeyPrev bool   `json:"adminRouteKeyPrevious"`
	AdminAllowedIPs   int    `json:"adminAllowedIps"`
	LogFormat         string `json:"logFormat"`
	LogLevel          string `json:"logLevel"`
	AccessLog         string `json:"accessLog"`
	MetricsStore      string `json:"metricsStore"`
	MetricsAddr       bool   `json:"metricsAddr"`
	MetricsToken      bool   `json:"metricsToken"`
	SlowQueryMs       int64  `json:"slowQueryMs"`
	ShutdownDelayMs   int64  `json:"shutdownDelayMs"`
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
		Production:        c.Production,
		MailDriver:        c.MailDriver,
		EmailVerification: c.VerificationMode(),
		RateLimits:        c.RateLimits,
		ClientIPHeader:    c.ClientIPHeader,
		TrustedProxies:    len(c.TrustedProxies),
		PerIPLimits:       onOff(!c.PerIPLimitsOff),
		CaptchaProvider:   c.CaptchaProvider,
		CaptchaSiteKey:    c.CaptchaSiteKey,
		CaptchaSecretKey:  c.CaptchaSecretKey != "",
		CaptchaTestKeys:   IsTurnstileTestKey(c.CaptchaSiteKey) || IsTurnstileTestKey(c.CaptchaSecretKey),
		Policies:          onOff(c.Policies),
		AdminRouteKey:     c.AdminRouteKey != "",
		AdminRouteKeyPrev: c.AdminRouteKeyPrevious != "",
		AdminAllowedIPs:   len(c.AdminAllowedIPs),
		LogFormat:         c.LogFormat,
		LogLevel:          c.LogLevel.String(),
		AccessLog:         c.AccessLog,
		MetricsStore:      c.MetricsStore,
		MetricsAddr:       c.MetricsAddr != "",
		MetricsToken:      c.MetricsToken != "",
		SlowQueryMs:       c.SlowQuery.Milliseconds(),
		ShutdownDelayMs:   c.ShutdownDelay.Milliseconds(),
		Migrations:        migrations,
	}
	if c.MailDriver == MailSMTP {
		s.SMTP = &SMTPSummary{Host: c.SMTP.Host, Port: c.SMTP.Port, TLS: c.SMTP.TLS}
	}
	return s
}

// onOff is a switch as its variable says it: on or off.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
