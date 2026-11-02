package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
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
		q = q.Where("alt.resolved_at IS NULL").OrderExpr("alt.level = ? DESC, alt.opened_at DESC, alt.id DESC", events.AlertCritical)
	case AlertStateResolved:
		q = q.Where("alt.resolved_at IS NOT NULL").OrderExpr("alt.opened_at DESC, alt.id DESC")
	default:
		q = q.OrderExpr("alt.opened_at DESC, alt.id DESC")
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
		page.Data = append(page.Data, alertOf(r))
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
	if err := tx.NewSelect().Model(&row).Where("public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
		return row, err
	}
	if row.ResolvedAt != nil {
		return row, ErrAlertResolved
	}
	return row, nil
}

// ResolveErrorGroup marks the grouped error publicID (err_…) resolved,
// recording admin.error.resolved; its next occurrence opens it again
// (RecordErrors). sql.ErrNoRows when there's none; ErrAlreadyDone when
// it's resolved already.
func (s *Store) ResolveErrorGroup(ctx context.Context, publicID string, admin User) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row appErrorRecord
		err := tx.NewSelect().Model(&row).Column("id", "resolved_at").Where("public_id = ?", publicID).For("UPDATE").Scan(ctx)
		if err != nil {
			return err
		}
		if !row.ResolvedAt.IsZero() {
			return ErrAlreadyDone
		}
		if _, err := tx.NewUpdate().Model((*appErrorRecord)(nil)).Set("resolved_at = now()").Where("id = ?", row.ID).Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, events.AdminErrorResolved{Admin: admin.PublicID, Error: publicID}, EventScope{ActorID: admin.ID})
	})
}

// FindAdminAlertEmail is which alerts admin is emailed (AlertEmailAll,
// AlertEmailCritical, AlertEmailNone). ErrNotAdmin when they aren't an
// admin.
func (s *Store) FindAdminAlertEmail(ctx context.Context, admin User) (string, error) {
	var setting string
	err := s.DB.NewSelect().Model((*adminRecord)(nil)).Column("alert_email").Where("user_id = ?", admin.ID).Scan(ctx, &setting)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotAdmin
	}
	return setting, err
}

// UpdateAdminAlertEmail sets which alerts admin is emailed, their own
// setting, recording admin.updated (changes.alertEmail, the admin as
// actor) when it changed. ErrNotAdmin when they aren't an admin.
func (s *Store) UpdateAdminAlertEmail(ctx context.Context, admin User, setting string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row adminRecord
		err := tx.NewSelect().Model(&row).Where("user_id = ?", admin.ID).For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotAdmin
		}
		if err != nil || row.AlertEmail == setting {
			return err
		}
		if _, err := tx.NewUpdate().Model((*adminRecord)(nil)).Set("alert_email = ?", setting).Where("user_id = ?", admin.ID).Exec(ctx); err != nil {
			return err
		}
		changes := events.Changes{}
		changes.Add("alertEmail", row.AlertEmail, setting)
		e := events.AdminUpdated{User: admin.PublicID, Changes: changes}
		return s.record(ctx, tx, e, EventScope{ActorID: admin.ID, UserID: admin.ID})
	})
}

// AlertRecipient is an admin the alert listener may email: their address
// and their setting.
type AlertRecipient struct {
	Email      string `bun:"email"`
	AlertEmail string `bun:"alert_email"`
}

// IndexAlertRecipients lists the admins who get any alert email (their
// setting isn't none), oldest grant first.
func (s *Store) IndexAlertRecipients(ctx context.Context) ([]AlertRecipient, error) {
	out := []AlertRecipient{}
	err := s.DB.NewSelect().Model((*adminRecord)(nil)).
		ColumnExpr("u.email, adm.alert_email").
		Join("JOIN users AS u ON u.id = adm.user_id").
		Where("adm.alert_email <> ?", AlertEmailNone).
		OrderExpr("adm.created_at, adm.user_id").
		Scan(ctx, &out)
	return out, err
}
