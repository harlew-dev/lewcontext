package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"time"
)

// Alerts (docs/api/monitoring.md#alerts): a monitoring signal at its warn
// or critical level for its rule's time. The evaluator (internal/alerts),
// on the one server holding the alerts' lock, opens, escalates, resolves,
// reminds and digests them here; each change and its event are one
// transaction, under the alerts' write lock (database.LockAlertWrites), so
// even two evaluators at once couldn't tell anyone twice. Every time is
// the evaluator's clock (at), so the rules' durations read one clock.

// How alerts are told about.
const (
	// AlertCooldown: an opening whose signal opened another alert within
	// it is quiet: no email or webhook for it, nor for its escalation or
	// resolution (a flapping signal, or someone raising a level on purpose
	// again and again, is told about once an hour at most).
	AlertCooldown = time.Hour
	// MaxSignalMails is the most emails one signal's alerts send within
	// AlertCooldown, of every kind (opening, escalation, resolution,
	// reminder): past it the email is left out (the dashboard and the
	// webhook still say).
	MaxSignalMails = 3
	// AlertBurst openings within AlertBurstWindow are emailed one by one;
	// those past it wait for a digest, sent at most once a window.
	AlertBurst       = 5
	AlertBurstWindow = 10 * time.Minute
	// AlertRemindEvery: a critical alert open, unacknowledged and not
	// silenced is emailed again this long after it was last emailed.
	AlertRemindEvery = 4 * time.Hour
	// MaxAlertSilence is the longest a signal may be silenced.
	MaxAlertSilence = 24 * time.Hour
)

// How an alert's opening was told (alerts.notified).
const (
	NotifiedMail   = "mail"
	NotifiedHeld   = "held"
	NotifiedDigest = "digest"
	NotifiedQuiet  = "quiet"
)

// Admins' alert email settings (admins.alert_email).
const (
	AlertEmailAll      = "all"
	AlertEmailCritical = "critical"
	AlertEmailNone     = "none"
)

var (
	// ErrAlertOpen: the signal already has an open alert.
	ErrAlertOpen = errors.New("the signal already has an open alert")
	// ErrAlertResolved: the alert is resolved, so there's nothing to do.
	ErrAlertResolved = errors.New("the alert is resolved")
	// ErrAlreadyDone: the alert is acknowledged already, or the error
	// group resolved already.
	ErrAlreadyDone = errors.New("already done")
)

type alertRecord struct {
	bun.BaseModel  `bun:"table:alerts,alias:alt"`
	ID             int64           `bun:",pk,autoincrement"`
	PublicID       string          `bun:",nullzero"`
	Signal         string          `bun:",notnull"`
	Level          string          `bun:",notnull"`
	OpenedAt       time.Time       `bun:",notnull"`
	LastValue      float64         `bun:",notnull"`
	PeakValue      float64         `bun:",notnull"`
	Details        json.RawMessage `bun:"type:jsonb,notnull"`
	Notified       string          `bun:",notnull"`
	LastNotifiedAt *time.Time
	// MailTimes is read back with the row but written only by recordMail.
	MailTimes      json.RawMessage `bun:"mail_times,type:jsonb,scanonly"`
	AcknowledgedAt *time.Time
	AcknowledgedBy *int64
	SilencedUntil  *time.Time
	SilencedBy     *int64
	ResolvedAt     *time.Time
}

// Alert is an alert as the evaluator, the listeners and the dashboard read
// it. Details are its signal's drill-down and an integrity check's sample
// IDs, for the dashboard only. AcknowledgedBy and SilencedBy are nil once
// that account is gone, or when not done.
type Alert struct {
	ID             int64           `json:"-"`
	PublicID       string          `json:"id"`
	Signal         string          `json:"signal"`
	Level          string          `json:"level"`
	OpenedAt       time.Time       `json:"openedAt"`
	LastValue      float64         `json:"lastValue"`
	PeakValue      float64         `json:"peakValue"`
	Details        json.RawMessage `json:"details"`
	Notified       string          `json:"notified"`
	LastNotifiedAt *time.Time      `json:"lastNotifiedAt"`
	AcknowledgedAt *time.Time      `json:"acknowledgedAt"`
	AcknowledgedBy *AdminPerson    `json:"acknowledgedBy"`
	SilencedUntil  *time.Time      `json:"silencedUntil"`
	SilencedBy     *AdminPerson    `json:"silencedBy"`
	ResolvedAt     *time.Time      `json:"resolvedAt"`
	// The people's internal IDs, named into AcknowledgedBy and SilencedBy.
	acknowledgedByID *int64
	silencedByID     *int64
}

