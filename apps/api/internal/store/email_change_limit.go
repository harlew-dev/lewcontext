package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// EmailChangeCompletionLimit is the HTTP layer's configured completion budget.
// A zero Maximum disables it for RATE_LIMITS=off. Reservation uses the same
// transaction/connection as the address change, never a nested pool checkout.
type EmailChangeCompletionLimit struct {
	Name, Key string
	Maximum   int
	Window    time.Duration
}

type emailChangeLimited struct {
	Hits     int
	ResetsAt time.Time
}

func (*emailChangeLimited) Error() string { return "email-change completion limit reached" }

func reserveEmailChangeCompletion(ctx context.Context, tx bun.Tx, limit EmailChangeCompletionLimit) error {
	if limit.Maximum == 0 {
		return nil
	}
	hits, resets, err := countHit(ctx, tx, limit.Key, limit.Window)
	if err != nil {
		return err
	}
	if hits > limit.Maximum {
		return &emailChangeLimited{Hits: hits, ResetsAt: resets}
	}
	return nil
}

// recordEmailChangeLimit retains one event per refused window. In direct mode
// the reservation is retried after the factor savepoint has rolled back, so
// the budget/refusal persists while recovery-code consumption does not.
func (s *Store) recordEmailChangeLimit(ctx context.Context, tx bun.Tx, user User, limit EmailChangeCompletionLimit, refused *emailChangeLimited) error {
	if refused.Hits != limit.Maximum+1 {
		return nil
	}
	e := events.RateLimitReached{Limit: limit.Name + ":block", User: user.PublicID, Window: int(limit.Window.Seconds()), ResetsAt: refused.ResetsAt}
	return s.record(ctx, tx, e, EventScope{UserID: user.ID})
}
