package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/observe"
	"log/slog"
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
	// AdminRequest marks an event recorded in an admin dashboard request
	// (events.WithAdminRequest): an admin's reads and actions, left out
	// of the admin's own Download my data.
	AdminRequest bool
	CreatedAt    time.Time `bun:",nullzero,notnull,default:current_timestamp"`
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
		// Marked from the context alone, so a dashboard action added later
		// is marked too.
		AdminRequest: events.AdminRequest(ctx),
	}
	if events.Audit(row.Name) {
		err = insertAudited(ctx, db, &row)
	} else {
		_, err = db.NewInsert().Model(&row).Returning("id, public_id").Exec(ctx)
	}
	if err != nil {
		return "", err
	}
	// Counted once written, in a transaction that may still roll back.
	observe.EventsRecorded.Inc(row.Name)
	return row.PublicID, s.queueDeliveries(ctx, db, row.ID, row.Name)
}

// insertAudited inserts an audit event (events.Audit) and writes it as a
// log line too (docs/compliance.md#audit-lines), msg "audit": the event's
// evt_ ID, name, the actor's, user's and workspace's public IDs, the IP and
// when, with the request's ID; never the payload, so no email or name. The
// operator ships these lines off the server, where a row altered or deleted
// later disagrees with them. The line is written as the row is inserted, in
// its transaction: a line whose event isn't in the table was rolled back
// (its request answered an error) or deleted since.
func insertAudited(ctx context.Context, db bun.IDB, row *eventRecord) error {
	var actor, user, workspace sql.NullString
	err := db.NewInsert().Model(row).
		Returning("id, public_id, created_at, "+
			"(SELECT u.public_id FROM users AS u WHERE u.id = actor_id), "+
			"(SELECT u.public_id FROM users AS u WHERE u.id = user_id), "+
			"(SELECT w.public_id FROM workspaces AS w WHERE w.id = workspace_id)").
		Scan(ctx, &row.ID, &row.PublicID, &row.CreatedAt, &actor, &user, &workspace)
	if err != nil {
		return err
	}
	attrs := []any{
		"event", row.PublicID,
		"name", row.Name,
		"actor", actor.String,
		"user", user.String,
		"workspace", workspace.String,
		"ip", row.IP,
		"createdAt", row.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	// Outside a request's own context (a background worker recording for
	// one), the ID comes from the event's.
	if observe.RequestID(ctx) == "" && row.RequestID != "" {
		attrs = append(attrs, observe.RequestIDKey, row.RequestID)
	}
	slog.InfoContext(ctx, "audit", attrs...)
	return nil
}

// Record writes an event that has no data change of its own, such as a login
// attempt or a rate limit being reached.
func (s *Store) Record(ctx context.Context, e events.Event, scope EventScope) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return s.record(ctx, tx, e, scope)
	})
}