// Silenced reports whether the alert is silenced at at.
func (a Alert) Silenced(at time.Time) bool {
	return a.SilencedUntil != nil && a.SilencedUntil.After(at)
}

// alertOf is a stored alert as read.
func alertOf(r alertRecord) Alert {
	return Alert{
		ID: r.ID, PublicID: r.PublicID, Signal: r.Signal, Level: r.Level, OpenedAt: r.OpenedAt,
		LastValue: r.LastValue, PeakValue: r.PeakValue, Details: r.Details, Notified: r.Notified,
		LastNotifiedAt: r.LastNotifiedAt, AcknowledgedAt: r.AcknowledgedAt, SilencedUntil: r.SilencedUntil,
		ResolvedAt: r.ResolvedAt, acknowledgedByID: r.AcknowledgedBy, silencedByID: r.SilencedBy,
	}
}

// IndexOpenAlerts lists every open alert (at most one per signal), for the
// evaluator.
func (s *Store) IndexOpenAlerts(ctx context.Context) ([]Alert, error) {
	var rows []alertRecord
	err := s.DB.NewSelect().Model(&rows).Where("resolved_at IS NULL").OrderExpr("signal").Scan(ctx)
	out := make([]Alert, len(rows))
	for i, r := range rows {
		out[i] = alertOf(r)
	}
	return out, err
}

// NewAlert is an alert to open: its signal, level and value now, and its
// details (the drill-down, an integrity check's samples).
type NewAlert struct {
	Signal  string
	Level   string
	Value   float64
	Details map[string]any
	At      time.Time
}

// OpenAlert opens an alert for n.Signal at n.At, deciding how it's told
// (notified): quiet while a silence of its signal runs (one set on an
// earlier alert carries over until it ends) or when its signal opened
// another alert within AlertCooldown; held, for a digest, once
// AlertBurst alerts opened (and weren't quiet) within AlertBurstWindow;
// mail otherwise. Records alert.opened, saying whether to email and post.
// ErrAlertOpen when the signal already has an open alert.
func (s *Store) OpenAlert(ctx context.Context, n NewAlert) (Alert, error) {
	details, err := json.Marshal(n.Details)
	if err != nil {
		return Alert{}, err
	}
	if n.Details == nil {
		details = []byte("{}")
	}
	var out Alert
	err = s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockAlertWrites(ctx, tx); err != nil {
			return err
		}
		open, err := tx.NewSelect().Model((*alertRecord)(nil)).Where("signal = ? AND resolved_at IS NULL", n.Signal).Exists(ctx)
		if err != nil {
			return err
		}
		if open {
			return ErrAlertOpen
		}
		row := alertRecord{Signal: n.Signal, Level: n.Level, OpenedAt: n.At, LastValue: n.Value, PeakValue: n.Value, Details: details}
		if row.SilencedUntil, row.SilencedBy, err = runningSilence(ctx, tx, n.Signal, n.At); err != nil {
			return err
		}
		if row.Notified, err = openingNotice(ctx, tx, n.Signal, n.At, row.SilencedUntil != nil); err != nil {
			return err
		}
		if row.Notified == NotifiedMail {
			row.LastNotifiedAt = &n.At
		}
		if _, err := tx.NewInsert().Model(&row).Returning("id, public_id").Exec(ctx); err != nil {
			return err
		}
		if row.Notified == NotifiedMail {
			if err := recordMail(ctx, tx, row.ID, n.At); err != nil {
				return err
			}
		}
		out = alertOf(row)
		e := events.AlertOpened{Alert: row.PublicID, Signal: row.Signal, Level: row.Level, Value: n.Value,
			Mail: row.Notified == NotifiedMail, Webhook: row.Notified == NotifiedMail || row.Notified == NotifiedHeld}
		return s.record(ctx, tx, e, EventScope{})
	})
	return out, err
}

// runningSilence is a silence of signal still running at at, set on an
// earlier alert of it, and who set it; nil when there's none.
func runningSilence(ctx context.Context, tx bun.Tx, signal string, at time.Time) (*time.Time, *int64, error) {
	var row alertRecord
	err := tx.NewSelect().Model(&row).Column("silenced_until", "silenced_by").
		Where("signal = ? AND silenced_until > ?", signal, at).
		OrderExpr("silenced_until DESC").Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	return row.SilencedUntil, row.SilencedBy, err
}

