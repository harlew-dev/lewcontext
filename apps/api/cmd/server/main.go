package main

import (
	"context"
	"errors"
	"io/fs"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/captcha"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"lewcontext/api/internal/web"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := database.Open(startup, c.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	bus := events.NewBus()
	repository := &store.Store{DB: db, Listeners: bus}
	// Listeners register on the bus here, before anything records an event.
	if err := listen(bus, repository, c); err != nil {
		return err
	}
	api, err := httpapi.New(repository, c)
	if err != nil {
		return err
	}
	if c.RateLimits == config.RateLimitsOff {
		slog.Warn("rate limits are off (RATE_LIMITS=off): for tests only")
	}
	if c.ClientIPHeader != "" {
		slog.Info("client addresses come from a proxy header", "header", c.ClientIPHeader, "trustedProxies", len(c.TrustedProxies))
	}
	logAdminAccess(c)
	if err := useCaptcha(api, c); err != nil {
		return err
	}
	if err := servePages(api, c); err != nil {
		return err
	}
	server := &http.Server{Addr: c.Address, Handler: router.New(api), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	failures := make(chan error, 1)
	go func() {
		slog.Info("listening", "address", c.Address, "pages", api.Pages != nil)
		failures <- server.ListenAndServe()
	}()
	// Delivers recorded events to their listeners until shutdown.
	worker := &events.Worker{Bus: bus, Queue: repository}
	delivering := make(chan struct{})
	go func() {
		defer close(delivering)
		worker.Run(ctx)
	}()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cleanup, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := repository.Cleanup(cleanup)
			cancel()
			if err != nil {
				slog.Error("session cleanup failed", "error", err)
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := server.Shutdown(shutdown)
			// Record the refused admin addresses still queued.
			api.StopAdminRefusals(shutdown)
			// Let the worker finish its batch; anything unfinished is
			// retried when its lease ends.
			select {
			case <-delivering:
			case <-shutdown.Done():
			}
			return err
		case err := <-failures:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	}
}

// listen registers every listener. Without email delivery (MAIL_DRIVER=off)
// nothing sends reset links or invitations, and the API refuses to take
// requests for either; email verification can't be on without it (config).
func listen(bus *events.Bus, repository *store.Store, c config.Config) error {
	if c.MailDriver == config.MailOff {
		slog.Info("mail is off (MAIL_DRIVER=off): password reset and invitations are unavailable")
		return nil
	}
	driver, err := mailDriver(c)
	if err != nil {
		return err
	}
	mailer, err := mail.New(mail.Options{Driver: driver, App: c.AppName, Origin: c.Origin, Blocks: repository})
	if err != nil {
		return err
	}
	events.Listen(bus, "mail.password_reset", mail.PasswordReset(repository, mailer, c.Origin))
	events.Listen(bus, "mail.workspace_invitation", mail.SendInvitation(repository, mailer, c.Origin))
	if c.Verify {
		keys, err := auth.NewKeys(c.AppKey)
		if err != nil {
			return err
		}
		events.Listen(bus, "mail.registration", mail.Registration(repository, mailer, keys, c.Origin, c.RateLimits == config.RateLimitsOn))
		events.Listen(bus, "mail.email_verification", mail.EmailVerification(repository, mailer, keys))
	}
	return nil
}

// mailDriver builds MAIL_DRIVER's driver and logs where mail goes: never the
// SMTP credentials.
func mailDriver(c config.Config) (mail.Driver, error) {
	if c.MailDriver == config.MailLog {
		slog.Info("mail goes to the development mailbox", "file", c.MailFile)
		return &mail.FileDriver{Path: c.MailFile}, nil
	}
	slog.Info("mail goes out over SMTP", "host", c.SMTP.Host, "port", c.SMTP.Port, "tls", c.SMTP.TLS, "from", c.MailFromAddress)
	return mail.NewSMTPDriver(mail.SMTPConfig{
		Host: c.SMTP.Host, Port: c.SMTP.Port, TLS: c.SMTP.TLS,
		Username: c.SMTP.Username, Password: c.SMTP.Password,
		FromAddress: c.MailFromAddress, FromName: c.MailFromName,
	})
}

// servePages adds the embedded web build, and with ADMIN_ROUTE_KEY set the
// admin build beside it. Production requires the web build; in development
// the API can run alone while Vite serves the pages (make web). A web build
// without its admin build, with admin on, never starts (ErrAdminNotBuilt),
// nor POLICIES=on without the policies in the build, or in production with
// a document still the template (web.Pages.ServePolicies).
func servePages(api *httpapi.Server, c config.Config) error {
	var admin fs.FS
	if c.AdminRouteKey != "" {
		admin = web.AdminFiles()
	}
	pages, err := web.New(web.Files(), admin, c.Production)
	if errors.Is(err, web.ErrNotBuilt) && !c.Production && !c.Policies {
		slog.Info("serving the API only; the web app isn't built into this binary (make serve)")
		return nil
	}
	if errors.Is(err, web.ErrNotBuilt) && c.Policies {
		return web.ErrPoliciesNotBuilt
	}
	if err != nil {
		return err
	}
	api.Pages = pages
	// The CAPTCHA provider's script and frame, only while there is one.
	if api.Captcha != nil {
		sources := api.Captcha.CSP()
		pages.AllowThirdParty(web.ThirdParty{Script: sources.Script, Frame: sources.Frame})
	}
	if c.Policies {
		built, err := pages.ServePolicies()
		if err != nil {
			return err
		}
		api.Policies = store.PolicyVersions{TermsOfService: built.TermsOfService.Version, PrivacyPolicy: built.PrivacyPolicy.Version}
		slog.Info("the terms of service and privacy policy are published", "termsOfService", api.Policies.TermsOfService, "privacyPolicy", api.Policies.PrivacyPolicy)
	}
	return nil
}

// useCaptcha sets up the CAPTCHA provider rate limits' challenge levels ask
// for (CAPTCHA_PROVIDER), and says what's off. It logs the provider's name,
// never its keys.
func useCaptcha(api *httpapi.Server, c config.Config) error {
	origin, err := url.Parse(c.Origin)
	if err != nil {
		return err
	}
	provider, err := captcha.New(c.Captcha, &http.Client{}, origin.Hostname())
	if err != nil {
		return err
	}
	api.Captcha = provider
	if provider == nil {
		if c.Production {
			slog.Warn("CAPTCHA_PROVIDER=off: rate limits' challenge levels are skipped, only their block levels apply")
		}
	} else {
		slog.Info("rate limits' challenge levels ask for a CAPTCHA", "provider", provider.Name())
		// Cloudflare's test secrets answer every check alike: the default
		// (1x…) passes, the others fail.
		switch {
		case c.Production || !config.IsTurnstileTestKey(c.CaptchaSecretKey):
		case c.CaptchaSecretKey == config.TurnstileTestSecretKey:
			slog.Info("captcha: test keys, every check passes")
		default:
			slog.Info("captcha: test keys, every check fails")
		}
	}
	if c.PerIPLimitsOff {
		slog.Warn("PER_IP_LIMITS=off: no limit counts client IPs; set CLIENT_IP_HEADER and TRUSTED_PROXIES instead if the proxy can send them")
	}
	return nil
}

// logAdminAccess says how the admin dashboard is reached, never a key's
// value: a previous key still accepted, and the address allowlist.
func logAdminAccess(c config.Config) {
	if c.AdminRouteKeyPrevious != "" {
		slog.Info("ADMIN_ROUTE_KEY_PREVIOUS is set: admin sessions made under it keep working until they end (8 hours at most); remove it then")
	}
	if len(c.AdminAllowedIPs) == 0 {
		return
	}
	slog.Info("the admin dashboard answers only ADMIN_ALLOWED_IPS", "ranges", len(c.AdminAllowedIPs))
	// Behind a proxy, every request comes from the proxy unless the real
	// client address is read from its header: an allowlist naming the proxy
	// would let everyone in. A server facing the internet directly is valid
	// without it, so this warns rather than refusing. Plan A4's
	// Configuration page shows the same warning.
	if c.Production && c.ClientIPHeader == "" {
		slog.Warn("ADMIN_ALLOWED_IPS is set without CLIENT_IP_HEADER: behind a proxy every request comes from the proxy's address, so an allowlist naming the proxy lets everyone in; set CLIENT_IP_HEADER and TRUSTED_PROXIES (docs/deployment.md)")
	}
}
