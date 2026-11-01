package store

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// The Accounts page's queries (docs/api/monitoring.md#accounts): which
// accounts behave like spam or abuse? Each abuse signal is a count of
// events an account recorded in a day (its actor, or the account a check
// names), read by name in the window (events_name_created_idx); an account
// tripping any of them is on the review list with every one it tripped.

// Account review thresholds, each over a day (docs/api/monitoring.md#accounts).
const (
	// WorkspaceSpree: workspaces created by one account. The limit is 10
	// an hour and the quota 20; real people make one or two.
	WorkspaceSpree = 5
	// InvitationSpree, InvitationSpreeEmails: invitations sent by one
	// account, to this many different addresses. Invitations are email we
	// send to addresses the inviter picks.
	InvitationSpree       = 20
	InvitationSpreeEmails = 15
	// ProjectSpree: projects created by one account: filling the database.
	ProjectSpree = 200
	// FailedChecksPerDay: failed password and second-factor checks by one
	// signed-in account: a stolen session guessing.
	FailedChecksPerDay = 3
	// LowAcceptanceSent, LowAcceptanceShare: invitations sent over
	// InvitationsLookback, and the share of them accepted under which the
	// sender looks like spam (spam isn't accepted).
	LowAcceptanceSent  = 20
	LowAcceptanceShare = 10
	// NewAccountFor: an account this young tripping any signal is "new and
	// busy".
	NewAccountFor = 24 * time.Hour
)

// Account review signals, by the name a review row lists them under.
const (
	ReviewWorkspaces  = "workspaces"
	ReviewInvitations = "invitations"
	ReviewAcceptance  = "acceptance"
	ReviewProjects    = "projects"
	ReviewLinks       = "links"
	ReviewLimits      = "limits"
	ReviewChecks      = "checks"
)

// AccountDay is a review list's grouping: a day from the window's start (or
// the whole window, when it's shorter).
const AccountDay = 24 * time.Hour

// linkPattern is what makes a name a link or markup in an email: a
// scheme, "www.", a domain-looking word (letters, digits or a hyphen, a
// dot, then two or more letters) or an angle bracket. Matched without
// regard to case, by PostgreSQL (~*) and Go alike (POSIX classes both
// read the same way). It flags for review; a name like "Mr.Smith" matches
// too, and that's fine.
const linkPattern = `https?:|www\.|[[:alnum:]-]+\.[[:alpha:]]{2,}|[<>]`

var linkInName = regexp.MustCompile("(?i)" + linkPattern)

// LinkInName reports whether a name holds a link or markup.
func LinkInName(name string) bool {
	return linkInName.MatchString(name)
}

// accountCount is one account's count for a review signal: Of is the other
// number some signals need (distinct addresses invited; invitations sent).
type accountCount struct {
	UserID int64 `bun:"user_id"`
	Count  int64 `bun:"count"`
	Of     int64 `bun:"of"`
}

// perAccount keeps each account's busiest period of grouped (a query with
// user_id, count and of per period), most first.
func (s *Store) perAccount(grouped *bun.SelectQuery) *bun.SelectQuery {
	return s.DB.NewSelect().TableExpr("(?) AS g", grouped).
		ColumnExpr("g.user_id, max(g.count) AS count, max(g.of) AS of").
		GroupExpr("g.user_id")
}

// actorSpree groups events named name by actor and period, keeping those
// with at least min.
func (s *Store) actorSpree(sp MonitorSpan, name string, min int) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, name).
		ColumnExpr(period+" AS period", args...).
		ColumnExpr("e.actor_id AS user_id, count(*) AS count, 0 AS of").
		Where("e.actor_id IS NOT NULL").
		GroupExpr("1, 2").
		Having("count(*) >= ?", min)
}

// invitationSpree groups invitations by inviter and period, keeping those
// with InvitationSpree or more to InvitationSpreeEmails or more addresses.
func (s *Store) invitationSpree(sp MonitorSpan) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, "workspace.invitation.created").
		ColumnExpr(period+" AS period", args...).
		ColumnExpr("e.actor_id AS user_id, count(*) AS count").
		ColumnExpr("count(DISTINCT lower(e.payload->>'email')) AS of").
		Where("e.actor_id IS NOT NULL").
		GroupExpr("1, 2").
		Having("count(*) >= ?", InvitationSpree).
		Having("count(DISTINCT lower(e.payload->>'email')) >= ?", InvitationSpreeEmails)
}