// openingNotice decides how an opening at at is told (OpenAlert).
func openingNotice(ctx context.Context, tx bun.Tx, signal string, at time.Time, silenced bool) (string, error) {
	if silenced {
		return NotifiedQuiet, nil
	}
	recent, err := tx.NewSelect().Model((*alertRecord)(nil)).
		Where("signal = ? AND opened_at > ?", signal, at.Add(-AlertCooldown)).Exists(ctx)
	if err != nil || recent {
		return NotifiedQuiet, err
	}
	burst, err := mailsSince(ctx, tx, "", at.Add(-AlertBurstWindow))
	if err != nil {
		return "", err
	}
	if burst >= AlertBurst {
		return NotifiedHeld, nil
	}
	return NotifiedMail, nil
}

// mailsSince counts the emails sent since from about signal's alerts (all
// signals' when signal is empty), from each alert's mail_times.
func mailsSince(ctx context.Context, tx bun.Tx, signal string, from time.Time) (int, error) {
	q := tx.NewSelect().TableExpr("alerts AS alt, jsonb_array_elements_text(alt.mail_times) AS m(at)").
		ColumnExpr("count(*)").Where("m.at::timestamptz > ?", from)
	if signal != "" {
		q = q.Where("alt.signal = ?", signal)
	}
	var n int
	err := q.Scan(ctx, &n)
	return n, err
}

// mailAllowed reports whether an email about signal's alert may go at at
// (an escalation, a resolution or a reminder; an opening has its own
// rules): its signal has sent fewer than MaxSignalMails within
// AlertCooldown, and fewer than AlertBurst emails went anywhere within
// AlertBurstWindow.
func mailAllowed(ctx context.Context, tx bun.Tx, signal string, at time.Time) (bool, error) {
	perSignal, err := mailsSince(ctx, tx, signal, at.Add(-AlertCooldown))
	if err != nil || perSignal >= MaxSignalMails {
		return false, err
	}
	burst, err := mailsSince(ctx, tx, "", at.Add(-AlertBurstWindow))
	return burst < AlertBurst, err
}

// recordMail notes that an email about alert id went at at.
func recordMail(ctx context.Context, tx bun.Tx, id int64, at time.Time) error {
	_, err := tx.NewUpdate().Model((*alertRecord)(nil)).
		Set("mail_times = mail_times || jsonb_build_array(?::text)", at.UTC().Format(time.RFC3339Nano)).
		Set("last_notified_at = ?", at).
		Where("id = ?", id).Exec(ctx)
	return err
}

// UpdateAlertValue keeps an open alert's value and its peak (the highest
// seen), each minute it's read; no event.
func (s *Store) UpdateAlertValue(ctx context.Context, id int64, value float64) error {
	_, err := s.DB.NewUpdate().Model((*alertRecord)(nil)).
		Set("last_value = ?", value).
		Set("peak_value = GREATEST(peak_value, ?)", value).
		Where("id = ? AND resolved_at IS NULL", id).
		Exec(ctx)
	return err
}

// EscalateAlert raises an open warn alert to critical at at, recording
// alert.escalated: emailed and posted unless silenced (an alert that was
// quiet by the cooldown isn't told of at all; one held for a digest is
// emailed now, within the caps). False when it isn't open at warn any more.
func (s *Store) EscalateAlert(ctx context.Context, id int64, value float64, at time.Time) (bool, error) {
	escalated := false
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockAlertWrites(ctx, tx); err != nil {
			return err
		}
		var row alertRecord
		err := tx.NewUpdate().Model(&row).
			Set("level = ?", events.AlertCritical).
			Set("last_value = ?", value).
			Set("peak_value = GREATEST(peak_value, ?)", value).
			Where("id = ? AND level = ? AND resolved_at IS NULL", id, events.AlertWarn).
			Returning("id, public_id, signal, notified, silenced_until").
			Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		escalated = true
		// Told only when the opening was (not silenced, not quiet by the
		// cooldown), and within the email caps.
		told := !alertOf(row).Silenced(at) && row.Notified != NotifiedQuiet
		mail := false
		if told {
			if mail, err = mailAllowed(ctx, tx, row.Signal, at); err != nil {
				return err
			}
		}
		if mail {
			if err := recordMail(ctx, tx, row.ID, at); err != nil {
				return err
			}
		}
		e := events.AlertEscalated{Alert: row.PublicID, Signal: row.Signal, From: events.AlertWarn, To: events.AlertCritical,
			Value: value, Mail: mail, Webhook: told}
		return s.record(ctx, tx, e, EventScope{})
	})
	return escalated, err
}

