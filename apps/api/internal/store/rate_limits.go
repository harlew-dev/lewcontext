package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"time"
)

type rateLimitRecord struct {
	bun.BaseModel `bun:"table:rate_limits,alias:l"`
	Key           string    `bun:",pk"`
	Hits          int       `bun:",notnull"`
	ResetsAt      time.Time `bun:",notnull"`
}

// CountHit adds a hit to key's counter in fixed windows of the given
// length, and returns the window's hits so far (this one included) and
// when it ends. Each request gets its own count, so the one that first
// passes a level is known exactly. Counters persist across restarts and
// multiple API processes.
func (s *Store) CountHit(ctx context.Context, key string, window time.Duration) (hits int, resetsAt time.Time, err error) {
	hit := rateLimitRecord{Key: key, Hits: 1}
	seconds := int(window.Seconds())
	err = s.DB.NewInsert().Model(&hit).
		Value("resets_at", "now() + make_interval(secs => ?)", seconds).
		On("CONFLICT (key) DO UPDATE").
		Set("hits = CASE WHEN l.resets_at <= now() THEN 1 ELSE l.hits + 1 END").
		Set("resets_at = CASE WHEN l.resets_at <= now() THEN now() + make_interval(secs => ?) ELSE l.resets_at END", seconds).
		Returning("hits, resets_at").
		Scan(ctx)
	return hit.Hits, hit.ResetsAt, err
}

// CountHitUntil adds a hit to key's counter in the window that ends at
// resetsAt, another counter's (CountHit's), so the two windows end
// together, and returns its hits so far and when the window it was counted
// in ends. A counter left from an earlier window starts again at 1; a late
// hit from an earlier window counts in the later one, and gets that
// window's end.
func (s *Store) CountHitUntil(ctx context.Context, key string, resetsAt time.Time) (hits int, counted time.Time, err error) {
	hit := rateLimitRecord{Key: key, Hits: 1, ResetsAt: resetsAt}
	err = s.DB.NewInsert().Model(&hit).
		On("CONFLICT (key) DO UPDATE").
		Set("hits = CASE WHEN l.resets_at < EXCLUDED.resets_at THEN 1 ELSE l.hits + 1 END").
		Set("resets_at = GREATEST(l.resets_at, EXCLUDED.resets_at)").
		Returning("hits, resets_at").
		Scan(ctx)
	return hit.Hits, hit.ResetsAt, err
}

// PeekHits is key's hits in the window that ends at resetsAt (or a later
// one), without counting a hit: 0 when there's no counter, or only an
// earlier window's.
func (s *Store) PeekHits(ctx context.Context, key string, resetsAt time.Time) (int, error) {
	var hits int
	err := s.DB.NewSelect().Model((*rateLimitRecord)(nil)).Column("hits").
		Where("key = ?", key).
		Where("resets_at >= ?", resetsAt).
		Scan(ctx, &hits)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return hits, err
}
