package monitor

import (
	"context"
	"encoding/json"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"sync"
	"time"
)

// The Integrity page (docs/api/monitoring.md#integrity): is anything true
// that should never be? Any duplicates from retries? Each check is a
// signal (alerts will run the same definitions every hour): its value is
// how many rows break it, at its level from the first. The page lists the
// rows behind each by public ID (store.CheckResult's samples).

// PageIntegrity is the Integrity page's name.
const PageIntegrity = "integrity"

// checkNow is the window of a check of how things stand now (an expired
// row, an overdue delivery): a signal needs one, and none is a page
// window, so it has no drill-down.
const checkNow = time.Minute

// releaseGrace is how long a release may run with servers out of step
// (versions or migrations) before it's a warning: a release stopped
// halfway.
const releaseGrace = 30 * time.Minute

// integrityCheck is one check: a signal's name, label and level (Warn or
// Critical at 1), its window, and how it's run.
type integrityCheck struct {
	name, label string
	level       Level
	window      time.Duration
	run         func(ctx context.Context, env Env, sp Span) (store.CheckResult, error)
}

// integrityChecks are the checks, in the page's order.
var integrityChecks = []integrityCheck{
	{"integrity.ownerless_workspaces", "Workspaces without an owner", LevelCritical, 24 * time.Hour,
		func(ctx context.Context, env Env, sp Span) (store.CheckResult, error) {
			return env.Store.CountOwnerlessWorkspaces(ctx, sp.From, sp.To)
		}},
	{"integrity.workspaces_over_quota", "Workspaces over a quota", LevelCritical, 24 * time.Hour,
		func(ctx context.Context, env Env, sp Span) (store.CheckResult, error) {
			return env.Store.CountWorkspacesOverQuota(ctx, sp.From, sp.To)
		}},
	{"integrity.member_invitations", "Live invitations to members", LevelWarn, 24 * time.Hour,
		func(ctx context.Context, env Env, sp Span) (store.CheckResult, error) {
			return env.Store.CountMemberInvitations(ctx, sp.From, sp.To)
		}},
	{"integrity.admins_without_mfa", "Admins without two-factor", LevelCritical, checkNow,
		func(ctx context.Context, env Env, _ Span) (store.CheckResult, error) {
			return env.Store.CountAdminsWithoutMFA(ctx)
		}},
	{"integrity.expired_rows", "Expired rows not cleaned up", LevelWarn, checkNow,
		func(ctx context.Context, env Env, _ Span) (store.CheckResult, error) {
			return env.Store.CountExpiredRows(ctx)
		}},
	{"integrity.ended_limit_windows", "Ended limit windows not cleaned up", LevelWarn, checkNow,
		func(ctx context.Context, env Env, _ Span) (store.CheckResult, error) {
			return env.Store.CountEndedLimitWindows(ctx)
		}},
	{"integrity.overdue_deliveries", "Deliveries overdue", LevelCritical, checkNow,
		func(ctx context.Context, env Env, _ Span) (store.CheckResult, error) {
			return env.Store.CountOverdueDeliveries(ctx)
		}},
	{"integrity.unmatched_deliveries", "Deliveries with no listener", LevelWarn, 7 * 24 * time.Hour,
		func(ctx context.Context, env Env, sp Span) (store.CheckResult, error) {
			return env.Store.CountUnmatchedDeliveries(ctx, sp.From, sp.To)
		}},
	{"integrity.unknown_events", "Events outside the catalogue", LevelWarn, 24 * time.Hour,
		func(ctx context.Context, env Env, sp Span) (store.CheckResult, error) {
			return env.Store.CountUnknownEventNames(ctx, sp.From, sp.To, events.Names())
		}},
	{"integrity.servers_out_of_step", "Servers out of step", LevelWarn, checkNow,
		func(ctx context.Context, env Env, _ Span) (store.CheckResult, error) {
			servers, err := env.Store.IndexServerInstances(ctx)
			return serversOutOfStep(servers, env.Now), err
		}},
}

// IntegritySignals are the Integrity page's signals: how many checks fail
// at each level (the Overview's tile), then each check.
var IntegritySignals = integritySignals()

// The two summaries' names.
const (
	integrityCritical = "integrity.critical_checks"
	integrityWarning  = "integrity.warning_checks"
)

func integritySignals() []Signal {
	signals := []Signal{
		failingChecks(integrityCritical, "Critical checks failing", LevelCritical),
		failingChecks(integrityWarning, "Warning checks failing", LevelWarn),
	}
	for _, c := range integrityChecks {
		sig := Signal{Name: c.name, Page: PageIntegrity, Label: c.label, Unit: UnitCount, Window: c.window,
			Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
				r, err := env.check(ctx, c, sp)
				return count(r.Count, err)
			}}
		if c.level == LevelCritical {
			sig.Critical = 1
		} else {
			sig.Warn = 1
		}
		signals = append(signals, sig)
	}
	return signals
}

// failingChecks is a signal counting the checks at level that fail.
func failingChecks(name, label string, level Level) Signal {
	sig := Signal{Name: name, Page: PageIntegrity, Label: label, Unit: UnitCount, Window: checkNow,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			var failing int64
			for _, c := range integrityChecks {
				if c.level != level {
					continue
				}
				r, err := env.check(ctx, c, checkSpan(c, env.Now))
				if err != nil {
					return Reading{}, err
				}
				if r.Count > 0 {
					failing++
				}
			}
			return count(failing, nil)
		}}
	if level == LevelCritical {
		sig.Critical = 1
	} else {
		sig.Warn = 1
	}
	return sig
}

