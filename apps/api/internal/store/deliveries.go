package store

import (
	"context"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/observe"
	"time"
	"unicode/utf8"
)

// The store is the events worker's queue: event_deliveries holds one row per
// event and listener. Claiming leases rows (pushes next_attempt_at ahead) in
// a short statement, so listeners run with no lock or transaction held.
var _ events.Queue = (*Store)(nil)

type deliveryRecord struct {
	bun.BaseModel `bun:"table:event_deliveries,alias:d"`
	EventID       int64  `bun:",pk"`
	Listener      string `bun:",pk"`
	Attempts      int
	NextAttemptAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeliveredAt   time.Time `bun:",nullzero"`
	FailedAt      time.Time `bun:",nullzero"`
	LastError     string
}

// maxLastError matches the last_error column's CHECK.
const maxLastError = 500

// queueDeliveries queues eventID for each of the event's listeners.
func (s *Store) queueDeliveries(ctx context.Context, db bun.IDB, eventID int64, event string) error {
	if s.Listeners == nil {
		return nil
	}
	names := s.Listeners.ListenersFor(event)
	if len(names) == 0 {
		return nil
	}
	rows := make([]deliveryRecord, len(names))
	for i, name := range names {
		rows[i] = deliveryRecord{EventID: eventID, Listener: name}
	}
	_, err := db.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// claimedRow is a claimed delivery with its event, as RETURNING gives it.
type claimedRow struct {
	EventID   int64
	Listener  string
	Attempts  int
	PublicID  string
	Name      string
	Payload   []byte
	CreatedAt time.Time
}

// Claim leases up to limit due deliveries, oldest first. FOR UPDATE SKIP
// LOCKED lets several workers claim at once without taking each other's rows
// or waiting on them; the row locks end with the statement.
func (s *Store) Claim(ctx context.Context, limit int, lease time.Duration) ([]events.Delivery, error) {
	due := s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		Column("event_id", "listener").
		Where("delivered_at IS NULL AND failed_at IS NULL AND next_attempt_at <= now()").
		OrderExpr("next_attempt_at, event_id").
		Limit(int64(limit)).
		For("UPDATE SKIP LOCKED")
	var rows []claimedRow
	err := s.DB.NewUpdate().
		With("due", due).
		Model((*deliveryRecord)(nil)).
		TableExpr("due").
		TableExpr("events AS e").
		Set("attempts = d.attempts + 1").
		Set("next_attempt_at = now() + ? * interval '1 millisecond'", lease.Milliseconds()).
		Where("d.event_id = due.event_id AND d.listener = due.listener").
		Where("e.id = d.event_id").
		Returning("d.event_id, d.listener, d.attempts, e.public_id, e.name, e.payload, e.created_at").
		Scan(ctx, &rows)
	claimed := make([]events.Delivery, len(rows))
	for i, r := range rows {
		claimed[i] = events.Delivery{EventID: r.EventID, Listener: r.Listener, Attempts: r.Attempts, PublicID: r.PublicID, Event: r.Name, Payload: r.Payload, CreatedAt: r.CreatedAt}
	}
	return claimed, err
}

// Delivered marks a delivery done.
func (s *Store) Delivered(ctx context.Context, d events.Delivery) error {
	return s.updateDelivery(ctx, d, "delivered_at = now(), last_error = ''")
}

// Retry schedules a failed delivery's next attempt.
func (s *Store) Retry(ctx context.Context, d events.Delivery, at time.Time, cause error) error {
	return s.updateDelivery(ctx, d, "next_attempt_at = ?, last_error = ?", at, errorText(cause))
}

// Fail gives up on a delivery.
func (s *Store) Fail(ctx context.Context, d events.Delivery, cause error) error {
	return s.updateDelivery(ctx, d, "failed_at = now(), last_error = ?", errorText(cause))
}

func (s *Store) updateDelivery(ctx context.Context, d events.Delivery, set string, args ...any) error {
	_, err := s.DB.NewUpdate().Model((*deliveryRecord)(nil)).
		Set(set, args...).
		Where("event_id = ? AND listener = ?", d.EventID, d.Listener).
		Exec(ctx)
	return err
}

// errorText is the error's message scrubbed (observe.Scrub: an SMTP refusal
// names the recipient, a database error can quote a value), as text
// PostgreSQL accepts (valid UTF-8, no NUL), cut to fit last_error.
func errorText(err error) string {
	text := observe.ScrubError(err)
	if utf8.RuneCountInString(text) > maxLastError {
		text = string([]rune(text)[:maxLastError])
	}
	return text
}

// MaxDeliveriesRetried is how many failed deliveries one bulk retry queues
// again, at most: for after an SMTP outage.
const MaxDeliveriesRetried = 1000

var (
	// ErrDeliveryNotFailed: a retry of a delivery that hasn't failed (it's
	// pending or delivered).
	ErrDeliveryNotFailed = errors.New("the delivery hasn't failed")
	// ErrNothingToRetry: no failed delivery matches a bulk retry.
	ErrNothingToRetry = errors.New("no failed delivery to retry")
)

// requeue is what queuing a failed delivery again sets: not failed, no
// attempts yet, due now. Its listener runs as for a new event; reset,
// invitation and sign-up listeners make a fresh link or code when they
// run, so an old one is never sent again.
func requeue(q *bun.UpdateQuery) *bun.UpdateQuery {
	return q.Set("failed_at = NULL").Set("attempts = 0").Set("next_attempt_at = now()")
}

// RequeueDelivery queues the failed delivery of the event publicID to
// listener again, recording admin.delivery.retried. sql.ErrNoRows when
// there's no such delivery, ErrDeliveryNotFailed when it hasn't failed.
func (s *Store) RequeueDelivery(ctx context.Context, publicID, listener string, a AdminAction) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var d deliveryRecord
		err := tx.NewSelect().Model(&d).
			Join("JOIN events AS e ON e.id = d.event_id").
			Where("e.public_id = ? AND d.listener = ?", publicID, listener).
			For("UPDATE OF d").
			Scan(ctx)
		if err != nil {
			return err
		}
		if d.FailedAt.IsZero() {
			return ErrDeliveryNotFailed
		}
		if _, err := requeue(tx.NewUpdate().Model((*deliveryRecord)(nil))).Where("event_id = ? AND listener = ?", d.EventID, listener).Exec(ctx); err != nil {
			return err
		}
		e := events.AdminDeliveryRetried{Admin: a.Admin.PublicID, Reason: a.Reason, Listener: listener, Event: publicID, Count: 1}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
}

// RequeueFailedDeliveries queues again every delivery to listener that
// failed at or after since, the most recent MaxDeliveriesRetried of them,
// recording admin.delivery.retried with how many. ErrNothingToRetry when
// none did (nothing is recorded).
func (s *Store) RequeueFailedDeliveries(ctx context.Context, listener string, since time.Time, a AdminAction) (int, error) {
	var count int
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		failed := tx.NewSelect().Model((*deliveryRecord)(nil)).
			Column("event_id").
			Where("listener = ? AND failed_at IS NOT NULL AND failed_at >= ?", listener, since).
			OrderExpr("failed_at DESC, event_id DESC").
			Limit(MaxDeliveriesRetried).
			For("UPDATE")
		res, err := requeue(tx.NewUpdate().Model((*deliveryRecord)(nil))).
			Where("listener = ? AND event_id IN (?)", listener, failed).
			Exec(ctx)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if count = int(n); count == 0 {
			return ErrNothingToRetry
		}
		e := events.AdminDeliveryRetried{Admin: a.Admin.PublicID, Reason: a.Reason, Listener: listener, FailedSince: &since, Count: count}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
	return count, err
}
