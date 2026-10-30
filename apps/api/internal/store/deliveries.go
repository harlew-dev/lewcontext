package store

import (
	"context"
	"forma/api/internal/events"
	"github.com/uptrace/bun"
	"strings"
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

// Claim leases up to limit due deliveries, oldest event first. FOR UPDATE
// SKIP LOCKED lets several workers claim at once without taking each
// other's rows or waiting on them; the row locks end with the statement.
func (s *Store) Claim(ctx context.Context, limit int, lease time.Duration) ([]events.Delivery, error) {
	var claimed []events.Delivery
	rows, err := s.DB.QueryContext(ctx, `
		WITH due AS (
			SELECT event_id, listener FROM event_deliveries
			WHERE delivered_at IS NULL AND failed_at IS NULL AND next_attempt_at <= now()
			ORDER BY next_attempt_at, event_id
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		UPDATE event_deliveries AS d
		SET attempts = d.attempts + 1, next_attempt_at = now() + ? * interval '1 millisecond'
		FROM due, events AS e
		WHERE d.event_id = due.event_id AND d.listener = due.listener AND e.id = d.event_id
		RETURNING d.event_id, d.listener, d.attempts, e.public_id, e.name, e.payload, e.created_at`,
		limit, lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d events.Delivery
		if err := rows.Scan(&d.EventID, &d.Listener, &d.Attempts, &d.PublicID, &d.Event, &d.Payload, &d.CreatedAt); err != nil {
			return nil, err
		}
		claimed = append(claimed, d)
	}
	return claimed, rows.Err()
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

// errorText is the error's message as text PostgreSQL accepts (valid UTF-8,
// no NUL), cut to fit last_error.
func errorText(err error) string {
	text := strings.ReplaceAll(strings.ToValidUTF8(err.Error(), ""), "\x00", "")
	if utf8.RuneCountInString(text) > maxLastError {
		text = string([]rune(text)[:maxLastError])
	}
	return text
}
