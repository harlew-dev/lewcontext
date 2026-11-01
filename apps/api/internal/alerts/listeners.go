package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"log/slog"
	"strconv"
	"time"
)

// The listeners on the alert events (registered in cmd/server): mail.alert
// emails admins, alert.webhook posts to ALERT_WEBHOOK_URL. Each event says
// whether to (decided when it was recorded: Mail, Webhook); a listener
// also sends nothing for an alert silenced since, or pruned, and nothing
// but the resolution for one resolved since. Alerts reach admins as
// operational mail they took on with the role, not a workspace
// notification: each admin's alert_email (all, critical by default, none)
// is the opt-in.

// change is an alert event as the listeners need it.
type change struct {
	event, alert, signal, level string
	value                       float64
	mail, webhook               bool
}

// Listen registers the listeners: email through mailer when it's set
// (MAIL_DRIVER isn't off), and the webhook when it's set.
func Listen(bus *events.Bus, s *store.Store, mailer *mail.Mailer, webhook *Webhook) {
	if mailer != nil {
		events.Listen(bus, "mail.alert", func(ctx context.Context, e events.AlertOpened, d events.Delivered) error {
			return emailChange(ctx, s, mailer, change{"alert.opened", e.Alert, e.Signal, e.Level, e.Value, e.Mail, e.Webhook})
		})
		events.Listen(bus, "mail.alert_escalated", func(ctx context.Context, e events.AlertEscalated, d events.Delivered) error {
			return emailChange(ctx, s, mailer, change{"alert.escalated", e.Alert, e.Signal, e.To, e.Value, e.Mail, e.Webhook})
		})
		events.Listen(bus, "mail.alert_reminded", func(ctx context.Context, e events.AlertReminded, d events.Delivered) error {
			return emailChange(ctx, s, mailer, change{"alert.reminded", e.Alert, e.Signal, e.Level, e.Value, true, true})
		})
		events.Listen(bus, "mail.alert_resolved", func(ctx context.Context, e events.AlertResolved, d events.Delivered) error {
			return emailChange(ctx, s, mailer, change{"alert.resolved", e.Alert, e.Signal, e.Level, 0, e.Mail, e.Webhook})
		})
		events.Listen(bus, "mail.alert_digest", func(ctx context.Context, e events.AlertDigested, d events.Delivered) error {
			return emailDigest(ctx, s, mailer, e)
		})
	}
	if webhook != nil {
		events.Listen(bus, "alert.webhook", func(ctx context.Context, e events.AlertOpened, d events.Delivered) error {
			return postChange(ctx, s, webhook, change{"alert.opened", e.Alert, e.Signal, e.Level, e.Value, e.Mail, e.Webhook}, d.EventID)
		})
		events.Listen(bus, "alert.webhook_escalated", func(ctx context.Context, e events.AlertEscalated, d events.Delivered) error {
			return postChange(ctx, s, webhook, change{"alert.escalated", e.Alert, e.Signal, e.To, e.Value, e.Mail, e.Webhook}, d.EventID)
		})
		events.Listen(bus, "alert.webhook_reminded", func(ctx context.Context, e events.AlertReminded, d events.Delivered) error {
			return postChange(ctx, s, webhook, change{"alert.reminded", e.Alert, e.Signal, e.Level, e.Value, true, true}, d.EventID)
		})
		events.Listen(bus, "alert.webhook_resolved", func(ctx context.Context, e events.AlertResolved, d events.Delivered) error {
			return postChange(ctx, s, webhook, change{"alert.resolved", e.Alert, e.Signal, e.Level, 0, e.Mail, e.Webhook}, d.EventID)
		})
	}
}

// current finds the alert a change is about, and whether to tell of it
// now: not when it's gone (pruned), silenced since, or resolved since
// (unless this is its resolution, which says so).
func current(ctx context.Context, s *store.Store, c change) (store.Alert, bool, error) {
	alert, err := s.FindAlert(ctx, c.alert)
	if errors.Is(err, sql.ErrNoRows) {
		return alert, false, nil
	}
	if err != nil {
		return alert, false, err
	}
	if alert.Silenced(time.Now()) {
		return alert, false, nil
	}
	if alert.ResolvedAt != nil && c.event != "alert.resolved" {
		return alert, false, nil
	}
	return alert, true, nil
}

