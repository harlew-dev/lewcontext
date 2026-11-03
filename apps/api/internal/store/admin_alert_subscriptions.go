package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"slices"
)

// Alert email is opt-in: nobody is emailed about a signal until they
// subscribe to it. A subscription is a signal and the lowest level wanted.

type adminAlertSubscription struct {
	bun.BaseModel `bun:"table:admin_alert_subscriptions,alias:aas"`
	ID            int64 `bun:",pk,autoincrement"`
	AdminUserID   int64
	Signal        string
	MinLevel      string
	Events        []string `bun:",array"`
}

// AlertSubscription is one signal an admin gets email about, from MinLevel
// (warn or critical) up, for the Events kinds they chose. In an update,
// MinLevel off unsubscribes, and left-out Events mean every kind (an empty
// list, nothing).
type AlertSubscription struct {
	Signal   string   `json:"signal"`
	MinLevel string   `bun:"min_level" json:"minLevel"`
	Events   []string `bun:"events,array" json:"events"`
}

// MaxAlertSubscriptions is the most signals one admin may subscribe to.
const MaxAlertSubscriptions = 500

var ErrAlertSubscription = errors.New("check alert subscriptions")

// IndexAdminAlertSubscriptions lists the signals admin is subscribed to.
func (s *Store) IndexAdminAlertSubscriptions(ctx context.Context, adminID int64) ([]AlertSubscription, error) {
	out := []AlertSubscription{}
	err := s.DB.NewSelect().Model((*adminAlertSubscription)(nil)).Column("signal", "min_level", "events").Where("admin_user_id = ?", adminID).Order("signal").Scan(ctx, &out)
	return out, err
}

// UpdateAdminAlertSubscriptions applies a bounded patch to admin's own
// subscriptions, atomically under their row lock: a listed signal is
// subscribed at its level, or, with MinLevel off, unsubscribed; the others
// are unchanged. known are the catalogue's signals (given by the HTTP
// boundary, which avoids a store -> monitor import cycle): a signal no longer
// in it can only be unsubscribed. One admin.updated records the signals that
// changed (changes.alertSubscriptions), none when nothing did.
func (s *Store) UpdateAdminAlertSubscriptions(ctx context.Context, admin User, patch []AlertSubscription, known map[string]bool) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row adminRecord
		err := tx.NewSelect().Model(&row).Where("user_id = ?", admin.ID).For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotAdmin
		}
		if err != nil {
			return err
		}
		var existing []adminAlertSubscription
		if err := tx.NewSelect().Model(&existing).Where("admin_user_id = ?", admin.ID).Scan(ctx); err != nil {
			return err
		}
		before := map[string]adminAlertSubscription{}
		for _, p := range existing {
			before[p.Signal] = p
		}
		changed := []AlertSubscription{}
		count := len(before)
		for _, p := range patch {
			old, exists := before[p.Signal]
			if p.MinLevel == "off" {
				if !exists {
					continue
				}
				if _, err := tx.NewDelete().Model((*adminAlertSubscription)(nil)).Where("admin_user_id = ? AND signal = ?", admin.ID, p.Signal).Exec(ctx); err != nil {
					return err
				}
				count--
			} else {
				if !known[p.Signal] || (p.MinLevel != "warn" && p.MinLevel != "critical") {
					return ErrAlertSubscription
				}
				kinds, err := alertEventKinds(p.Events)
				if err != nil {
					return err
				}
				p.Events = kinds
				if exists && old.MinLevel == p.MinLevel && slices.Equal(old.Events, kinds) {
					continue
				}
				if !exists {
					count++
				}
				v := adminAlertSubscription{AdminUserID: admin.ID, Signal: p.Signal, MinLevel: p.MinLevel, Events: kinds}
				if _, err := tx.NewInsert().Model(&v).On("CONFLICT (admin_user_id, signal) DO UPDATE").Set("min_level = EXCLUDED.min_level").Set("events = EXCLUDED.events").Exec(ctx); err != nil {
					return err
				}
			}
			changed = append(changed, p)
		}
		if count > MaxAlertSubscriptions {
			return ErrAlertSubscription
		}
		if len(changed) == 0 {
			return nil
		}
		return s.record(ctx, tx, adminPreferencesUpdated{User: admin.PublicID, Changes: map[string]any{"alertSubscriptions": changed}}, EventScope{ActorID: admin.ID, UserID: admin.ID})
	})
}

// alertEventKinds checks the kinds an email is about and puts them in the
// dashboard's order, without repeats. nil (none given) is every kind; an
// empty list is none.
func alertEventKinds(given []string) ([]string, error) {
	if given == nil {
		return append([]string{}, events.AlertWebhookKinds...), nil
	}
	for _, k := range given {
		if !slices.Contains(events.AlertWebhookKinds, k) {
			return nil, ErrAlertSubscription
		}
	}
	out := []string{}
	for _, k := range events.AlertWebhookKinds {
		if slices.Contains(given, k) {
			out = append(out, k)
		}
	}
	return out, nil
}

type adminPreferencesUpdated struct {
	User    string         `json:"user"`
	Changes map[string]any `json:"changes"`
}

func (adminPreferencesUpdated) EventName() string { return "admin.updated" }

// AlertRecipient is an admin who subscribed to any alert email: their
// address and the signals they chose, each with the lowest level wanted and
// the kinds of event wanted.
type AlertRecipient struct {
	Email         string
	Subscriptions map[string]string
	Kinds         map[string][]string
}

// CoversLevel reports whether this admin subscribed to signal at a level the
// alert has reached: a warn subscription takes warnings and critical, a
// critical one only critical. No subscription, no email.
func (r AlertRecipient) CoversLevel(signal, level string) bool {
	switch r.Subscriptions[signal] {
	case "warn":
		return level == events.AlertWarn || level == events.AlertCritical
	case "critical":
		return level == events.AlertCritical
	}
	return false
}

// Covers is CoversLevel and the kind of event too (opened, escalated,
// recovered, reopened, resolved, reminded; a digest is of openings): email
// goes only for the kinds the admin chose for the signal.
func (r AlertRecipient) Covers(signal, level, kind string) bool {
	return r.CoversLevel(signal, level) && slices.Contains(r.Kinds[signal], kind)
}

// IndexAlertRecipients lists the admins subscribed to at least one signal,
// oldest grant first, read when the listener runs.
func (s *Store) IndexAlertRecipients(ctx context.Context) ([]AlertRecipient, error) {
	var rows []struct {
		Email    string   `bun:"email"`
		Signal   string   `bun:"signal"`
		MinLevel string   `bun:"min_level"`
		Events   []string `bun:"events,array"`
	}
	err := s.DB.NewSelect().Model((*adminAlertSubscription)(nil)).
		ColumnExpr("u.email, aas.signal, aas.min_level, aas.events").
		Join("JOIN admins AS adm ON adm.user_id = aas.admin_user_id").
		Join("JOIN users AS u ON u.id = aas.admin_user_id").
		OrderExpr("adm.created_at, adm.user_id, aas.signal").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := []AlertRecipient{}
	index := map[string]int{}
	for _, r := range rows {
		i, ok := index[r.Email]
		if !ok {
			i = len(out)
			index[r.Email] = i
			out = append(out, AlertRecipient{Email: r.Email, Subscriptions: map[string]string{}, Kinds: map[string][]string{}})
		}
		out[i].Subscriptions[r.Signal] = r.MinLevel
		out[i].Kinds[r.Signal] = r.Events
	}
	return out, nil
}
