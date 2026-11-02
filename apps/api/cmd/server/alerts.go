package main

import (
	"context"
	"lewcontext/api/internal/alerts"
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

// listenAlerts registers the alert listeners (docs/api/monitoring.md#alerts):
// email to admins through mailer (nil with MAIL_DRIVER=off), and the
// webhook when ALERT_WEBHOOK_URL is set. Logs where alerts go, never a URL.
func listenAlerts(bus *events.Bus, repository *store.Store, mailer *mail.Mailer, c config.Config) {
	var webhook *alerts.Webhook
	if c.AlertWebhookURL != "" {
		webhook = &alerts.Webhook{URL: c.AlertWebhookURL, Format: c.AlertWebhookFormat, Client: alerts.NewClient(alerts.WebhookTimeout, guardAlerts(c))}
		if c.AlertWebhookSecret != "" {
			webhook.Secret = []byte(c.AlertWebhookSecret)
		}
	}
	alerts.Listen(bus, repository, mailer, webhook)
	slog.Info("alerts go to", "adminEmail", mailer != nil, "webhook", webhook != nil, "webhookFormat", c.AlertWebhookFormat,
		"webhookSigned", c.AlertWebhookSecret != "", "heartbeat", c.AlertHeartbeatURL != "")
}

// runAlerts starts the alert loop: each minute this server evaluates if it
// holds the alerts' lock, and pings ALERT_HEARTBEAT_URL after. The stop
// it returns ends the loop and hands the lock back, before the database
// closes.
func runAlerts(ctx context.Context, db *bun.DB, repository *store.Store, c config.Config) (stop func()) {
	runner := &alerts.Runner{DB: db, Evaluator: &alerts.Evaluator{Store: repository, Limits: httpapi.RateLimits, Rules: monitor.AlertRules}}
	if c.AlertHeartbeatURL != "" {
		runner.Heartbeat = &alerts.Heartbeat{URL: c.AlertHeartbeatURL, Client: alerts.NewClient(alerts.HeartbeatTimeout, guardAlerts(c))}
	}
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
