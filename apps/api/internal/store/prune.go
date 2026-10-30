package store

import (
	"context"
	"time"
)

// prunableEvents is the condition for an event the prune command may delete:
// created before the cutoff, with no delivery still waiting for a listener.
// Delivered and failed deliveries go with their event (ON DELETE CASCADE).
const prunableEvents = `e.created_at < ? AND NOT EXISTS (
	SELECT 1 FROM event_deliveries AS d
	WHERE d.event_id = e.id AND d.delivered_at IS NULL AND d.failed_at IS NULL)`

// CountOldEvents counts the events created before cutoff: those that can be
// pruned, and those kept because a delivery is still pending.
func (s *Store) CountOldEvents(ctx context.Context, before time.Time) (prunable, kept int, err error) {
	err = s.DB.NewRaw(`
		SELECT count(*) FILTER (WHERE `+prunableEvents+`),
		       count(*) FILTER (WHERE NOT (`+prunableEvents+`))
		FROM events AS e
		WHERE e.created_at < ?`, before, before, before).Scan(ctx, &prunable, &kept)
	return prunable, kept, err
}

// PruneEvents deletes up to limit prunable events created before cutoff, in
// one short statement, and returns how many it deleted. SKIP LOCKED leaves
// rows another prune run holds rather than waiting for them.
func (s *Store) PruneEvents(ctx context.Context, before time.Time, limit int) (int, error) {
	result, err := s.DB.NewRaw(`
		DELETE FROM events WHERE id IN (
			SELECT e.id FROM events AS e
			WHERE `+prunableEvents+`
			ORDER BY e.id
			LIMIT ?
			FOR UPDATE SKIP LOCKED)`, before, limit).Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}