// ResolveAlert closes an open alert at at, recording alert.resolved: an
// alert that was emailed is emailed again to say so, one that was posted
// is posted, unless silenced. False when it isn't open any more.
func (s *Store) ResolveAlert(ctx context.Context, id int64, at time.Time) (bool, error) {
	resolved := false
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockAlertWrites(ctx, tx); err != nil {
			return err
		}
		var row alertRecord
		err := tx.NewUpdate().Model(&row).
			Set("resolved_at = GREATEST(opened_at, ?)", at).
			Where("id = ? AND resolved_at IS NULL", id).
			Returning("id, public_id, signal, level, opened_at, notified, silenced_until, resolved_at").
			Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		resolved = true
		silenced := alertOf(row).Silenced(at)
		// Emailed again only if its opening was emailed, within the caps.
		mail := false
		if !silenced && row.Notified == NotifiedMail {
			if mail, err = mailAllowed(ctx, tx, row.Signal, at); err != nil {
				return err
			}
		}
		if mail {
			if err := recordMail(ctx, tx, row.ID, at); err != nil {
				return err
			}
		}
		e := events.AlertResolved{Alert: row.PublicID, Signal: row.Signal, Level: row.Level,
			OpenFor: int(row.ResolvedAt.Sub(row.OpenedAt).Seconds()),
			Mail:    mail,
			Webhook: !silenced && row.Notified != NotifiedQuiet}
		return s.record(ctx, tx, e, EventScope{})
	})
	return resolved, err
}

// RemindAlerts emails again, at at, every critical alert still open,
// neither acknowledged nor silenced, that was told of (not quiet), last
// emailed (or opened, if it never was) AlertRemindEvery ago or more and
// within the email caps (mailAllowed), recording alert.reminded for each.
// It says how many.
func (s *Store) RemindAlerts(ctx context.Context, at time.Time) (int, error) {
	reminded := 0
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockAlertWrites(ctx, tx); err != nil {
			return err
		}
		var rows []alertRecord
		err := tx.NewSelect().Model(&rows).
			Where("resolved_at IS NULL AND level = ? AND acknowledged_at IS NULL", events.AlertCritical).
			Where("(silenced_until IS NULL OR silenced_until <= ?)", at).
			Where("coalesce(last_notified_at, opened_at) <= ?", at.Add(-AlertRemindEvery)).
			Where("notified <> ?", NotifiedQuiet).
			OrderExpr("id").
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, row := range rows {
			ok, err := mailAllowed(ctx, tx, row.Signal, at)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := recordMail(ctx, tx, row.ID, at); err != nil {
				return err
			}
			e := events.AlertReminded{Alert: row.PublicID, Signal: row.Signal, Level: row.Level, Value: row.LastValue}
			if err := s.record(ctx, tx, e, EventScope{}); err != nil {
				return err
			}
			reminded++
		}
		return nil
	})
	return reminded, err
}

// DigestAlerts gathers the openings held for a digest into one,
// recording alert.digested, unless a digest went within AlertBurstWindow
// before at (they wait for the next). It says how many it gathered. A
// silence set since an opening was held leaves it out (quiet).
func (s *Store) DigestAlerts(ctx context.Context, at time.Time) (int, error) {
	gathered := 0
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockAlertWrites(ctx, tx); err != nil {
			return err
		}
		recent, err := tx.NewSelect().Model((*alertRecord)(nil)).
			Where("notified = ? AND last_notified_at > ?", NotifiedDigest, at.Add(-AlertBurstWindow)).Exists(ctx)
		if err != nil || recent {
			return err
		}
		if _, err := tx.NewUpdate().Model((*alertRecord)(nil)).
			Set("notified = ?", NotifiedQuiet).
			Where("notified = ? AND silenced_until > ?", NotifiedHeld, at).
			Exec(ctx); err != nil {
			return err
		}
		var rows []alertRecord
		err = tx.NewUpdate().Model(&rows).
			Set("notified = ?", NotifiedDigest).
			Set("last_notified_at = ?", at).
			Where("notified = ?", NotifiedHeld).
			Returning("public_id, signal, level, last_value, opened_at").
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		e := events.AlertDigested{Alerts: make([]events.DigestedAlert, len(rows))}
		for i, row := range rows {
			e.Alerts[i] = events.DigestedAlert{Alert: row.PublicID, Signal: row.Signal, Level: row.Level, Value: row.LastValue}
		}
		gathered = len(rows)
		return s.record(ctx, tx, e, EventScope{})
	})
	return gathered, err
}
