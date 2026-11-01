package monitor

import (
	"context"
	"lewcontext/api/internal/store"
	"time"
)

// The Security page (docs/api/monitoring.md#security): is anyone
// attacking sign-in, second factors, password resets or the admin door?

// PageSecurity is the Security page's name.
const PageSecurity = "security"

// Shared limits the Security page watches.
const (
	registerGlobal      = "register:global"
	passwordResetGlobal = "password-reset:global"
)

// takeoverDrill is what IndexTakeovers looks at.
var takeoverDrill = drill("auth.password_changed,auth.mfa.disabled,auth.recovery_codes.created,auth.other_sessions_revoked")

// SecuritySignals are the Security page's signals, in its order.
var SecuritySignals = []Signal{
	{
		Name: "auth.failed_sign_ins", Page: PageSecurity, Label: "Failed sign-ins", Unit: UnitCount,
		Window: time.Hour, Warn: 100, Critical: 1000,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountFailedSignIns(ctx, sp.From, sp.To))
		},
		Drill:      drill("auth.login_attempted", "outcome", "failed"),
		DrillExact: true,
	},
	{
		Name: "auth.stuffing_addresses", Page: PageSecurity, Label: "Addresses trying many emails", Unit: UnitCount,
		Window: time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			addresses, _, err := env.Store.CountStuffing(ctx, sp.whole())
			return count(addresses, err)
		},
		Drill: drill("auth.login_attempted"),
	},
	{
		Name: "auth.stuffing_successes", Page: PageSecurity, Label: "…of them signing someone in", Unit: UnitCount,
		Window: time.Hour, Critical: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			_, succeeded, err := env.Store.CountStuffing(ctx, sp.whole())
			return count(succeeded, err)
		},
		Drill: drill("auth.login_attempted"),
	},
	{
		Name: "auth.distributed_guessing", Page: PageSecurity, Label: "Emails failing from many addresses", Unit: UnitCount,
		Window: time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountGuessing(ctx, sp.whole()))
		},
		Drill: drill("auth.login_attempted", "outcome", "failed"),
	},
	{
		Name: "auth.unknown_account_share", Page: PageSecurity, Label: "Attempts for unknown accounts", Unit: UnitPercent,
		Window: time.Hour, Warn: 50, MinSample: 50,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			s, err := env.Store.CountUnknownAccounts(ctx, sp.From, sp.To)
			return share(s.Unknown, s.Attempts), err
		},
		Drill:      drill("auth.login_attempted"),
		DrillExact: true,
	},
	{
		Name: "auth.success_after_failures", Page: PageSecurity, Label: "Sign-ins after 5+ failures", Unit: UnitCount,
		Window: time.Hour, Critical: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountSuccessAfterFailures(ctx, sp.whole()))
		},
		Drill: drill("auth.login_attempted"),
	},
	{
		Name: "auth.possible_takeovers", Page: PageSecurity, Label: "Possible takeovers", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountTakeovers(ctx, sp.From, sp.To))
		},
		Drill: takeoverDrill,
	},
	{
		Name: "auth.second_factor_failures", Page: PageSecurity, Label: "Accounts failing codes", Unit: UnitCount,
		Window: time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountFailedChecks(ctx, store.CheckSecondFactor, sp.whole()))
		},
		Drill: drill("auth.mfa.checked", "outcome", "failed"),
	},
	{
		Name: "auth.reauthentication_failures", Page: PageSecurity, Label: "Accounts failing password checks", Unit: UnitCount,
		Window: time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountFailedChecks(ctx, store.CheckPassword, sp.whole()))
		},
		Drill: drill("auth.password_checked", "outcome", "failed"),
	},
	{
		Name: "auth.reset_limit_reached", Page: PageSecurity, Label: "Password reset limit reached", Unit: UnitCount,
		Window: time.Hour, Critical: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(windowsReached(ctx, env, sp, passwordResetGlobal))
		},
		Drill: drill("rate_limit.reached"),
	},
	{
		Name: "auth.sign_up_limit_used", Page: PageSecurity, Label: "Sign-up limit used", Unit: UnitPercent,
		// Sized from register:global (1,000 an hour): half of it, and all.
		Window: time.Hour, Warn: 50, Critical: 100,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			return limitUsed(ctx, env, registerGlobal)
		},
		Drill: drill("auth.registration.requested,auth.registered"),
	},
	{
		Name: "admin.door_failures", Page: PageSecurity, Label: "Failed admin sign-ins", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			door, err := env.Store.CountDoor(ctx, sp.From, sp.To)
			return count(sum(door, store.DoorFailed...), err)
		},
		Drill: drill("admin.login_attempted", "outcome", "failed"),
	},
	{
		// Someone holds the key, and maybe a real password.
		Name: "admin.door_refusals", Page: PageSecurity, Label: "Admin door refusals", Unit: UnitCount,
		Window: 24 * time.Hour, Critical: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			door, err := env.Store.CountDoor(ctx, sp.From, sp.To)
			return count(sum(door, append([]string{"access_refused"}, store.DoorRefused...)...), err)
		},
		Drill: drill("admin.login_attempted,admin.access_refused", "outcome", "failed"),
	},
	{
		// An admin session used from another network than it signed in
		// from (admin.session.moved, once per session).
		Name: "admin.sessions_moved", Page: PageSecurity, Label: "Admin sessions that moved", Unit: UnitCount,
		Window: time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountEvents(ctx, sp.From, sp.To, "admin.session.moved"))
		},
		Drill: drill("admin.session.moved"), DrillExact: true,
	},
}

