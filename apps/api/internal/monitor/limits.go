package monitor

import (
	"context"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/store"
	"strings"
	"time"
)

// The Rate limits page (docs/api/monitoring.md#rate-limits): which limits
// are being hit, by whom, and is any shared limit shutting everyone out?

// PageLimits is the Rate limits page's name.
const PageLimits = "limits"

// Budgets: the limits every signed-in request counts against. Reaching
// one is a script, a scraper or a runaway browser tab.
var budgetLimits = []string{"api:per-user", "api-read:per-user"}

// LimitsSignals are the Rate limits page's signals.
var LimitsSignals = []Signal{
	{
		// A shared limit reached is a feature switched off for everyone.
		Name: "limits.shared_reached", Page: PageLimits, Label: "Shared limits reached", Unit: UnitCount,
		Window: time.Hour, Critical: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(windowsReached(ctx, env, sp, globalLimits(env)...))
		},
		Drill: drill("rate_limit.reached"),
	},
	{
		Name: "limits.windows_reached", Page: PageLimits, Label: "Limit windows reached", Unit: UnitCount,
		Window: time.Hour,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountEvents(ctx, sp.From, sp.To, "rate_limit.reached"))
		},
		Drill:      drill("rate_limit.reached"),
		DrillExact: true,
	},
}

// limitsSections are the Rate limits page's charts and lists, over its
// window.
type limitsSections struct {
	Reached      store.MonitorSeries    `json:"reached"`
	ByLimit      []store.LimitReached   `json:"byLimit"`
	Refused      []store.MetricTotal    `json:"refused"`
	Shared       []SharedLimit          `json:"shared"`
	Blocked      []blockedLimit         `json:"blocked"`
	TopAccounts  []store.AccountWindows `json:"topAccounts"`
	TopAddresses []store.AddressCount   `json:"topAddresses"`
	Budgets      []store.AccountWindows `json:"budgets"`
	Quotas       []store.MetricTotal    `json:"quotas"`
	Routes       []store.RouteRefusals  `json:"routes"`
}

// blockedLimit is how many subjects a limit refuses right now. Subjects
// is null for a limit counted in each server's memory, which no query
// sees.
type blockedLimit struct {
	Name     string `json:"name"`
	Max      int    `json:"max"`
	Subjects *int64 `json:"subjects"`
}

// limitsPage reads the Rate limits page's sections over w, each on its
// own budget.
func limitsPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &limitsSections{}
	return readSections(ctx, env, out, []section{
		{"reached", func(ctx context.Context) (err error) {
			out.Reached, err = s.SummarizeLimitsReached(ctx, w.SummaryWindow)
			return
		}},
		{"byLimit", func(ctx context.Context) (err error) {
			out.ByLimit, err = s.IndexLimitsReached(ctx, w.From, w.To)
			return
		}},
		{"refused", func(ctx context.Context) (err error) {
			out.Refused, err = s.SummarizeMetricTotals(ctx, "rate_limit_refused_total", "limit", w.From, w.To)
			return
		}},
		{"shared", func(ctx context.Context) (err error) {
			out.Shared, err = sharedLimits(ctx, env, w, globalLimits(env))
			return
		}},
		{"blocked", func(ctx context.Context) (err error) { out.Blocked, err = blocked(ctx, env); return }},
		{"topAccounts", func(ctx context.Context) (err error) {
			out.TopAccounts, err = s.IndexTopAccountsReaching(ctx, w.From, w.To)
			return
		}},
		{"topAddresses", func(ctx context.Context) (err error) {
			out.TopAddresses, err = s.IndexTopAddressesReaching(ctx, w.From, w.To)
			return
		}},
		{"budgets", func(ctx context.Context) (err error) {
			out.Budgets, err = s.IndexTopAccountsReaching(ctx, w.From, w.To, budgetLimits...)
			return
		}},
		{"quotas", func(ctx context.Context) (err error) {
			out.Quotas, err = s.SummarizeMetricTotals(ctx, "quota_refused_total", "quota", w.From, w.To)
			return
		}},
		{"routes", func(ctx context.Context) (err error) {
			out.Routes, err = s.IndexRouteRefusals(ctx, w.From, w.To)
			return
		}},
	})
}

// blocked counts, per limit counted by subject, the subjects at it now.
func blocked(ctx context.Context, env Env) ([]blockedLimit, error) {
	max := map[string]int{}
	var limits []ratelimit.Limit
	for _, l := range env.Limits {
		if isGlobal(l) {
			continue
		}
		limits = append(limits, l)
		if !l.InMemory {
			max[l.Name] = l.Max
		}
	}
	at, err := env.Store.CountSubjectsAt(ctx, max)
	out := make([]blockedLimit, 0, len(limits))
	for _, l := range limits {
		b := blockedLimit{Name: l.Name, Max: l.Max}
		if !l.InMemory {
			n := at[l.Name]
			b.Subjects = &n
		}
		out = append(out, b)
	}
	return out, err
}

// isGlobal is a limit shared by everyone: counted by "global", its key is
// its bare name, so it has no subjects.
func isGlobal(l ratelimit.Limit) bool {
	return strings.HasSuffix(l.Name, ":global")
}

// limitNamed finds a declared limit.
func limitNamed(env Env, name string) (ratelimit.Limit, bool) {
	for _, l := range env.Limits {
		if l.Name == name {
			return l, true
		}
	}
	return ratelimit.Limit{}, false
}
