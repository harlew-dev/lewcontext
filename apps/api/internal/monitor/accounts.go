package monitor

import (
	"context"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/url"
	"strings"
	"time"
)

// The Accounts page (docs/api/monitoring.md#accounts): which accounts
// behave like spam or abuse? A review list of the accounts tripping any
// abuse signal in a day, each with the signals it tripped; then the
// accounts writing the most, and owning the most workspaces.

// PageAccounts is the Accounts page's name.
const PageAccounts = "accounts"

// accountSignal is a signal counting the accounts that tripped one review
// signal (store.Review…) in a day.
func accountSignal(name, review, label string, window time.Duration, warn float64, d url.Values) Signal {
	return Signal{
		Name: name, Page: PageAccounts, Label: label, Unit: UnitCount,
		Window: window, Warn: warn,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountAccountReviewSignal(ctx, reviewSpan(env, sp.whole(), sp.From), review))
		},
		Drill: d,
	}
}

// AccountsSignals are the Accounts page's signals: how many accounts
// tripped each review signal, then how many of them are new.
var AccountsSignals = []Signal{
	accountSignal("accounts.workspace_sprees", store.ReviewWorkspaces, "Accounts making 5+ workspaces", 24*time.Hour, 1, drill("workspace.created")),
	accountSignal("accounts.invitation_sprees", store.ReviewInvitations, "Accounts inviting 15+ addresses", 24*time.Hour, 1, drill("workspace.invitation.created")),
	accountSignal("accounts.low_acceptance", store.ReviewAcceptance, "Invitations rarely accepted", 7*24*time.Hour, 1, drill("workspace.invitation.created,workspace.invitation.accepted")),
	accountSignal("accounts.project_sprees", store.ReviewProjects, "Accounts making 200+ projects", 24*time.Hour, 1, drill("project.created")),
	// Names printed in invitation emails: the classic way to make someone
	// else's mail carry spam.
	accountSignal("accounts.links_in_names", store.ReviewLinks, "Links in names", 24*time.Hour, 1, nil),
	accountSignal("accounts.limits_reached", store.ReviewLimits, "Accounts reaching their limits", 24*time.Hour, 0, drill("rate_limit.reached")),
	accountSignal("accounts.failed_checks", store.ReviewChecks, "Accounts failing checks", 24*time.Hour, 1, drill("auth.password_checked,auth.mfa.checked", "outcome", "failed")),
	{
		Name: "accounts.new_and_busy", Page: PageAccounts, Label: "New accounts tripping any", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountNewAndBusy(ctx, reviewSpan(env, sp.whole(), env.Now.Add(-store.InvitationsLookback))))
		},
		Drill: drill("auth.registered"),
	},
}

// reviewSpan is what the review signals read: sp in periods, the
// invitations sent since acceptance (store.InvitationsLookback before the
// end, or a signal's own window), and the limits each account counts.
func reviewSpan(env Env, sp store.MonitorSpan, acceptance time.Time) store.AccountReviewSpan {
	return store.AccountReviewSpan{Span: sp, Acceptance: acceptance, Limits: perUserLimits(env), Now: env.Now}
}

// perUserLimits are the limits counted per account, as declared.
func perUserLimits(env Env) []string {
	var names []string
	for _, l := range env.Limits {
		if strings.HasSuffix(l.Name, ":per-user") {
			names = append(names, l.Name)
		}
	}
	return names
}

// writeNames are the events an account's writes record: everything about
// workspaces and projects.
func writeNames() []string {
	var names []string
	for _, n := range events.Names() {
		if strings.HasPrefix(n, "workspace.") || strings.HasPrefix(n, "project.") {
			names = append(names, n)
		}
	}
	return names
}

// accountsSections are the Accounts page's lists, over its window.
type accountsSections struct {
	Review     []store.AccountReview `json:"review"`
	TopWriters []store.AccountCount  `json:"topWriters"`
	TopOwners  []store.AccountCount  `json:"topOwners"`
}

// accountsPage reads the Accounts page's lists over w: the review list in
// days back from the window's end, acceptance over the 7 days before it.
func accountsPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &accountsSections{}
	// Days counted back from the window's end: the last is the last 24 hours.
	rs := reviewSpan(env, store.MonitorSpan{From: w.From, To: w.To, Per: store.AccountDay, Origin: w.To}, w.To.Add(-store.InvitationsLookback))
	return readSections(ctx, env, out, []section{
		{"review", func(ctx context.Context) (err error) { out.Review, err = s.IndexAccountReview(ctx, rs); return }},
		{"topWriters", func(ctx context.Context) (err error) {
			out.TopWriters, err = s.IndexTopWriters(ctx, w.From, w.To, writeNames())
			return
		}},
		{"topOwners", func(ctx context.Context) (err error) {
			out.TopOwners, err = s.IndexTopOwners(ctx, w.From, w.To)
			return
		}},
	})
}
