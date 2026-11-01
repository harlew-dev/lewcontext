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