// failedChecksPerAccount groups failed password and second-factor checks
// by the account they name and period, keeping those with
// FailedChecksPerDay or more.
func (s *Store) failedChecksPerAccount(sp MonitorSpan) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, CheckPassword, CheckSecondFactor).
		Join("JOIN users AS u ON u.public_id = e.payload->>'user'").
		ColumnExpr(period+" AS period", args...).
		ColumnExpr("u.id AS user_id, count(*) AS count, 0 AS of").
		Where("e.payload->>'ok' = 'false'").
		GroupExpr("1, 2").
		Having("count(*) >= ?", FailedChecksPerDay)
}

// limitsPerAccount groups windows reached of the limits named (per
// account) by account and period.
func (s *Store) limitsPerAccount(sp MonitorSpan, limits []string) *bun.SelectQuery {
	period, args := sp.period()
	return s.eventsIn(sp.From, sp.To, "rate_limit.reached").
		ColumnExpr(period+" AS period", args...).
		ColumnExpr("e.user_id, count(*) AS count, 0 AS of").
		Where("e.user_id IS NOT NULL").
		Where(reachedLimit+" IN (?)", bun.In(limits)).
		GroupExpr("1, 2")
}

// lowAcceptance groups the invitations sent in [from, to) by inviter,
// with how many were accepted, keeping senders of LowAcceptanceSent or
// more with under LowAcceptanceShare percent accepted. An acceptance finds
// its invitation by public ID among the acceptances in the same span.
func (s *Store) lowAcceptance(from, to time.Time) *bun.SelectQuery {
	accepted := s.eventsIn(from, to, "workspace.invitation.accepted").ColumnExpr("DISTINCT e.payload->>'invitation' AS invitation")
	return s.eventsIn(from, to, "workspace.invitation.created").
		Join("LEFT JOIN (?) AS a ON a.invitation = e.payload->>'invitation'", accepted).
		ColumnExpr("e.actor_id AS user_id, count(a.invitation) AS count, count(*) AS of").
		Where("e.actor_id IS NOT NULL").
		GroupExpr("e.actor_id").
		Having("count(*) >= ?", LowAcceptanceSent).
		Having("count(a.invitation) * 100 < count(*) * ?", LowAcceptanceShare)
}

// linkCandidates selects the accounts that named something in [from, to):
// signed up, changed their name, made, renamed or invited from a
// workspace. Only their names, and their workspaces', are read.
func (s *Store) linkCandidates(from, to time.Time) *bun.SelectQuery {
	return s.eventsIn(from, to, "auth.registered", "auth.profile_updated", "workspace.created", "workspace.renamed", "workspace.invitation.created").
		ColumnExpr("DISTINCT coalesce(e.actor_id, e.user_id)")
}

// LinkedName is an account whose name, or a workspace it owns, holds a
// link or markup (LinkInName). Workspace is the workspace, when it's its
// name.
type LinkedName struct {
	UserID    int64         `bun:"user_id"`
	Workspace *WorkspaceRef `bun:"-"`
	PublicID  *string       `bun:"workspace"`
	Name      *string       `bun:"workspace_name"`
}

// linkedNames finds, among the accounts that named something in [from,
// to), those whose name or an owned workspace's holds a link: the account
// first, then each workspace (workspace_user_user_id_idx).
func (s *Store) linkedNames(ctx context.Context, from, to time.Time) ([]LinkedName, error) {
	candidates := s.linkCandidates(from, to)
	var accounts []LinkedName
	err := s.DB.NewSelect().Model((*User)(nil)).ColumnExpr("u.id AS user_id").
		Where("u.id IN (?)", candidates).
		Where("u.name ~* ?", linkPattern).
		OrderExpr("u.id").Limit(maxReviewRows).
		Scan(ctx, &accounts)
	if err != nil {
		return nil, err
	}
	var owned []LinkedName
	err = s.DB.NewSelect().TableExpr("workspace_user AS m").
		Join("JOIN workspaces AS w ON w.id = m.workspace_id").
		ColumnExpr("m.user_id, w.public_id AS workspace, w.name AS workspace_name").
		Where("m.role = 'owner'").
		Where("m.user_id IN (?)", candidates).
		Where("w.name ~* ?", linkPattern).
		OrderExpr("m.user_id, w.public_id").Limit(maxReviewRows).
		Scan(ctx, &owned)
	for i, o := range owned {
		owned[i].Workspace = &WorkspaceRef{ID: *o.PublicID, Name: *o.Name}
	}
	return append(accounts, owned...), err
}

