package store

import (
	"context"
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