// checkSpan is the span a check counts at now, as its signal's.
func checkSpan(c integrityCheck, now time.Time) Span {
	return Signal{Window: c.window}.SpanAt(now)
}

// checkMemo keeps the checks run while a page is read, so a check is run
// once whether its signal, the summary or the page's list asks first.
type checkMemo struct {
	mu      sync.Mutex
	results map[string]store.CheckResult
}

// check runs c over sp, or answers it from the page's memo.
func (env Env) check(ctx context.Context, c integrityCheck, sp Span) (store.CheckResult, error) {
	if env.checks != nil {
		env.checks.mu.Lock()
		r, ok := env.checks.results[c.name]
		env.checks.mu.Unlock()
		if ok {
			return r, nil
		}
	}
	r, err := c.run(ctx, env, sp)
	if err != nil {
		return store.CheckResult{}, err
	}
	if env.checks != nil {
		env.checks.mu.Lock()
		env.checks.results[c.name] = r
		env.checks.mu.Unlock()
	}
	return r, nil
}

// withMemo is env with a memo of its own for one page.
func (env Env) withMemo() Env {
	env.checks = &checkMemo{results: map[string]store.CheckResult{}}
	return env
}

// serversOutOfStep finds the live servers out of step, once the newest
// has run releaseGrace: on another version than it, or with migrations
// the database hasn't applied (a server built ahead of its database).
func serversOutOfStep(servers []store.ServerInstance, now time.Time) store.CheckResult {
	live := liveServers(servers, now)
	var newest *store.ServerInstance
	for i := range live {
		if newest == nil || live[i].StartedAt.After(newest.StartedAt) {
			newest = &live[i]
		}
	}
	var ids []string
	for _, si := range live {
		behind := si.Version != newest.Version && now.Sub(newest.StartedAt) >= releaseGrace
		summary := configOf(si)
		pending := summary.Migrations.Applied < summary.Migrations.Embedded && now.Sub(si.StartedAt) >= releaseGrace
		if behind || pending {
			ids = append(ids, si.ID)
		}
	}
	return checkSamples(ids)
}

// checkSamples is a check's result from its rows' IDs.
func checkSamples(ids []string) store.CheckResult {
	out := store.CheckResult{Count: int64(len(ids)), Samples: []string{}}
	for _, id := range ids {
		if len(out.Samples) < store.MaxCheckSamples {
			out.Samples = append(out.Samples, id)
		}
	}
	return out
}

// liveServers are the servers heard from in the last ServerLiveFor.
func liveServers(servers []store.ServerInstance, now time.Time) []store.ServerInstance {
	var live []store.ServerInstance
	for _, si := range servers {
		if si.LastSeenAt.After(store.ServerLiveSince(now)) {
			live = append(live, si)
		}
	}
	return live
}

// configOf is a server's configuration summary as it wrote it: only the
// fields config.Summary has (anything else in the column is ignored), and
// zero values for a summary it can't read.
func configOf(si store.ServerInstance) config.Summary {
	var s config.Summary
	_ = json.Unmarshal(si.Config, &s)
	return s
}

// CheckRows are a check's rows as the page names them.
type CheckRows struct {
	Name    string   `json:"name"`
	Capped  bool     `json:"capped,omitempty"`
	Samples []string `json:"samples"`
}

// integritySections are the Integrity page's lists: each check's rows,
// then duplicates and retries over the page's window.
type integritySections struct {
	Checks            []CheckRows        `json:"checks"`
	Duplicates        store.Duplicates   `json:"duplicates"`
	DuplicateSends    store.CheckResult  `json:"duplicateSends"`
	RetriedDeliveries store.CheckResult  `json:"retriedDeliveries"`
	TimedOutWrites    []store.RouteCount `json:"timedOutWrites"`
}

// integrityPage reads each check's rows (a part each, named by the check,
// most already run by its signal) and the duplicates over w.
func integrityPage(ctx context.Context, env Env, w Window) (Sections, error) {
	none := store.CheckResult{Samples: []string{}}
	out := &integritySections{
		Checks:     make([]CheckRows, len(integrityChecks)),
		Duplicates: store.Duplicates{CheckResult: none, ByName: []store.NameCount{}}, DuplicateSends: none, RetriedDeliveries: none,
		TimedOutWrites: []store.RouteCount{},
	}
	parts := make([]section, 0, len(integrityChecks)+4)
	for i, c := range integrityChecks {
		out.Checks[i] = CheckRows{Name: c.name, Samples: []string{}}
		parts = append(parts, section{c.name, func(ctx context.Context) error {
			r, err := env.check(ctx, c, checkSpan(c, env.Now))
			if err == nil {
				out.Checks[i] = CheckRows{Name: c.name, Capped: r.Capped, Samples: r.Samples}
			}
			return err
		}})
	}
	s := env.Store
	parts = append(parts,
		section{"duplicates", func(ctx context.Context) (err error) {
			out.Duplicates, err = s.CountDuplicateCreates(ctx, w.From, w.To)
			return
		}},
		section{"duplicateSends", func(ctx context.Context) (err error) {
			out.DuplicateSends, err = s.CountDuplicateSends(ctx, w.From, w.To)
			return
		}},
		section{"retriedDeliveries", func(ctx context.Context) (err error) {
			out.RetriedDeliveries, err = s.CountRetriedDeliveries(ctx, w.From, w.To)
			return
		}},
		section{"timedOutWrites", func(ctx context.Context) (err error) {
			out.TimedOutWrites, err = s.IndexTimedOutWrites(ctx, w.From, w.To)
			return
		}},
	)
	return readSections(ctx, env, out, parts)
}
