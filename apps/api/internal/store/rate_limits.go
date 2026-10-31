package store

import (
	"context"
	"github.com/uptrace/bun"
	"time"
)

type rateLimitRecord struct {
	bun.BaseModel `bun:"table:rate_limits,alias:l"`
	Key           string    `bun:",pk"`
	Hits          int       `bun:",notnull"`
	ResetsAt      time.Time `bun:",notnull"`
}

// Allow counts a hit against key's limit in fixed windows of the given
// length, and returns when the current window ends. Limits persist across
// restarts and multiple API processes; no proxy header is trusted.
// firstRefusal is true for the one request in a window that first goes over
// the limit, so callers can record it once however many follow.
func (s *Store) Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed, firstRefusal bool, resetsAt time.Time, err error) {
	hit := rateLimitRecord{Key: key, Hits: 1}
	seconds := int(window.Seconds())
	err = s.DB.NewInsert().Model(&hit).
		Value("resets_at", "now() + make_interval(secs => ?)", seconds).
		On("CONFLICT (key) DO UPDATE").
		Set("hits = CASE WHEN l.resets_at <= now() THEN 1 ELSE l.hits + 1 END").
		Set("resets_at = CASE WHEN l.resets_at <= now() THEN now() + make_interval(secs => ?) ELSE l.resets_at END", seconds).
		Returning("hits, resets_at").
		Scan(ctx)
	return hit.Hits <= limit, hit.Hits == limit+1, hit.ResetsAt, err
}
