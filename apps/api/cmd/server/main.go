package main

import (
	"context"
	"errors"
	"io/fs"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"lewcontext/api/internal/web"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// The log's format and level come first, so even a refused config is
	// logged as the operator asked (docs/api/observability.md).
	logging, err := config.LoadLogging()
	if err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(observe.NewLogger(os.Stdout, logging.LogFormat, logging.LogLevel))
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
	// Every query is timed, and a slow one logged without its values
	// (SLOW_QUERY_MS); the pool's gauges are read at each collection.
	db.AddQueryHook(database.QueryHook{Slow: c.SlowQuery})
	observe.WatchDB(db.DB)
	bus := events.NewBus()
	repository := &store.Store{DB: db, Listeners: bus}
	// Grouped errors (app_errors): every problem reported from here on is
	// grouped in memory and stored every 10 seconds, only when there are any,
	// through the shutdown's drain too, until flushErrors stops it last.
	collector := observe.NewCollector(repository.RecordErrors, observe.Version())
	observe.SetReporter(collector)
	collecting, stopCollecting := context.WithCancel(context.Background())
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		collector.Run(collecting, observe.ErrorFlushInterval)
	}()
	defer flushErrors(collector, stopCollecting, collected)
	// Listeners register on the bus here, before anything records an event.
	if err := listen(bus, repository, c); err != nil {
		return err
	}
	// This server's row and server.started; then each minute's metrics
	// with its heartbeat, and the hourly rollup. On the way out
	// server.stopped and the last minute go first, the error groups last
	// (deferred in that order), within finalBudget.
	instance, err := newServerInstance(startup, db, c)
	if err != nil {
		return err
	}
	// A crash loop's rows go even if no server lives long enough to roll up.
	if _, err := deleteSilentServers(startup, repository); err != nil {
		slog.Warn("deleting silent servers failed", observe.ErrorAttr(err))
	}
	if err := repository.RecordServerStart(startup, instance); err != nil {
		slog.Warn("recording server.started failed; the heartbeat will add the server", observe.ErrorAttr(err))
	}
	slog.Info("server started", "server", instance.ID, "version", instance.Version, "metricsStore", c.MetricsStore)
	minutes := minuteFlush(repository, instance, c)
	stopMinutes := runMinutes(minutes)
	// Failed, unless the server stops because it was asked to.
	stopReason := events.ServerFailed
	defer func() {
		stopMinutes()
		stopServer(repository, instance, stopReason, minutes)
	}()
	// The rollup stops, and is waited for, before the last minute is written.
	stopRollup := rollUp(ctx, repository)
	defer stopRollup()
	metricsServer, err := serveMetrics(c)
	if err != nil {
		return err
	}
	// If startup fails from here on, the /metrics listener closes too (after
	// a graceful shutdown it already has, and this does nothing).
	if metricsServer != nil {
		defer func() { _ = metricsServer.Close() }()
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
	if err := servePages(api, c); err != nil {
		return err
	}
	server := &http.Server{Addr: c.Address, Handler: router.New(api), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		// net/http's own complaints (a broken connection, a bad request
		// line) go through the same handler, as warnings.
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn)}
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
				slog.Error("session cleanup failed", observe.ErrorAttr(err))
				observe.Report(ctx, observe.Problem{Source: observe.SourceBackground, Route: "cleanup", Err: err})
			}
		case <-ctx.Done():
			stopReason = events.ServerSignal
			// Readiness answers 503 from here on, while the server keeps
			// answering for SHUTDOWN_DELAY, so a load balancer polling it
			// takes it out before it stops taking connections.
			api.Drain()
			if c.ShutdownDelay > 0 {
				slog.Info("shutting down: readiness answers 503; waiting before the drain", "delay", c.ShutdownDelay.String())
				time.Sleep(c.ShutdownDelay)
			}
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := server.Shutdown(shutdown)
			// /metrics answers through the drain, and stops with it.
			if metricsServer != nil {
				_ = metricsServer.Shutdown(shutdown)
			}
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
			if metricsServer != nil {
				_ = metricsServer.Close()
			}
			return err
		}
	}
}

// flushErrors stops reporting and stores the groups collected since the
// last flush, once the requests, the worker, server.stopped and the last
// minute's metrics are done: the last failures before a shutdown are the
// ones worth keeping. It has what's left of finalBudget (2 s at least).
func flushErrors(collector *observe.Collector, stop context.CancelFunc, stopped <-chan struct{}) {
	stop()
	<-stopped
	observe.SetReporter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), finalBudget-stopEventBudget-lastMinuteBudget)
	defer cancel()
	if err := collector.Flush(ctx); err != nil {
		slog.Warn("storing the last error groups failed", observe.ErrorAttr(err), "groups", collector.Len())
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
// without its admin build, with admin on, never starts (ErrAdminNotBuilt).
func servePages(api *httpapi.Server, c config.Config) error {
	var admin fs.FS
	if c.AdminRouteKey != "" {
		admin = web.AdminFiles()
	}
	pages, err := web.New(web.Files(), admin, c.Production)
	if errors.Is(err, web.ErrNotBuilt) && !c.Production {
		slog.Info("serving the API only; the web app isn't built into this binary (make serve)")
		return nil
	}
	if err != nil {
		return err
	}
	api.Pages = pages
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
