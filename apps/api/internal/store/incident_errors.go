package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// ResolveErrorGroup closes exactly the submitted cycle and occurrence snapshot,
// selecting an existing nonredacted comment. Parent, cycle, event and projection
// commit together; a concurrent occurrence or decision yields a conflict.
func (s *Store) ResolveErrorGroup(ctx context.Context, publicID, cycle, message string, lastSeenAt time.Time, admin User) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row appErrorRecord
		if err := tx.NewSelect().Model(&row).Where("ae.public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		// References are checked before stale state, so foreign IDs stay 404.
		parent := IncidentParent{AppErrorID: row.ID}
		selected, err := findIncidentCycle(ctx, tx, parent, cycle, true)
		if err != nil {
			return err
		}
		var msg IncidentMessage
		if err := tx.NewSelect().Model(&msg).Where("im.cycle_id = ? AND im.public_id = ?", selected.ID, message).Scan(ctx); err != nil {
			return err
		}
		if !row.ResolvedAt.IsZero() || !row.LastSeenAt.Equal(lastSeenAt) {
			return ErrIncidentConflict
		}
		var at time.Time
		if err := tx.NewSelect().ColumnExpr("clock_timestamp()").Scan(ctx, &at); err != nil {
			return err
		}
		if _, err := ResolveIncidentCycle(ctx, tx, parent, cycle, message, admin.ID, at); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model(&row).Set("resolved_at = ?", at).WherePK().Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, events.AdminErrorResolved{Admin: admin.PublicID, Error: publicID, Cycle: cycle, Message: message}, EventScope{ActorID: admin.ID})
	})
}