// AccountReview is an account on the review list: when it was made (new:
// within NewAccountFor), and every signal it tripped.
type AccountReview struct {
	Account   AccountRef      `json:"account"`
	CreatedAt time.Time       `json:"createdAt"`
	New       bool            `json:"new"`
	Signals   []AccountSignal `json:"signals"`
}

// AccountSignal is a review signal an account tripped: its count in its
// busiest day, and for some a second number (Of: the addresses invited;
// the invitations sent, of which Count were accepted). Workspace names the
// workspace whose name holds a link.
type AccountSignal struct {
	Signal    string        `json:"signal"`
	Count     int64         `json:"count"`
	Of        int64         `json:"of,omitempty"`
	Workspace *WorkspaceRef `json:"workspace,omitempty"`
}

// maxReviewAccounts is how many accounts the review list holds: each
// signal finds maxReviewRows at most, the list keeps the most suspicious.
const maxReviewAccounts = 50

// AccountReviewSpan is what the review list reads: the window in days
// (sp, Per AccountDay), the acceptance's lookback ending at the same time,
// the limits counted per account, and the time "new" is judged at.
type AccountReviewSpan struct {
	Span       MonitorSpan
	Acceptance time.Time
	Limits     []string
	Now        time.Time
}

// reviewQueries are the review signals' queries over rs, each an account
// per row (user_id, count, of): its busiest period.
func (s *Store) reviewQueries(rs AccountReviewSpan) []struct {
	signal string
	q      *bun.SelectQuery
} {
	sp := rs.Span
	out := []struct {
		signal string
		q      *bun.SelectQuery
	}{
		{ReviewWorkspaces, s.perAccount(s.actorSpree(sp, "workspace.created", WorkspaceSpree))},
		{ReviewInvitations, s.perAccount(s.invitationSpree(sp))},
		{ReviewAcceptance, s.DB.NewSelect().TableExpr("(?) AS g", s.lowAcceptance(rs.Acceptance, sp.To)).ColumnExpr("g.*")},
		{ReviewProjects, s.perAccount(s.actorSpree(sp, "project.created", ProjectSpree))},
		{ReviewChecks, s.perAccount(s.failedChecksPerAccount(sp))},
	}
	if len(rs.Limits) > 0 {
		out = append(out, struct {
			signal string
			q      *bun.SelectQuery
		}{ReviewLimits, s.perAccount(s.limitsPerAccount(sp, rs.Limits))})
	}
	return out
}

// IndexAccountReview lists the accounts tripping any review signal (each
// signal's maxReviewRows busiest), with every signal each tripped: most
// signals first, then new accounts, then the newest.
func (s *Store) IndexAccountReview(ctx context.Context, rs AccountReviewSpan) ([]AccountReview, error) {
	found := map[int64][]AccountSignal{}
	for _, step := range s.reviewQueries(rs) {
		var rows []accountCount
		if err := step.q.OrderExpr("count DESC, user_id").Limit(maxReviewRows).Scan(ctx, &rows); err != nil {
			return nil, fmt.Errorf("account review %s: %w", step.signal, err)
		}
		for _, r := range rows {
			found[r.UserID] = append(found[r.UserID], AccountSignal{Signal: step.signal, Count: r.Count, Of: r.Of})
		}
	}
	linked, err := s.linkedNames(ctx, rs.Span.From, rs.Span.To)
	if err != nil {
		return nil, err
	}
	for _, l := range linked {
		found[l.UserID] = append(found[l.UserID], AccountSignal{Signal: ReviewLinks, Count: 1, Workspace: l.Workspace})
	}
	return s.reviewRows(ctx, found, rs.Now)
}

// reviewRows names the accounts found, marks the new ones, and orders
// them: most signals, new accounts, then newest.
func (s *Store) reviewRows(ctx context.Context, found map[int64][]AccountSignal, now time.Time) ([]AccountReview, error) {
	out := []AccountReview{}
	if len(found) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	var accounts []struct {
		ID        int64     `bun:"id"`
		PublicID  string    `bun:"public_id"`
		Name      string    `bun:"name"`
		CreatedAt time.Time `bun:"created_at"`
	}
	if err := s.DB.NewSelect().Model((*User)(nil)).Column("u.id", "u.public_id", "u.name", "u.created_at").
		Where("u.id IN (?)", bun.In(ids)).Scan(ctx, &accounts); err != nil {
		return nil, err
	}
	for _, a := range accounts {
		out = append(out, AccountReview{
			Account:   AccountRef{ID: a.PublicID, Name: a.Name},
			CreatedAt: a.CreatedAt,
			New:       now.Sub(a.CreatedAt) < NewAccountFor,
			Signals:   found[a.ID],
		})
	}
	slices.SortFunc(out, func(a, b AccountReview) int {
		if len(a.Signals) != len(b.Signals) {
			return len(b.Signals) - len(a.Signals)
		}
		if a.New != b.New {
			if a.New {
				return -1
			}
			return 1
		}
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Account.ID, b.Account.ID)
	})
	if len(out) > maxReviewAccounts {
		out = out[:maxReviewAccounts]
	}
	return out, nil
}

