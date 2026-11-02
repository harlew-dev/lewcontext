package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// The Usage page's queries (docs/api/monitoring.md#usage): how many people
// use the app, and what they use. Active and inactive accounts come from
// users.last_seen_at (users_last_seen_idx, users_inactive_idx), sign-ups
// and feature use from events by name, workspace sizes from the workspaces
// with an event in the window (as the Workspaces page counts rows).

// InactiveAfter is how long an account goes unseen before it's listed as
// inactive (compliance decision 9): never deleted automatically, since
// deleting is a business policy, not a default.
const InactiveAfter = 2 * 365 * 24 * time.Hour

// Activation: what a new account does first, and how long it's given.
const (
	// ActivatedWithin: creating or joining a workspace within a day.
	ActivatedWithin = 24 * time.Hour
	// InvitedWithin: inviting someone within a week.
	InvitedWithin = 7 * 24 * time.Hour
)

// Activation is a cohort of new accounts: how many registered, and how many
// of them did something within the time they're given.
type Activation struct {
	Accounts  int64     `json:"accounts"`
	Activated int64     `json:"activated"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
}

// CountActivation counts the accounts registered in [from, to)
// (events_name_created_idx), and those of them that recorded one of names
// as actor within `within` of registering (events_actor_created_idx).
func (s *Store) CountActivation(ctx context.Context, from, to time.Time, within time.Duration, names ...string) (Activation, error) {
	out := Activation{From: from, To: to}
	did := s.DB.NewSelect().TableExpr("events AS x").ColumnExpr("1").
		Where("x.actor_id = e.user_id").
		Where("x.name IN (?)", bun.In(names)).
		Where("x.created_at >= e.created_at").
		Where("x.created_at < e.created_at + ?::interval", fmt.Sprintf("%d seconds", int(within.Seconds())))
	err := s.eventsIn(from, to, "auth.registered").
		ColumnExpr("count(*)").
		ColumnExpr("count(*) FILTER (WHERE EXISTS (?))", did).
		Where("e.user_id IS NOT NULL").
		Scan(ctx, &out.Accounts, &out.Activated)
	return out, err
}

// CountMFAAccounts counts the accounts seen since since (users_last_seen_idx)
// with two-factor authentication on.
func (s *Store) CountMFAAccounts(ctx context.Context, since time.Time) (int64, error) {
	n, err := s.DB.NewSelect().Model((*User)(nil)).
		Join("JOIN mfa_totp AS t ON t.user_id = u.id AND t.confirmed_at IS NOT NULL").
		Where("u.last_seen_at >= ?", since).
		Count(ctx)
	return int64(n), err
}

// SizeCount is how many workspaces are of a size.
type SizeCount struct {
	Size       string `json:"size" bun:"size"`
	Workspaces int64  `json:"workspaces" bun:"workspaces"`
}

// WorkspaceSizes are how many workspaces have so many members, and so many
// projects, each size listed (zero included) in order.
type WorkspaceSizes struct {
	Members  []SizeCount `json:"members"`
	Projects []SizeCount `json:"projects"`
}

// Workspace sizes, by members and by projects.
var (
	memberSizes  = []string{"1", "2–5", "6–20", "21+"}
	projectSizes = []string{"0", "1–10", "11–100", "101+"}
)

// The SQL that puts a count in its size, in the order above.
const (
	memberSize  = "CASE WHEN q.members <= 1 THEN '1' WHEN q.members <= 5 THEN '2–5' WHEN q.members <= 20 THEN '6–20' ELSE '21+' END"
	projectSize = "CASE WHEN q.projects = 0 THEN '0' WHEN q.projects <= 10 THEN '1–10' WHEN q.projects <= 100 THEN '11–100' ELSE '101+' END"
)

// CountWorkspaceSizes counts the workspaces with an event in [from, to) by
// how many members and projects they have.
func (s *Store) CountWorkspaceSizes(ctx context.Context, from, to time.Time) (WorkspaceSizes, error) {
	counts := s.DB.NewSelect().TableExpr("workspaces AS w").
		ColumnExpr("(SELECT count(*) FROM workspace_user AS m WHERE m.workspace_id = w.id) AS members").
		ColumnExpr("(SELECT count(*) FROM projects AS p WHERE p.workspace_id = w.id) AS projects").
		Where("w.id IN (?)", s.activeWorkspaces(from, to))
	var rows []struct {
		Members    string `bun:"members"`
		Projects   string `bun:"projects"`
		Workspaces int64  `bun:"workspaces"`
	}
	err := s.DB.NewSelect().TableExpr("(?) AS q", counts).
		ColumnExpr(memberSize+" AS members").
		ColumnExpr(projectSize+" AS projects").
		ColumnExpr("count(*) AS workspaces").
		GroupExpr("1, 2").
		Scan(ctx, &rows)
	out := WorkspaceSizes{Members: sizes(memberSizes), Projects: sizes(projectSizes)}
	for _, r := range rows {
		for i := range out.Members {
			if out.Members[i].Size == r.Members {
				out.Members[i].Workspaces += r.Workspaces
			}
		}
		for i := range out.Projects {
			if out.Projects[i].Size == r.Projects {
				out.Projects[i].Workspaces += r.Workspaces
			}
		}
	}
	return out, err
}

func sizes(names []string) []SizeCount {
	out := make([]SizeCount, len(names))
	for i, n := range names {
		out[i] = SizeCount{Size: n}
	}
	return out
}

// InactiveAccount is an account not seen since before a time: its last
// signed-in request, or never (then when it was made). Admin says it holds
// admin: admin sessions don't move last_seen_at, so an admin who never uses
// the app looks inactive.
type InactiveAccount struct {
	Account    AccountRef `json:"account" bun:"-"`
	PublicID   string     `json:"-" bun:"public_id"`
	Name       string     `json:"-" bun:"name"`
	LastSeenAt *time.Time `json:"lastSeenAt" bun:"last_seen_at"`
	CreatedAt  time.Time  `json:"createdAt" bun:"created_at"`
	Admin      bool       `json:"admin" bun:"admin"`
}

// inactiveSince is when an account was last active: its last signed-in
// request, or when it was made (users_inactive_idx).
const inactiveSince = "coalesce(u.last_seen_at, u.created_at)"

// inactive selects the accounts not seen since before.
func (s *Store) inactive(before time.Time) *bun.SelectQuery {
	return s.DB.NewSelect().Model((*User)(nil)).Where(inactiveSince+" < ?", before)
}

// IndexInactiveAccounts lists the accounts not seen since before, longest
// unseen first.
func (s *Store) IndexInactiveAccounts(ctx context.Context, before time.Time) ([]InactiveAccount, error) {
	rows := []InactiveAccount{}
	err := s.inactive(before).
		Column("u.public_id", "u.name", "u.last_seen_at", "u.created_at").
		ColumnExpr("EXISTS (SELECT 1 FROM admins AS a WHERE a.user_id = u.id) AS admin").
		OrderExpr(inactiveSince+", u.id").
		Limit(maxReviewRows).
		Scan(ctx, &rows)
	for i, r := range rows {
		rows[i].Account = AccountRef{ID: r.PublicID, Name: r.Name}
	}
	return rows, err
}

// CountInactiveAccounts counts the accounts not seen since before.
func (s *Store) CountInactiveAccounts(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.inactive(before).Count(ctx)
	return int64(n), err
}
