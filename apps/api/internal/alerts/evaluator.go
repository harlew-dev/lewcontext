// Package alerts turns monitoring signals into alerts
// (docs/api/monitoring.md#alerts, plan A5): each minute the server holding
// the alerts' lock (Runner) reads the signals the rules name
// (monitor.AlertRules) and opens, escalates and resolves alerts
// (Evaluator); listeners on the alert events email admins and post to a
// webhook (listeners.go), and the leader pings a heartbeat URL after each
// evaluation that worked, so something outside notices when it stops.
package alerts

import (
	"context"
	"errors"
	"fmt"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"time"
)

// Evaluation bounds: each signal is read under the Monitoring pages' part
// budget, each page's signals under a page's deadline (docs/api/monitoring.md),
// so a slow query hides its own signal for that minute, never the rest.
const (
	SignalBudget = 2 * time.Second
	PageDeadline = 5 * time.Second
)

// Hysteresis is how long a signal must be back at ok before its alert
// resolves (decision 3), so a signal hovering at its threshold doesn't
// open and close an alert every minute.
const Hysteresis = 10 * time.Minute

// HourlyEvery is how often the hourly rules (the integrity checks) are read.
const HourlyEvery = time.Hour

// Evaluator decides, each minute, what changed. It remembers, per signal,
// since when it has been at a level (for its rule's For) and since when
// back at ok (for Hysteresis): only on the server evaluating, so when the
// lock moves the new leader starts both again, which delays an alert,
// never doubles one (the store keeps one open alert per signal).
type Evaluator struct {
	Store  *store.Store
	Limits []ratelimit.Limit
	// Rules are the rules evaluated: monitor.AlertRules.
	Rules []monitor.AlertRule
	// Signals finds a rule's signal: monitor.FindSignal when nil (tests
	// give their own).
	Signals func(name string) (monitor.Signal, bool)
	// Budget and Deadline: SignalBudget and PageDeadline when zero.
	Budget, Deadline time.Duration

	since      map[string]time.Time
	okSince    map[string]time.Time
	lastHourly time.Time
}

// Evaluation is what one evaluation did.
type Evaluation struct {
	Read, Unavailable           int
	Opened, Escalated, Resolved int
	Reminded, Digested          int
	Hourly                      bool
}

// Reset forgets what the evaluator remembered: a server that lost the lock
// starts afresh if it takes it again.
func (e *Evaluator) Reset() {
	e.since, e.okSince, e.lastHourly = nil, nil, time.Time{}
}

// Evaluate reads the rules due at now (every minute rule, and the hourly
// ones once an hour) and applies what changed, then sends the reminders
// and the digest due. An error means something couldn't be read or
// written (a signal that only ran out of time isn't one): no heartbeat.
func (e *Evaluator) Evaluate(ctx context.Context, now time.Time) (Evaluation, error) {
	if e.since == nil {
		e.since, e.okSince = map[string]time.Time{}, map[string]time.Time{}
	}
	var out Evaluation
	open, err := e.Store.IndexOpenAlerts(ctx)
	if err != nil {
		return out, err
	}
	byID := map[string]store.Alert{}
	for _, a := range open {
		byID[a.Signal] = a
	}
	out.Hourly = e.lastHourly.IsZero() || now.Sub(e.lastHourly) >= HourlyEvery
	env := monitor.Env{Store: e.Store, Limits: e.Limits, Now: now}.WithChecks()
	readings, err := e.read(ctx, env, out.Hourly)
	if err != nil {
		return out, err
	}
	for _, r := range e.Rules {
		if r.Hourly && !out.Hourly {
			continue
		}
		reading, ok := readings[r.Signal]
		if !ok || reading.Unavailable {
			out.Unavailable++
			continue
		}
		out.Read++
		alert, isOpen := byID[r.Signal]
		var current *store.Alert
		if isOpen {
			current = &alert
		}
		if err := e.apply(ctx, env, r, reading, current, now, &out); err != nil {
			return out, err
		}
	}
	if out.Hourly {
		e.lastHourly = now
	}
	if out.Reminded, err = e.Store.RemindAlerts(ctx, now); err != nil {
		return out, err
	}
	out.Digested, err = e.Store.DigestAlerts(ctx, now)
	return out, err
}

