package alerts

import (
	"context"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/observe"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
)

// Interval is how often alerts are evaluated.
const Interval = time.Minute

// releaseTimeout bounds handing the lock back at shutdown.
const releaseTimeout = 2 * time.Second

// Runner is the alert loop every server runs (decision 1): each minute it
// takes, or checks it still holds, the alerts' session lock
// (database.TryLeadAlerts); only the holder evaluates, then pings the
// heartbeat. The others stand by and try again the next minute, so when
// the leader stops (its lock released at shutdown) or dies (its connection
// closes, and PostgreSQL releases the lock), another takes over within a
// minute.
type Runner struct {
	DB        *bun.DB
	Evaluator *Evaluator
	// Heartbeat is pinged after each evaluation that worked; nil: none.
	Heartbeat *Heartbeat
	// Now is the clock: time.Now when nil.
	Now func() time.Time

	leader *database.Leader
}

// Run evaluates every Interval until ctx ends, then hands the lock back.
func (r *Runner) Run(ctx context.Context, every time.Duration) {
	defer r.Release()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Leading reports whether this server holds the alerts' lock.
func (r *Runner) Leading() bool {
	return r.leader != nil
}

// Tick is one turn: lead or stand by, evaluate, ping. It reports whether
// this server evaluated and whether that worked.
func (r *Runner) Tick(ctx context.Context) (evaluated bool, err error) {
	if !r.lead(ctx) {
		return false, nil
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	turn, cancel := context.WithTimeout(ctx, Interval/2)
	defer cancel()
	result, err := r.Evaluator.Evaluate(turn, now())
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "evaluating alerts failed", observe.ErrorAttr(err))
			observe.Report(ctx, observe.Problem{Source: observe.SourceBackground, Route: "alerts", Err: err})
		}
		return true, err
	}
	if result.Opened+result.Escalated+result.Resolved+result.Reminded+result.Digested > 0 || result.Unavailable > 0 {
		slog.InfoContext(ctx, "alerts evaluated", "opened", result.Opened, "escalated", result.Escalated, "resolved", result.Resolved,
			"reminded", result.Reminded, "digested", result.Digested, "unavailable", result.Unavailable)
	}
	if r.Heartbeat != nil {
		if err := r.Heartbeat.Ping(turn); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "the alerts' heartbeat failed", observe.ErrorAttr(err))
		}
	}
	return true, nil
}

// lead takes the lock, or checks it's still held: a lost lock (its
// connection broke) is let go, and the evaluator's memory with it.
func (r *Runner) lead(ctx context.Context) bool {
	if r.leader != nil {
		if err := r.leader.Check(ctx); err == nil {
			return true
		} else if ctx.Err() == nil {
			slog.WarnContext(ctx, "lost the alerts' lock; standing by", observe.ErrorAttr(err))
		}
		r.Release()
	}
	leader, err := database.TryLeadAlerts(ctx, r.DB)
	if err != nil {
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "taking the alerts' lock failed", observe.ErrorAttr(err))
		}
		return false
	}
	if leader == nil {
		return false
	}
	slog.InfoContext(ctx, "this server evaluates alerts")
	r.leader = leader
	r.Evaluator.Reset()
	return true
}

// Release hands the lock back, if held.
func (r *Runner) Release() {
	if r.leader == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	r.leader.Release(ctx)
	r.leader = nil
	r.Evaluator.Reset()
}