// sum adds the counts of keys.
func sum(counts map[string]int64, keys ...string) int64 {
	var n int64
	for _, k := range keys {
		n += counts[k]
	}
	return n
}

// windowsReached counts the windows of a limit reached in sp.
func windowsReached(ctx context.Context, env Env, sp Span, limits ...string) (int64, error) {
	if len(limits) == 0 {
		return 0, nil
	}
	rows, err := env.Store.IndexLimitsReached(ctx, sp.From, sp.To, limits...)
	var n int64
	for _, r := range rows {
		n += r.Windows
	}
	return n, err
}

// limitUsed reads how much of a shared limit's live window is used, as a
// percentage of its maximum, from its hits.
func limitUsed(ctx context.Context, env Env, name string) (Reading, error) {
	limit, ok := limitNamed(env, name)
	if !ok || limit.Max == 0 {
		return Reading{}, nil
	}
	states, err := env.Store.FindLimitStates(ctx, []string{name})
	hits := states[name].Hits
	return Reading{Value: float64(hits) * 100 / float64(limit.Max), Sample: hits}, err
}

// SharedLimit is a limit shared by everyone: its cap, its live window, and
// how often it was reached in a page's window. Reached, it's a feature
// switched off for everyone until the window ends.
type SharedLimit struct {
	Name          string     `json:"name"`
	Max           int        `json:"max"`
	WindowSeconds int        `json:"windowSeconds"`
	Hits          int64      `json:"hits"`
	ResetsAt      *time.Time `json:"resetsAt"`
	Reached       int64      `json:"reached"`
	LastReached   *time.Time `json:"lastReached"`
}

