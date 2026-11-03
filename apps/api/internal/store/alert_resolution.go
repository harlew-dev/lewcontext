package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
)

// AlertResolution names the exact recovered revision and selected comment.
type AlertResolution struct {
	Cycle    string
	Message  string
	Revision int64
}

// ResolveRecoveredAlert records an operator decision only after confirmed fresh
// recovery, with parent, cycle, message and revision checked under write locks.
func (s *Store) ResolveRecoveredAlert(ctx context.Context, publicID string, input AlertResolution, admin User) (Alert, error) {
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := lockOpenAlert(ctx, tx, publicID)
		if err != nil {
			return err
		}
		var at time.Time
		if err := tx.NewSelect().ColumnExpr("clock_timestamp()").Scan(ctx, &at); err != nil {
			return err
		}
		// ResolveIncidentCycle scopes both references before precondition checks,
		// so foreign references remain 404 even for an ineligible parent.
		cycle, err := ResolveIncidentCycle(ctx, tx, IncidentParent{AlertID: row.ID}, input.Cycle, input.Message, admin.ID, at)
		if err != nil {
			return err
		}
		if row.Revision != input.Revision || row.State != "recovered" || row.CurrentLevel != "ok" || row.RecoveredAt == nil || !row.AlertHealth.Fresh(at) {
			return ErrIncidentConflict
		}
		row.ResolvedAt, row.State = &at, "resolved"
		row.Revision++
		row.TransitionSequence++
		row.cyclePublicID = cycle.PublicID
		if err := saveAlertHealth(ctx, tx, &row); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model(&row).Column("resolved_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		mail, webhook, err := reserveAlertTransition(ctx, tx, row, at)
		if err != nil {
			return err
		}
		if err := s.record(ctx, tx, events.AdminAlertResolved{Admin: admin.PublicID, Alert: row.PublicID, Cycle: input.Cycle, Message: input.Message}, EventScope{ActorID: admin.ID}); err != nil {
			return err
		}
		return s.record(ctx, tx, events.AlertResolved{Alert: row.PublicID, Signal: row.Signal, Level: row.Level, Source: "admin", OpenFor: int(at.Sub(row.OpenedAt).Seconds()), Mail: mail, Webhook: webhook, Snapshot: alertSnapshot(row, at)}, EventScope{})
	})
	if err != nil {
		return Alert{}, err
	}
	return s.FindAlert(ctx, publicID)
}