// CountAccountReviewSignal counts the accounts tripping one review signal
// (Review…) in rs.
func (s *Store) CountAccountReviewSignal(ctx context.Context, rs AccountReviewSpan, signal string) (int64, error) {
	if signal == ReviewLinks {
		return s.countGroups(ctx, s.linkedAccounts(rs.Span.From, rs.Span.To))
	}
	for _, step := range s.reviewQueries(rs) {
		if step.signal == signal {
			return s.countGroups(ctx, step.q)
		}
	}
	return 0, nil
}

// CountNewAndBusy counts the accounts made within NewAccountFor of rs.Now
// that tripped any review signal in rs.
func (s *Store) CountNewAndBusy(ctx context.Context, rs AccountReviewSpan) (int64, error) {
	tripped := s.linkedAccounts(rs.Span.From, rs.Span.To)
	for _, step := range s.reviewQueries(rs) {
		tripped = tripped.Union(s.DB.NewSelect().TableExpr("(?) AS x", step.q).ColumnExpr("x.user_id"))
	}
	var n int64
	err := s.DB.NewSelect().Model((*User)(nil)).ColumnExpr("count(*)").
		Where("u.id IN (?)", tripped).
		Where("u.created_at > ?", rs.Now.Add(-NewAccountFor)).
		Scan(ctx, &n)
	return n, err
}

// linkedAccounts selects the accounts linkedNames finds, once each.
func (s *Store) linkedAccounts(from, to time.Time) *bun.SelectQuery {
	candidates := s.linkCandidates(from, to)
	byName := s.DB.NewSelect().Model((*User)(nil)).ColumnExpr("u.id AS user_id").
		Where("u.id IN (?)", candidates).
		Where("u.name ~* ?", linkPattern)
	byWorkspace := s.DB.NewSelect().TableExpr("workspace_user AS m").
		Join("JOIN workspaces AS w ON w.id = m.workspace_id").
		ColumnExpr("m.user_id").
		Where("m.role = 'owner'").
		Where("m.user_id IN (?)", candidates).
		Where("w.name ~* ?", linkPattern)
	return byName.Union(byWorkspace)
}

// AccountCount is an account and a count: its writes, or the workspaces
// it owns.
type AccountCount struct {
	Account AccountRef `json:"account" bun:"-"`
	UserID  int64      `json:"-" bun:"user_id"`
	Count   int64      `json:"count" bun:"count"`
}

// IndexTopWriters lists the accounts recording the most events named
// names (their writes) as actor in [from, to), most first.
func (s *Store) IndexTopWriters(ctx context.Context, from, to time.Time, names []string) ([]AccountCount, error) {
	rows := []AccountCount{}
	err := s.eventsIn(from, to, names...).
		ColumnExpr("e.actor_id AS user_id, count(*) AS count").
		Where("e.actor_id IS NOT NULL").
		GroupExpr("e.actor_id").
		OrderExpr("count DESC, e.actor_id").
		Limit(maxTopSubjects).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	return rows, s.nameAccounts(ctx, rows)
}

// IndexTopOwners lists, of the accounts that created a workspace in
// [from, to), those owning the most workspaces (workspace_user_user_id_idx).
func (s *Store) IndexTopOwners(ctx context.Context, from, to time.Time) ([]AccountCount, error) {
	creators := s.eventsIn(from, to, "workspace.created").ColumnExpr("e.actor_id").Where("e.actor_id IS NOT NULL")
	rows := []AccountCount{}
	err := s.DB.NewSelect().TableExpr("workspace_user AS m").
		ColumnExpr("m.user_id, count(*) AS count").
		Where("m.role = 'owner'").
		Where("m.user_id IN (?)", creators).
		GroupExpr("m.user_id").
		OrderExpr("count DESC, m.user_id").
		Limit(maxTopSubjects).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	return rows, s.nameAccounts(ctx, rows)
}

func (s *Store) nameAccounts(ctx context.Context, rows []AccountCount) error {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.UserID
	}
	refs, err := s.accountRefs(ctx, ids)
	for i := range rows {
		rows[i].Account = refs[rows[i].UserID]
	}
	return err
}
