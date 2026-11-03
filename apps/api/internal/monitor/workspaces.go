package monitor

import (
	"context"
	"lewcontext/api/internal/store"
	"time"
)

// The Workspaces page (docs/api/monitoring.md#workspaces): which
// workspaces are filling up, growing fast, or used to send spam? Rows are
// counted only for the workspaces with an event in the window (store's
// monitor_workspaces.go says why).

// PageWorkspaces is the Workspaces page's name.
const PageWorkspaces = "workspaces"

// LargeWorkspace: a workspace holding this many rows across every
// workspace table is a warning (the quotas keep today's tables far below
// it; a future table without one is what it watches for).
const LargeWorkspace = store.WorkspaceRowsCap

// WorkspacesSignals are the Workspaces page's signals.
var WorkspacesSignals = []Signal{
	{
		// A quota 80% full is a warning; full, it's refusing creates.
		Name: "workspaces.quota_used", Page: PageWorkspaces, Label: "Fullest quota", Unit: UnitPercent,
		Window: 24 * time.Hour, Warn: store.QuotaWarnShare, Critical: 100,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return fullestQuota(ctx, env, sp)
		},
	},
	{
		Name: "workspaces.largest", Page: PageWorkspaces, Label: "Most rows in a workspace", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: LargeWorkspace,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			rows, err := env.Store.IndexWorkspaceRows(ctx, sp.From, sp.To, 1)
			if err != nil || len(rows) == 0 {
				return Reading{}, err
			}
			return Reading{Value: float64(rows[0].Total), Sample: rows[0].Total}, nil
		},
	},
	{
		Name: "workspaces.throwaway_accounts", Page: PageWorkspaces, Label: "Accounts throwing workspaces away", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountThrowawayAccounts(ctx, sp.From, sp.To))
		},
		Drill: drill("workspace.created,workspace.deleted"),
	},
}

// fullestQuota is the fullest quota, in percent, of the workspaces with an
// event in sp and of their owners.
func fullestQuota(ctx context.Context, env Env, sp Span) (Reading, error) {
	workspaces, err := env.Store.IndexQuotaHeadroom(ctx, sp.From, sp.To, 0)
	if err != nil {
		return Reading{}, err
	}
	owners, err := env.Store.IndexOwnerHeadroom(ctx, sp.From, sp.To, 0)
	if err != nil {
		return Reading{}, err
	}
	var r Reading
	if len(workspaces) > 0 {
		r = Reading{Value: workspaces[0].Used, Sample: 1}
	}
	if len(owners) > 0 && owners[0].Used > r.Value {
		r = Reading{Value: owners[0].Used, Sample: 1}
	}
	return r, nil
}

// Quotas are the caps the headroom is measured against, for the meters.
type Quotas struct {
	Projects    int `json:"projects"`
	Members     int `json:"members"`
	Invitations int `json:"invitations"`
	Owned       int `json:"owned"`
}

// workspacesSections are the Workspaces page's lists and charts, over its
// window (invitations over the 7 days before its end).
type workspacesSections struct {
	Quotas struct {
		Caps       Quotas             `json:"caps"`
		Workspaces []store.QuotaRow   `json:"workspaces"`
		Owners     []store.OwnerQuota `json:"owners"`
	} `json:"quotas"`
	Rows struct {
		// Cap: each table is counted up to one more than this, which
		// reads as over it.
		Cap        int64                  `json:"cap"`
		Tables     []store.WorkspaceTable `json:"tables"`
		Workspaces []store.WorkspaceRows  `json:"workspaces"`
	} `json:"rows"`
	Growth      []store.WorkspaceGrowth      `json:"growth"`
	Invitations []store.WorkspaceInvitations `json:"invitations"`
	Throwaways  []store.ThrowawayRow         `json:"throwaways"`
	Totals      struct {
		Estimate int64               `json:"estimate"`
		Series   store.MonitorSeries `json:"series"`
	} `json:"totals"`
}

// maxWorkspaceRows is how many workspaces the rows list holds.
const maxWorkspaceRows = 10

// workspacesPage reads the Workspaces page's sections over w.
func workspacesPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &workspacesSections{}
	out.Quotas.Caps = Quotas{Projects: store.MaxWorkspaceProjects, Members: store.MaxWorkspaceMembers, Invitations: store.MaxPendingInvitations, Owned: store.MaxOwnedWorkspaces}
	out.Rows.Tables, out.Rows.Cap = store.WorkspaceTables, store.WorkspaceRowsCap
	invitationsFrom := w.To.Add(-store.InvitationsLookback)
	return readSections(ctx, env, out, []section{
		{"quotas", func(ctx context.Context) (err error) {
			out.Quotas.Workspaces, err = s.IndexQuotaHeadroom(ctx, w.From, w.To, store.QuotaWarnShare)
			return
		}},
		{"quotas", func(ctx context.Context) (err error) {
			out.Quotas.Owners, err = s.IndexOwnerHeadroom(ctx, w.From, w.To, store.QuotaWarnShare)
			return
		}},
		{"rows", func(ctx context.Context) (err error) {
			out.Rows.Workspaces, err = s.IndexWorkspaceRows(ctx, w.From, w.To, maxWorkspaceRows)
			return
		}},
		{"growth", func(ctx context.Context) (err error) {
			out.Growth, err = s.IndexWorkspaceGrowth(ctx, w.From, w.To)
			return
		}},
		{"invitations", func(ctx context.Context) (err error) {
			out.Invitations, err = s.IndexWorkspaceInvitations(ctx, invitationsFrom, w.To)
			return
		}},
		{"throwaways", func(ctx context.Context) (err error) {
			out.Throwaways, err = s.IndexThrowaways(ctx, w.From, w.To)
			return
		}},
		{"totals", func(ctx context.Context) (err error) {
			out.Totals.Estimate, err = s.CountWorkspaceEstimate(ctx)
			return
		}},
		{"totals", func(ctx context.Context) (err error) {
			out.Totals.Series, err = s.SummarizeEventNames(ctx, w.SummaryWindow, "workspace.created", "workspace.deleted")
			return
		}},
	})
}