// sharedLimits reads the shared limits named, in that order.
func sharedLimits(ctx context.Context, env Env, w Window, names []string) ([]SharedLimit, error) {
	states, err := env.Store.FindLimitStates(ctx, names)
	if err != nil {
		return nil, err
	}
	reached, err := env.Store.IndexLimitsReached(ctx, w.From, w.To, names...)
	if err != nil {
		return nil, err
	}
	out := []SharedLimit{}
	for _, name := range names {
		limit, ok := limitNamed(env, name)
		if !ok {
			continue
		}
		s := SharedLimit{Name: name, Max: limit.Max, WindowSeconds: int(limit.Window.Seconds())}
		if state, ok := states[name]; ok {
			s.Hits, s.ResetsAt = state.Hits, &state.ResetsAt
		}
		for _, r := range reached {
			if r.Limit == name {
				s.Reached, s.LastReached = r.Windows, &r.LastReached
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// globalLimits are the limits shared by everyone, as declared.
func globalLimits(env Env) []string {
	var names []string
	for _, l := range env.Limits {
		if isGlobal(l) {
			names = append(names, l.Name)
		}
	}
	return names
}

// FailedChecks are an account review list's rows, for second factors and
// password checks.
type FailedChecks = []store.FailedChecksRow

// securitySections are the Security page's charts and review lists, over
// its window.
type securitySections struct {
	SignIns              store.MonitorSeries             `json:"signIns"`
	Stuffing             []store.StuffingRow             `json:"stuffing"`
	Guessing             []store.GuessingRow             `json:"guessing"`
	Unknown              store.SignInShare               `json:"unknown"`
	SuccessAfterFailures []store.SuccessAfterFailuresRow `json:"successAfterFailures"`
	Takeovers            []store.TakeoverRow             `json:"takeovers"`
	SecondFactor         secondFactor                    `json:"secondFactor"`
	Reauthentication     FailedChecks                    `json:"reauthentication"`
	Resets               flow                            `json:"resets"`
	SignUps              flow                            `json:"signUps"`
	AdminDoor            adminDoor                       `json:"adminDoor"`
	Sessions             sessions                        `json:"sessions"`
}

type secondFactor struct {
	Failures    FailedChecks      `json:"failures"`
	BackupCodes store.BackupCodes `json:"backupCodes"`
}

// flow is a request that sends mail or makes an account: asked for and
// done per bucket, the addresses asking most, and its shared limit.
type flow struct {
	Series       store.MonitorSeries  `json:"series"`
	TopAddresses []store.AddressCount `json:"topAddresses"`
	Limit        *SharedLimit         `json:"limit"`
}

// adminDoor's counts are a list of outcomes (never a map keyed by them:
// "wrong_password" as a key reads like a secret's field).
type adminDoor struct {
	Counts []OutcomeCount      `json:"counts"`
	Series store.MonitorSeries `json:"series"`
	Recent []store.DoorEvent   `json:"recent"`
}

type sessions struct {
	Series store.MonitorSeries `json:"series"`
	store.SessionCounts
}

// securityPage reads the Security page's sections over w, each on its
// own budget (its key in the sections names it).
func securityPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &securitySections{}
	return readSections(ctx, env, out, []section{
		{"signIns", func(ctx context.Context) (err error) {
			out.SignIns, err = s.SummarizeSignIns(ctx, w.SummaryWindow)
			return
		}},
		{"stuffing", func(ctx context.Context) (err error) { out.Stuffing, err = s.IndexStuffing(ctx, w.hourly()); return }},
		{"guessing", func(ctx context.Context) (err error) { out.Guessing, err = s.IndexGuessing(ctx, w.hourly()); return }},
		{"unknown", func(ctx context.Context) (err error) {
			out.Unknown, err = s.CountUnknownAccounts(ctx, w.From, w.To)
			return
		}},
		{"successAfterFailures", func(ctx context.Context) (err error) {
			out.SuccessAfterFailures, err = s.IndexSuccessAfterFailures(ctx, w.hourly())
			return
		}},
		{"takeovers", func(ctx context.Context) (err error) {
			out.Takeovers, err = s.IndexTakeovers(ctx, w.From, w.To)
			return
		}},
		{"secondFactor", func(ctx context.Context) (err error) {
			out.SecondFactor.Failures, err = s.IndexFailedChecks(ctx, store.CheckSecondFactor, w.hourly())
			return
		}},
		{"secondFactor", func(ctx context.Context) (err error) {
			out.SecondFactor.BackupCodes, err = s.CountBackupCodes(ctx, w.From, w.To)
			return
		}},
		{"reauthentication", func(ctx context.Context) (err error) {
			out.Reauthentication, err = s.IndexFailedChecks(ctx, store.CheckPassword, w.hourly())
			return
		}},
		{"resets", func(ctx context.Context) (err error) {
			out.Resets, err = readFlow(ctx, env, w, passwordResetGlobal, "auth.password_reset.requested", "auth.password_reset.sent", "auth.password_reset.requested")
			return
		}},
		{"signUps", func(ctx context.Context) (err error) {
			out.SignUps, err = readFlow(ctx, env, w, registerGlobal, "auth.registration.requested", "auth.registered", "auth.registered")
			return
		}},
		{"adminDoor", func(ctx context.Context) error {
			counts, err := s.CountDoor(ctx, w.From, w.To)
			out.AdminDoor.Counts = outcomes(counts, store.DoorOutcomes)
			return err
		}},
		{"adminDoor", func(ctx context.Context) (err error) {
			out.AdminDoor.Series, err = s.SummarizeDoor(ctx, w.SummaryWindow)
			return
		}},
		{"adminDoor", func(ctx context.Context) (err error) {
			out.AdminDoor.Recent, err = s.IndexDoorEvents(ctx, w.From, w.To)
			return
		}},
		{"sessions", func(ctx context.Context) (err error) {
			out.Sessions.Series, err = s.SummarizeEventNames(ctx, w.SummaryWindow, "auth.logged_in")
			return
		}},
		{"sessions", func(ctx context.Context) (err error) { out.Sessions.SessionCounts, err = s.CountSessions(ctx); return }},
	})
}

// readFlow reads a flow: asked for and done per bucket, the top addresses
// by the events named from (resets asked for; accounts made), and the
// shared limit on it.
func readFlow(ctx context.Context, env Env, w Window, limit, asked, done, from string) (flow, error) {
	var out flow
	var err error
	if out.Series, err = env.Store.SummarizeEventNames(ctx, w.SummaryWindow, asked, done); err != nil {
		return out, err
	}
	if out.TopAddresses, err = env.Store.IndexTopAddresses(ctx, from, w.From, w.To); err != nil {
		return out, err
	}
	limits, err := sharedLimits(ctx, env, w, []string{limit})
	if len(limits) == 1 {
		out.Limit = &limits[0]
	}
	return out, err
}

// OutcomeCount is how many times something ended one way.
type OutcomeCount struct {
	Outcome string `json:"outcome"`
	Count   int64  `json:"count"`
}

// outcomes lists counts in order, zeros included.
func outcomes(counts map[string]int64, order []string) []OutcomeCount {
	out := make([]OutcomeCount, len(order))
	for i, o := range order {
		out[i] = OutcomeCount{Outcome: o, Count: counts[o]}
	}
	return out
}