// read reads the signals of the rules due, page by page, each page under
// the deadline and each signal under the budget. A signal past its budget
// is unavailable; any other failure is the evaluation's.
func (e *Evaluator) read(ctx context.Context, env monitor.Env, hourly bool) (map[string]monitor.Result, error) {
	budget, deadline := e.Budget, e.Deadline
	if budget == 0 {
		budget = SignalBudget
	}
	if deadline == 0 {
		deadline = PageDeadline
	}
	var pages []string
	byPage := map[string][]monitor.Signal{}
	for _, r := range e.Rules {
		if r.Hourly && !hourly {
			continue
		}
		find := e.Signals
		if find == nil {
			find = monitor.FindSignal
		}
		sig, ok := find(r.Signal)
		if !ok {
			return nil, fmt.Errorf("alert rule %s: no such signal", r.Signal)
		}
		if _, seen := byPage[sig.Page]; !seen {
			pages = append(pages, sig.Page)
		}
		byPage[sig.Page] = append(byPage[sig.Page], sig)
	}
	out := map[string]monitor.Result{}
	for _, page := range pages {
		if err := readPage(ctx, env, byPage[page], budget, deadline, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// readPage reads one page's signals into out.
func readPage(ctx context.Context, env monitor.Env, signals []monitor.Signal, budget, deadline time.Duration, out map[string]monitor.Result) error {
	pageCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	for _, sig := range signals {
		sigCtx, cancelSig := context.WithTimeout(pageCtx, budget)
		r, err := monitor.Read(sigCtx, env, sig)
		timedOut := sigCtx.Err() != nil
		cancelSig()
		switch {
		case err == nil:
			out[sig.Name] = r
		case ctx.Err() != nil:
			return ctx.Err()
		case timedOut:
			out[sig.Name] = monitor.Result{Name: sig.Name, Unavailable: true}
		default:
			return err
		}
	}
	return nil
}

// apply moves one rule's alert on from what its signal reads now.
func (e *Evaluator) apply(ctx context.Context, env monitor.Env, r monitor.AlertRule, reading monitor.Result, open *store.Alert, now time.Time, out *Evaluation) error {
	level := r.Level(reading.Level)
	if level == monitor.LevelOK {
		delete(e.since, r.Signal)
		if open == nil {
			delete(e.okSince, r.Signal)
			return nil
		}
		if _, ok := e.okSince[r.Signal]; !ok {
			e.okSince[r.Signal] = now
		}
		if !r.Hourly && now.Sub(e.okSince[r.Signal]) < Hysteresis {
			return e.Store.UpdateAlertValue(ctx, open.ID, reading.Value)
		}
		resolved, err := e.Store.ResolveAlert(ctx, open.ID, now)
		if resolved {
			out.Resolved++
			delete(e.okSince, r.Signal)
		}
		return err
	}
	delete(e.okSince, r.Signal)
	if open != nil {
		if level == monitor.LevelCritical && open.Level == string(monitor.LevelWarn) {
			escalated, err := e.Store.EscalateAlert(ctx, open.ID, reading.Value, now)
			if escalated {
				out.Escalated++
			}
			return err
		}
		return e.Store.UpdateAlertValue(ctx, open.ID, reading.Value)
	}
	if _, ok := e.since[r.Signal]; !ok {
		e.since[r.Signal] = now
	}
	if now.Sub(e.since[r.Signal]) < r.For {
		return nil
	}
	_, err := e.Store.OpenAlert(ctx, store.NewAlert{Signal: r.Signal, Level: string(level), Value: reading.Value, At: now,
		Details: details(env, reading)})
	if errors.Is(err, store.ErrAlertOpen) {
		return nil
	}
	if err == nil {
		out.Opened++
		delete(e.since, r.Signal)
	}
	return err
}

// details are what an alert keeps for its page: the drill-down behind its
// signal, and an integrity check's sample IDs.
func details(env monitor.Env, reading monitor.Result) map[string]any {
	d := map[string]any{}
	if reading.Drill != nil {
		d["drill"] = reading.Drill
	}
	if samples := env.CheckSamples(reading.Name); len(samples) > 0 {
		d["samples"] = samples
	}
	return d
}
