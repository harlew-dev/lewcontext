package main

import (
	"context"
	"lewcontext/api/internal/alerts"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"log/slog"

	"github.com/uptrace/bun"
)

// guardAlerts is whether the alerts' outbound requests refuse non-public
// addresses: in production, unless ALERT_ALLOW_PRIVATE_TARGETS.
func guardAlerts(c config.Config) bool {
	return c.Production && !c.AlertAllowPrivateTargets
}

// alertSealer opens the webhooks' and heartbeats' sealed URLs and keys.
func alertSealer(c config.Config) *auth.DestinationSealer {
	sealer, err := auth.NewDestinationSealer(c.AppKey)
	if err != nil {
		panic("validated application key unavailable for alert destinations")
	}
	return sealer
}

// listenAlerts registers the alert listeners (docs/api/monitoring.md#alerts):
// email to subscribed admins through mailer (nil with MAIL_DRIVER=off), and
// the webhooks, always: which receivers exist is read from the database for
// each delivery, so one added in the dashboard works with no restart.
func listenAlerts(bus *events.Bus, repository *store.Store, mailer *mail.Mailer, c config.Config) {
	webhook := &alerts.Webhook{Store: repository, Sealer: alertSealer(c), Production: c.Production, AllowPrivate: c.AlertAllowPrivateTargets,
		Client: alerts.NewClient(alerts.WebhookTimeout, guardAlerts(c))}
	alerts.Listen(bus, repository, mailer, webhook)
	slog.Info("alert listeners registered", "email", mailer != nil)
}

// runAlerts starts the alert loop: each minute this server evaluates if it
// holds the alerts' lock, and pings the heartbeats after. The stop it returns
// ends the loop and hands the lock back, before the database closes.
func runAlerts(ctx context.Context, db *bun.DB, repository *store.Store, c config.Config) (stop func()) {
	runner := &alerts.Runner{DB: db, Evaluator: &alerts.Evaluator{Store: repository, Limits: httpapi.RateLimits, Rules: monitor.AlertRules, MetricsStoreOff: c.MetricsStore == config.MetricsStoreOff}}
	runner.Heartbeat = &alerts.Heartbeat{Store: repository, Sealer: alertSealer(c), Production: c.Production, AllowPrivate: c.AlertAllowPrivateTargets,
		Client: alerts.NewClient(alerts.HeartbeatTimeout, guardAlerts(c))}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx, alerts.Interval)
	}()
	return func() {
		cancel()
		<-done
	}
}
