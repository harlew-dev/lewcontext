package store

import (
	"context"
	"encoding/json"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// EventScope says whom an event concerns, by internal ID, for the events
// table's columns; zero leaves a column empty. ActorID is who acted, and
// defaults to the signed-in user in the context (events.WithActor). UserID is
// the account an event is about, WorkspaceID the workspace.
type EventScope struct {
	ActorID, UserID, WorkspaceID int64
}

type eventRecord struct {
	bun.BaseModel `bun:"table:events,alias:e"`
	ID            int64  `bun:",pk,autoincrement"`
	PublicID      string `bun:",nullzero"`
	Name          string
	ActorID       int64           `bun:",nullzero"`
	UserID        int64           `bun:",nullzero"`
	WorkspaceID   int64           `bun:",nullzero"`
	Payload       json.RawMessage `bun:"type:jsonb"`
	// IP is the client's address for an event recorded during a request
	// (events.WithRequest), and null for the CLI and listeners.
	IP string `bun:"ip,type:inet,nullzero"`
	// RequestID is the ID of the request it was recorded in
	// (events.WithRequest), its log lines' requestId; "" outside one.
	RequestID string
	UserAgent string
	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// record writes e, and queues a delivery to each of its listeners. Store
// methods that change data call it inside their transaction, so the change,
// its event and its deliveries are saved together or not at all.
func (s *Store) record(ctx context.Context, db bun.IDB, e events.Event, scope EventScope) error {
	_, err := s.recordID(ctx, db, e, scope)
	return err
}

// recordID is record, returning the event's public ID (evt_…), for a row
// that a listener of the event must find.
func (s *Store) recordID(ctx context.Context, db bun.IDB, e events.Event, scope EventScope) (string, error) {
	payload, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	if scope.ActorID == 0 {
		scope.ActorID = events.Actor(ctx)
	}
	row := eventRecord{
		Name:        e.EventName(),
		ActorID:     scope.ActorID,
		UserID:      scope.UserID,
		WorkspaceID: scope.WorkspaceID,
		Payload:     payload,
		IP:          events.IP(ctx),
		RequestID:   events.RequestID(ctx),
		UserAgent:   events.UserAgent(ctx),
	}
	if _, err = db.NewInsert().Model(&row).Returning("id, public_id").Exec(ctx); err != nil {
		return "", err
	}
	return row.PublicID, s.queueDeliveries(ctx, db, row.ID, row.Name)
}

// Record writes an event that has no data change of its own, such as a login
// attempt or a rate limit being reached.
func (s *Store) Record(ctx context.Context, e events.Event, scope EventScope) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return s.record(ctx, tx, e, scope)
	})
}