// emailChange emails the admins whose setting covers the change's level.
func emailChange(ctx context.Context, s *store.Store, mailer *mail.Mailer, c change) error {
	if !c.mail {
		return nil
	}
	alert, tell, err := current(ctx, s, c)
	if err != nil || !tell {
		return err
	}
	about := AboutSignal(c.signal)
	data := mail.AlertData{
		Kind:    kind(c.event),
		Level:   LevelWord(c.level),
		Title:   about.Title,
		Value:   FormatValue(c.value, about.Unit),
		Opened:  FormatTime(alert.OpenedAt),
		Meaning: about.Meaning,
	}
	if c.event == "alert.reminded" || c.event == "alert.escalated" {
		data.Value = FormatValue(alert.LastValue, about.Unit)
	}
	if alert.ResolvedAt != nil {
		data.OpenFor = FormatDuration(alert.ResolvedAt.Sub(alert.OpenedAt))
	}
	return emailAdmins(ctx, s, mailer, mail.Alert, data, func(setting string) bool { return covers(setting, c.level) })
}

// kind is the email's kind for an event name: "opened", "escalated", …
func kind(event string) string {
	return map[string]string{
		"alert.opened": "opened", "alert.escalated": "escalated", "alert.reminded": "reminded", "alert.resolved": "resolved",
	}[event]
}

// covers reports whether an admin's setting takes alerts at level.
func covers(setting, level string) bool {
	return setting == store.AlertEmailAll || (setting == store.AlertEmailCritical && level == events.AlertCritical)
}

// emailDigest emails each admin the digest's alerts their setting covers;
// nothing to one it covers none of.
func emailDigest(ctx context.Context, s *store.Store, mailer *mail.Mailer, e events.AlertDigested) error {
	recipients, err := s.IndexAlertRecipients(ctx)
	if err != nil {
		return err
	}
	var failed []error
	sent := 0
	for _, r := range recipients {
		data := mail.AlertDigestData{}
		for _, a := range e.Alerts {
			if !covers(r.AlertEmail, a.Level) {
				continue
			}
			alert, err := s.FindAlert(ctx, a.Alert)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && alert.Silenced(time.Now())) {
				continue
			}
			if err != nil {
				return err
			}
			about := AboutSignal(a.Signal)
			data.Alerts = append(data.Alerts, mail.AlertDigestItem{Level: LevelWord(a.Level), Title: about.Title,
				Value: FormatValue(a.Value, about.Unit), Opened: FormatTime(alert.OpenedAt)})
		}
		if len(data.Alerts) == 0 {
			continue
		}
		data.Count = strconv.Itoa(len(data.Alerts))
		if err := mailer.Send(ctx, r.Email, mail.AlertDigest, data); err != nil && !errors.Is(err, mail.ErrBlocked) {
			failed = append(failed, err)
			continue
		}
		sent++
	}
	return partly(ctx, "mail.alert_digest", sent, failed)
}

// emailAdmins sends t with data to every admin whose setting wants it.
func emailAdmins(ctx context.Context, s *store.Store, mailer *mail.Mailer, t mail.Template, data any, wants func(setting string) bool) error {
	recipients, err := s.IndexAlertRecipients(ctx)
	if err != nil {
		return err
	}
	var failed []error
	sent := 0
	for _, r := range recipients {
		if !wants(r.AlertEmail) {
			continue
		}
		if err := mailer.Send(ctx, r.Email, t, data); err != nil && !errors.Is(err, mail.ErrBlocked) {
			failed = append(failed, err)
			continue
		}
		sent++
	}
	return partly(ctx, "mail.alert", sent, failed)
}

// partly is the outcome of emailing several admins. When every send
// failed (the SMTP server down, usually) it's an error, so the delivery
// is retried. When some went, retrying would email those admins again,
// so it's done: the failures are logged and reported as an error group,
// and the admins missed still have the dashboard (and the reminder, for
// a critical alert).
func partly(ctx context.Context, listener string, sent int, failed []error) error {
	if len(failed) == 0 {
		return nil
	}
	err := fmt.Errorf("%d of %d alert emails failed: %w", len(failed), sent+len(failed), errors.Join(failed...))
	if sent == 0 {
		return err
	}
	slog.ErrorContext(ctx, "some alert emails failed", observe.ErrorAttr(err), "listener", listener)
	observe.Report(ctx, observe.Problem{Source: observe.SourceWorker, Route: listener, Err: err})
	return nil
}

// postChange posts the change to the webhook.
func postChange(ctx context.Context, s *store.Store, w *Webhook, c change, delivery string) error {
	if !c.webhook {
		return nil
	}
	alert, tell, err := current(ctx, s, c)
	if err != nil || !tell {
		return err
	}
	about := AboutSignal(c.signal)
	n := Notice{
		Alert: alert.PublicID, Event: c.event, State: "open", Signal: c.signal, Title: about.Title,
		Level: c.level, Value: alert.LastValue, Unit: about.Unit, OpenedAt: alert.OpenedAt, ResolvedAt: alert.ResolvedAt,
		Meaning: about.Meaning,
	}
	if c.event == "alert.opened" {
		n.Value = c.value
	}
	if alert.ResolvedAt != nil {
		n.State = "resolved"
	}
	return w.Post(ctx, n, delivery)
}
