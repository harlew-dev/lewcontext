package monitor

import (
	"context"
	"lewcontext/api/internal/store"
	"time"
)

// The Usage page (docs/api/monitoring.md#usage): how many people use the
// app, and what do they use? From the data the app already has: last seen,
// sign-ups and the events of what people do. And the accounts not seen for
// two years (compliance decision 9), listed, never deleted automatically.

// PageUsage is the Usage page's name.
const PageUsage = "usage"

// Active accounts are counted over a day, a week and a month.
const (
	activeDay   = 24 * time.Hour
	activeWeek  = 7 * 24 * time.Hour
	activeMonth = 30 * 24 * time.Hour
)

// UsageSignals are the Usage page's signals. None has a level: they're
// product numbers, not problems.
var UsageSignals = []Signal{
	{
		Name: "usage.sign_ups", Page: PageUsage, Label: "Sign-ups", Unit: UnitCount,
		Window: 24 * time.Hour,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountEvents(ctx, sp.From, sp.To, "auth.registered"))
		},
		Drill:      drill("auth.registered"),
		DrillExact: true,
	},
	{
		Name: "usage.active_accounts", Page: PageUsage, Label: "Active accounts", Unit: UnitCount,
		Window: activeDay,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountActiveAccounts(ctx, sp.From))
		},
	},
	{
		Name: "usage.weekly_active_accounts", Page: PageUsage, Label: "Active accounts this week", Unit: UnitCount,
		Window: activeWeek,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountActiveAccounts(ctx, sp.From))
		},
	},
	{
		// Its window is how long unseen: the count is of now.
		Name: "usage.inactive_accounts", Page: PageUsage, Label: "Not seen for two years", Unit: UnitCount,
		Window: store.InactiveAfter,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			return count(env.Store.CountInactiveAccounts(ctx, env.Now.Add(-store.InactiveAfter)))
		},
	},
}

// featureNames are the features the Usage page charts, a series each (a
// chart stacks five at most).
var featureNames = []string{"project.created", "project.updated", "workspace.invitation.created", "workspace.invitation.accepted", "auth.mfa.enabled"}

// usageSections are the Usage page's numbers and charts.
type usageSections struct {
	Active struct {
		Day   int64 `json:"day"`
		Week  int64 `json:"week"`
		Month int64 `json:"month"`
		// WithMFA: of the week's, those with two-factor authentication on.
		WithMFA int64 `json:"withMfa"`
	} `json:"active"`
	SignUps    store.MonitorSeries `json:"signUps"`
	Activation struct {
		// Workspace: accounts registered 8 to 1 days ago that created or
		// joined a workspace within a day; Invited: those registered 14
		// to 7 days ago that invited someone within a week. Each cohort
		// is 7 days of sign-ups, every one given its whole time.
		Workspace store.Activation `json:"workspace"`
		Invited   store.Activation `json:"invited"`
	} `json:"activation"`
	Features store.MonitorSeries  `json:"features"`
	Sizes    store.WorkspaceSizes `json:"sizes"`
	Inactive struct {
		Before   time.Time               `json:"before"`
		Count    int64                   `json:"count"`
		Accounts []store.InactiveAccount `json:"accounts"`
	} `json:"inactive"`
}

// usagePage reads the Usage page over w: sign-ups, features and workspace
// sizes follow the window; active accounts, activation and inactive
// accounts are of now.
func usagePage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	now := env.Now
	out := &usageSections{}
	out.Inactive.Before = now.Add(-store.InactiveAfter)
	day := 24 * time.Hour
	return readSections(ctx, env, out, []section{
		{"active", func(ctx context.Context) (err error) {
			out.Active.Day, err = s.CountActiveAccounts(ctx, now.Add(-activeDay))
			return
		}},
		{"active", func(ctx context.Context) (err error) {
			out.Active.Week, err = s.CountActiveAccounts(ctx, now.Add(-activeWeek))
			return
		}},
		{"active", func(ctx context.Context) (err error) {
			out.Active.Month, err = s.CountActiveAccounts(ctx, now.Add(-activeMonth))
			return
		}},
		{"active", func(ctx context.Context) (err error) {
			out.Active.WithMFA, err = s.CountMFAAccounts(ctx, now.Add(-activeWeek))
			return
		}},
		{"signUps", func(ctx context.Context) (err error) {
			out.SignUps, err = s.SummarizeEventNames(ctx, w.SummaryWindow, "auth.registered")
			return
		}},
		{"activation", func(ctx context.Context) (err error) {
			out.Activation.Workspace, err = s.CountActivation(ctx, now.Add(-8*day), now.Add(-day), store.ActivatedWithin, "workspace.created", "workspace.invitation.accepted")
			return
		}},
		{"activation", func(ctx context.Context) (err error) {
			out.Activation.Invited, err = s.CountActivation(ctx, now.Add(-14*day), now.Add(-7*day), store.InvitedWithin, "workspace.invitation.created")
			return
		}},
		{"features", func(ctx context.Context) (err error) {
			out.Features, err = s.SummarizeEventNames(ctx, w.SummaryWindow, featureNames...)
			return
		}},
		{"sizes", func(ctx context.Context) (err error) {
			out.Sizes, err = s.CountWorkspaceSizes(ctx, w.From, w.To)
			return
		}},
		{"inactive", func(ctx context.Context) (err error) {
			out.Inactive.Count, err = s.CountInactiveAccounts(ctx, out.Inactive.Before)
			return
		}},
		{"inactive", func(ctx context.Context) (err error) {
			out.Inactive.Accounts, err = s.IndexInactiveAccounts(ctx, out.Inactive.Before)
			return
		}},
	})
}
