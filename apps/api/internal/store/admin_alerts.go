package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"time"
)

// What the admin dashboard reads and does about alerts
// (docs/api/monitoring.md#alerts): the Alerts page, acknowledging and
// silencing; resolving an error group; an admin's own alert email setting
// and the recipients the alert listener emails.

// Alert states the Alerts page filters by.
const (
	AlertStateOpen     = "open"
	AlertStateResolved = "resolved"
	AlertStateAll      = "all"
)

// AlertFilter narrows the Alerts list: its state, and Before (alt_…), the
// cursor, for older alerts.
type AlertFilter struct {
	State  string
	Before string
	// Search matches a title or signal name, any case, wildcards literal.
	Search string
	// Level is the alert's level, warn or critical; Health its recovery
	// state (active, confirming, recovered, unknown), as last stored.
	Level, Health string
	// Signals narrows to those signals' alerts.
	Signals []string
	// Acknowledged and Silenced are "yes" or "no"; empty doesn't filter.
	Acknowledged, Silenced string
	// From (inclusive) and To (exclusive) bound when it opened.
	From, To *time.Time
}

// AlertPage is one page of alerts and the cursor for the next, older one
// (nil at the end).
type AlertPage struct {
	Data []Alert `json:"data"`
	Next *string `json:"next"`
}

