package monitor

import (
	"context"
	"database/sql"
	"errors"
	"lewcontext/api/internal/store"
	"regexp"
	"time"
)

// The Errors page (docs/api/monitoring.md#errors): what's failing, since
// when, and did the last release cause it? The grouped errors (app_errors)
// seen in the window, those new since the current release marked, and
// their occurrences per bucket (errors_total). One group's page has its
// stack. Read-only: resolving a group is an admin action (A6).

// PageErrors is the Errors page's name.
const PageErrors = "errors"

// releaseLookback is how far back the current release's first start is
// looked for: the windows' longest.
const releaseLookback = 7 * 24 * time.Hour

// ErrorsSignals are the Errors page's signals.
var ErrorsSignals = []Signal{
	{
		Name: "errors.new", Page: PageErrors, Label: "New error groups", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			g, err := env.Store.CountErrorGroups(ctx, sp.From)
			return count(g.New, err)
		},
	},
	{
		Name: "errors.new_panics", Page: PageErrors, Label: "New panics", Unit: UnitCount,
		Window: 24 * time.Hour, Critical: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			g, err := env.Store.CountErrorGroups(ctx, sp.From)
			return count(g.NewPanics, err)
		},
	},
	{
		// A browser's group isn't counted: anyone signed in can report one.
		Name: "errors.new_frequent", Page: PageErrors, Label: "New errors happening often", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			g, err := env.Store.CountErrorGroups(ctx, sp.From)
			return count(g.NewFrequent, err)
		},
	},
	{
		Name: "errors.open", Page: PageErrors, Label: "Open error groups", Unit: UnitCount,
		Window: 24 * time.Hour,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			g, err := env.Store.CountErrorGroups(ctx, sp.From)
			return count(g.Open, err)
		},
	},
}

// errorsSections are the Errors page's lists.
type errorsSections struct {
	// Release is the version the newest server started on, and when the
	// first started on it: a group first seen since is new in it. Null
	// when no server started in the last 7 days.
	Release     *store.Release      `json:"release"`
	Groups      []store.ErrorGroup  `json:"groups"`
	Occurrences store.MonitorSeries `json:"occurrences"`
}

// errorsPage reads the groups seen in w, the release, and the
// occurrences per bucket by source.
func errorsPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &errorsSections{Groups: []store.ErrorGroup{}}
	read, err := readSections(ctx, env, out, []section{
		{"groups", func(ctx context.Context) (err error) {
			out.Groups, err = s.IndexErrorGroups(ctx, w.From, w.To)
			return
		}},
		{"groups", func(ctx context.Context) (err error) {
			out.Release, err = currentRelease(ctx, env)
			return
		}},
		{"occurrences", func(ctx context.Context) (err error) {
			out.Occurrences, err = s.SummarizeMetric(ctx, "errors_total", "source", nil, w.SummaryWindow)
			return
		}},
	})
	for i := range out.Groups {
		out.Groups[i].New = newInRelease(out.Groups[i], out.Release)
	}
	return read, err
}

// newInRelease reports whether g was first seen since release started:
// did the release break it?
func newInRelease(g store.ErrorGroup, release *store.Release) bool {
	return release != nil && !g.FirstSeenAt.Before(release.StartedAt)
}

// currentRelease is the release now running, or nil when no server
// started within releaseLookback.
func currentRelease(ctx context.Context, env Env) (*store.Release, error) {
	r, err := env.Store.FindCurrentRelease(ctx, env.Now.Add(-releaseLookback))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// errorShape is a group's public ID: err_ and 14 base62.
var errorShape = regexp.MustCompile(`^err_[0-9A-Za-z]{14}$`)

// ErrNoErrorGroup: no group has that ID.
var ErrNoErrorGroup = errors.New("monitoring: no such error group")

// ErrorView is one group's page: the group with its stack, the release
// now running, and its route's occurrences per bucket of the window
// (errors_total counts by source and route, so every group on the route).
type ErrorView struct {
	Window      WindowEcho             `json:"window"`
	Group       store.ErrorGroupDetail `json:"group"`
	Release     *store.Release         `json:"release"`
	Occurrences store.MonitorSeries    `json:"occurrences"`
}

// ShowError reads one group over w; ErrNoErrorGroup for an ID of another
// shape or none.
func ShowError(ctx context.Context, env Env, id string, w Window) (ErrorView, error) {
	if !errorShape.MatchString(id) {
		return ErrorView{}, ErrNoErrorGroup
	}
	group, err := env.Store.FindErrorGroup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrorView{}, ErrNoErrorGroup
	}
	if err != nil {
		return ErrorView{}, err
	}
	out := ErrorView{Window: w.Echo(), Group: group}
	if out.Release, err = currentRelease(ctx, env); err != nil {
		return ErrorView{}, err
	}
	out.Group.New = newInRelease(out.Group.ErrorGroup, out.Release)
	match := map[string]string{"source": group.Source, "route": group.Route}
	out.Occurrences, err = env.Store.SummarizeMetric(ctx, "errors_total", "source", match, w.SummaryWindow)
	return out, err
}