// IndexAlerts lists alerts in state, up to perPage: the open ones critical
// first, then newest; resolved and all newest first, by when they opened.
// ErrNoCursor when f.Before is unknown, or used with the open ones (there
// are at most one per signal, all on one page).
func (s *Store) IndexAlerts(ctx context.Context, f AlertFilter, perPage int) (AlertPage, error) {
	q := s.DB.NewSelect().Model((*alertRecord)(nil)).Limit(int64(perPage + 1))
	switch f.State {
	case AlertStateOpen:
		if f.Before != "" {
			return AlertPage{}, ErrNoCursor
		}
		q = q.Where("alt.resolved_at IS NULL").OrderExpr("(alt.state = 'recovered' AND alt.last_successful_at >= now() - CASE WHEN alt.hourly THEN interval '65 minutes' ELSE interval '2 minutes' END) ASC, alt.level = ? DESC, alt.opened_at DESC, alt.id DESC", events.AlertCritical)
	case AlertStateResolved:
		q = q.Where("alt.resolved_at IS NOT NULL").OrderExpr("alt.opened_at DESC, alt.id DESC")
	default:
		q = q.OrderExpr("alt.opened_at DESC, alt.id DESC")
	}
	if f.Search != "" {
		pattern := containsPattern(f.Search)
		q = q.Where(`(alt.external_title ILIKE ? ESCAPE '\' OR alt.signal ILIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if f.Level != "" {
		q = q.Where("alt.level = ?", f.Level)
	}
	if f.Health != "" {
		q = q.Where("alt.state = ?", f.Health)
	}
	if len(f.Signals) > 0 {
		q = q.Where("alt.signal IN (?)", bun.In(f.Signals))
	}
	switch f.Acknowledged {
	case "yes":
		q = q.Where("alt.acknowledged_at IS NOT NULL")
	case "no":
		q = q.Where("alt.acknowledged_at IS NULL")
	}
	switch f.Silenced {
	case "yes":
		q = q.Where("alt.silenced_until > now()")
	case "no":
		q = q.Where("(alt.silenced_until IS NULL OR alt.silenced_until <= now())")
	}
	if f.From != nil {
		q = q.Where("alt.opened_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("alt.opened_at < ?", *f.To)
	}
	if f.Before != "" {
		var cursor alertRecord
		err := s.DB.NewSelect().Model(&cursor).Column("id", "opened_at").Where("public_id = ?", f.Before).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return AlertPage{}, ErrNoCursor
		}
		if err != nil {
			return AlertPage{}, err
		}
		q = q.Where("(alt.opened_at, alt.id) < (?, ?)", cursor.OpenedAt, cursor.ID)
	}
	var rows []alertRecord
	if err := q.Scan(ctx, &rows); err != nil {
		return AlertPage{}, err
	}
	page := AlertPage{Data: []Alert{}}
	if len(rows) > perPage {
		rows = rows[:perPage]
		next := rows[perPage-1].PublicID
		page.Next = &next
	}
	for _, r := range rows {
		a := alertOf(r)
		page.Data = append(page.Data, a)
	}
	return page, s.nameAlertPeople(ctx, page.Data)
}

// FindAlert finds the alert publicID; sql.ErrNoRows when there's none.
func (s *Store) FindAlert(ctx context.Context, publicID string) (Alert, error) {
	var row alertRecord
	if err := s.DB.NewSelect().Model(&row).Where("public_id = ?", publicID).Scan(ctx); err != nil {
		return Alert{}, err
	}
	alerts := []Alert{alertOf(row)}
	err := s.nameAlertPeople(ctx, alerts)
	return alerts[0], err
}

// nameAlertPeople names who acknowledged and silenced each alert, nil once
// that account is gone.
func (s *Store) nameAlertPeople(ctx context.Context, alerts []Alert) error {
	at, clockErr := s.FindAlertEvaluationTime(ctx)
	if clockErr != nil {
		return clockErr
	}
	for i := range alerts {
		alerts[i].AlertHealth = alerts[i].AlertHealth.At(at)
	}
	var ids []int64
	for _, a := range alerts {
		for _, id := range []*int64{a.acknowledgedByID, a.silencedByID} {
			if id != nil {
				ids = append(ids, *id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var users []User
	if err := s.DB.NewSelect().Model(&users).Column("id", "public_id", "name", "email").Where("u.id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return err
	}
	people := map[int64]*AdminPerson{}
	for _, u := range users {
		people[u.ID] = &AdminPerson{PublicID: u.PublicID, Name: u.Name, Email: u.Email}
	}
	for i := range alerts {
		if id := alerts[i].acknowledgedByID; id != nil {
			alerts[i].AcknowledgedBy = people[*id]
		}
		if id := alerts[i].silencedByID; id != nil {
			alerts[i].SilencedBy = people[*id]
		}
	}
	return nil
}

// CountOpenAlerts counts the open alerts, for the admin sidebar's badge.
func (s *Store) CountOpenAlerts(ctx context.Context) (int, error) {
	n, err := s.DB.NewSelect().Model((*alertRecord)(nil)).Where("resolved_at IS NULL").Count(ctx)
	return int(n), err
}

// AcknowledgeAlert marks the open alert publicID acknowledged by admin,
// which stops its reminders, recording admin.alert.acknowledged.
// sql.ErrNoRows when there's none; ErrAlertResolved when it's resolved;
// ErrAlreadyDone when it's acknowledged already.
func (s *Store) AcknowledgeAlert(ctx context.Context, publicID string, admin User) (Alert, error) {
	var out Alert
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := lockOpenAlert(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if row.AcknowledgedAt != nil {
			return ErrAlreadyDone
		}
		if err := tx.NewUpdate().Model(&row).
			Set("acknowledged_at = now()").
			Set("acknowledged_by = ?", admin.ID).
			WherePK().
			Returning("*").
			Scan(ctx); err != nil {
			return err
		}
		out = alertOf(row)
		out.AcknowledgedBy = &AdminPerson{PublicID: admin.PublicID, Name: admin.Name, Email: admin.Email}
		e := events.AdminAlertAcknowledged{Admin: admin.PublicID, Alert: row.PublicID, Signal: row.Signal}
		return s.record(ctx, tx, e, EventScope{ActorID: admin.ID})
	})
	if err != nil {
		return Alert{}, err
	}
	return out, s.nameAlertPeople(ctx, []Alert{out})
}

// SilenceAlert silences the open alert publicID's signal for d (at most
// MaxAlertSilence, from the database's clock), recording
// admin.alert.silenced with the reason: no email or webhook about the
// alert, nor about another alert of its signal opening meanwhile. Errors
// as AcknowledgeAlert's, without ErrAlreadyDone: silencing again sets a
// new end.
func (s *Store) SilenceAlert(ctx context.Context, publicID string, d time.Duration, a AdminAction) (Alert, error) {
	if d <= 0 || d > MaxAlertSilence {
		return Alert{}, errors.New("store: a silence lasts up to 24 hours")
	}
	var out Alert
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := lockOpenAlert(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if err := tx.NewUpdate().Model(&row).
			Set("silenced_until = now() + make_interval(secs => ?)", int(d.Seconds())).
			Set("silenced_by = ?", a.Admin.ID).
			WherePK().
			Returning("*").
			Scan(ctx); err != nil {
			return err
		}
		out = alertOf(row)
		e := events.AdminAlertSilenced{Admin: a.Admin.PublicID, Alert: row.PublicID, Signal: row.Signal, Until: *row.SilencedUntil, Reason: a.Reason}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
	if err != nil {
		return Alert{}, err
	}
	alerts := []Alert{out}
	err = s.nameAlertPeople(ctx, alerts)
	return alerts[0], err
}

// lockOpenAlert locks the alert publicID for a change. sql.ErrNoRows when
// there's none; ErrAlertResolved when it's resolved.
func lockOpenAlert(ctx context.Context, tx bun.Tx, publicID string) (alertRecord, error) {
	var row alertRecord
	if err := database.LockAlertWrites(ctx, tx); err != nil {
		return row, err
	}
	if err := tx.NewSelect().Model(&row).Where("public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
		return row, err
	}
	if row.ResolvedAt != nil {
		return row, ErrAlertResolved
	}
	return row, nil
}
