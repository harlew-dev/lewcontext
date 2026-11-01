package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// The Workspaces page's queries (docs/api/monitoring.md#workspaces):
// which workspaces are filling up, growing fast, or used to send spam.
//
// A workspace's rows have no time window of their own, so counting them is
// bounded by its candidates instead: only the workspaces with an event in
// the window (events_created_idx), each counted one at a time through its
// tables' workspace_id indexes (WorkspaceTables). A workspace grows only by
// writing, which records events, so one filling up is among them; a
// workspace nobody has touched in the window isn't counted at all.

// QuotaWarnShare: a quota this full (in percent) is a warning; full, it's
// refusing creates.
const QuotaWarnShare = 80

// ThrowawayWorkspaces: an account creating and deleting this many
// workspaces, each within ThrowawayWithin, is worth a look.
const (
	ThrowawayWorkspaces = 3
	ThrowawayWithin     = 24 * time.Hour
)

// InvitationsLookback is what the invitations per workspace, and their
// acceptance, are counted over.
const InvitationsLookback = 7 * 24 * time.Hour

// maxTopWorkspaces is how many workspaces a top list holds.
const maxTopWorkspaces = 10

// WorkspaceRef names a workspace in a list: its public ID and name. ID
// alone once it's deleted.
type WorkspaceRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// activeWorkspaces selects the internal IDs of the workspaces with an event
// in [from, to).
func (s *Store) activeWorkspaces(from, to time.Time) *bun.SelectQuery {
	return s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("DISTINCT e.workspace_id").
		Where("e.created_at >= ?", from).
		Where("e.created_at < ?", to).
		Where("e.workspace_id IS NOT NULL")
}

// QuotaRow is a workspace's use of its quotas: its projects (of
// MaxWorkspaceProjects), its members and live invitations together (of
// MaxWorkspaceMembers) and its live invitations (of MaxPendingInvitations),
// and the fullest as a share.
type QuotaRow struct {
	Workspace   WorkspaceRef `json:"workspace" bun:"-"`
	PublicID    string       `json:"-" bun:"id"`
	Name        string       `json:"-" bun:"name"`
	Projects    int64        `json:"projects" bun:"projects"`
	Members     int64        `json:"members" bun:"members"`
	Invitations int64        `json:"invitations" bun:"invitations"`
	Used        float64      `json:"used" bun:"used"`
}

// quotaUsed is a workspace's fullest quota, in percent.
const quotaUsed = "greatest(q.projects * 100.0 / ?, (q.members + q.invitations) * 100.0 / ?, q.invitations * 100.0 / ?)"

// IndexQuotaHeadroom lists the workspaces with an event in [from, to)
// whose fullest quota is atLeast percent used or more, fullest first.
func (s *Store) IndexQuotaHeadroom(ctx context.Context, from, to time.Time, atLeast float64) ([]QuotaRow, error) {
	counts := s.DB.NewSelect().TableExpr("workspaces AS w").
		ColumnExpr("w.public_id AS id, w.name").
		ColumnExpr("(SELECT count(*) FROM projects AS p WHERE p.workspace_id = w.id) AS projects").
		ColumnExpr("(SELECT count(*) FROM workspace_user AS m WHERE m.workspace_id = w.id) AS members").
		ColumnExpr("(SELECT count(*) FROM workspace_invitations AS i WHERE i.workspace_id = w.id AND i.expires_at > now()) AS invitations").
		Where("w.id IN (?)", s.activeWorkspaces(from, to))
	quotas := []any{MaxWorkspaceProjects, MaxWorkspaceMembers, MaxPendingInvitations}
	rows := []QuotaRow{}
	err := s.DB.NewSelect().TableExpr("(?) AS q", counts).
		ColumnExpr("q.*").
		ColumnExpr(quotaUsed+" AS used", quotas...).
		Where(quotaUsed+" >= ?", append(quotas, atLeast)...).
		OrderExpr("used DESC, q.id").
		Limit(maxReviewRows).
		Scan(ctx, &rows)
	for i, r := range rows {
		rows[i].Workspace = WorkspaceRef{ID: r.PublicID, Name: r.Name}
	}
	return rows, err
}

// OwnerQuota is how many workspaces an account owns, of
// MaxOwnedWorkspaces, as a share.
type OwnerQuota struct {
	Account AccountRef `json:"account" bun:"-"`
	UserID  int64      `json:"-" bun:"user_id"`
	Owned   int64      `json:"owned" bun:"owned"`
	Used    float64    `json:"used" bun:"used"`
}

// IndexOwnerHeadroom lists the owners of workspaces with an event in
// [from, to) who own atLeast percent of MaxOwnedWorkspaces or more, most
// first (workspace_user_user_id_idx).
func (s *Store) IndexOwnerHeadroom(ctx context.Context, from, to time.Time, atLeast float64) ([]OwnerQuota, error) {
	owners := s.DB.NewSelect().TableExpr("workspace_user AS o").ColumnExpr("o.user_id").
		Where("o.role = 'owner'").
		Where("o.workspace_id IN (?)", s.activeWorkspaces(from, to))
	rows := []OwnerQuota{}
	err := s.DB.NewSelect().TableExpr("workspace_user AS m").
		ColumnExpr("m.user_id, count(*) AS owned").
		ColumnExpr("count(*) * 100.0 / ? AS used", MaxOwnedWorkspaces).
		Where("m.role = 'owner'").
		Where("m.user_id IN (?)", owners).
		GroupExpr("m.user_id").
		Having("count(*) * 100.0 / ? >= ?", MaxOwnedWorkspaces, atLeast).
		OrderExpr("owned DESC, m.user_id").
		Limit(maxReviewRows).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	return rows, s.nameOwners(ctx, rows)
}

func (s *Store) nameOwners(ctx context.Context, rows []OwnerQuota) error {
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

// WorkspaceRows is a workspace's rows in every table of WorkspaceTables,
// in that order, and their total.
type WorkspaceRows struct {
	Workspace WorkspaceRef `json:"workspace" bun:"-"`
	PublicID  string       `json:"-" bun:"id"`
	Name      string       `json:"-" bun:"name"`
	Counts    []int64      `json:"counts" bun:"counts,array"`
	Total     int64        `json:"total" bun:"total"`
}

// IndexWorkspaceRows lists the workspaces with an event in [from, to)
// holding the most rows across WorkspaceTables, most first.
func (s *Store) IndexWorkspaceRows(ctx context.Context, from, to time.Time, limit int) ([]WorkspaceRows, error) {
	// A count per registered table: their names are fixed Go strings,
	// quoted as identifiers, never input.
	counts := make([]string, len(WorkspaceTables))
	tables := make([]any, len(WorkspaceTables))
	for i, t := range WorkspaceTables {
		counts[i] = "(SELECT count(*) FROM ? AS t WHERE t.workspace_id = w.id)"
		tables[i] = bun.Ident(t.Name)
	}
	perTable := s.DB.NewSelect().TableExpr("workspaces AS w").
		ColumnExpr("w.public_id AS id, w.name").
		ColumnExpr("ARRAY["+strings.Join(counts, ", ")+"] AS counts", tables...).
		Where("w.id IN (?)", s.activeWorkspaces(from, to))
	rows := []WorkspaceRows{}
	err := s.DB.NewSelect().TableExpr("(?) AS q", perTable).
		ColumnExpr("q.*").
		ColumnExpr("(SELECT sum(c) FROM unnest(q.counts) AS c)::bigint AS total").
		OrderExpr("total DESC, q.id").
		Limit(int64(limit)).
		Scan(ctx, &rows)
	for i, r := range rows {
		rows[i].Workspace = WorkspaceRef{ID: r.PublicID, Name: r.Name}
	}
	return rows, err
}

// WorkspaceGrowth is a workspace's activity in a window: the projects and
// invitations it created, and every event recorded about it.
type WorkspaceGrowth struct {
	Workspace   WorkspaceRef `json:"workspace" bun:"-"`
	WorkspaceID int64        `json:"-" bun:"workspace_id"`
	Created     int64        `json:"created" bun:"created"`
	Events      int64        `json:"events" bun:"events"`
}

// growthCreates are the events that add a workspace's rows: projects and
// invitations.
var growthCreates = []string{"project.created", "workspace.invitation.created"}

// IndexWorkspaceGrowth lists the workspaces that created the most, and
// those with the most events, in [from, to): the top maxTopWorkspaces by
// each.
func (s *Store) IndexWorkspaceGrowth(ctx context.Context, from, to time.Time) ([]WorkspaceGrowth, error) {
	grouped := s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("e.workspace_id").
		ColumnExpr("count(*) FILTER (WHERE e.name IN (?)) AS created", bun.In(growthCreates)).
		ColumnExpr("count(*) AS events").
		Where("e.created_at >= ?", from).
		Where("e.created_at < ?", to).
		Where("e.workspace_id IS NOT NULL").
		GroupExpr("e.workspace_id")
	ranked := s.DB.NewSelect().TableExpr("(?) AS g", grouped).ColumnExpr("g.*").
		ColumnExpr("row_number() OVER (ORDER BY g.created DESC, g.workspace_id) AS by_created").
		ColumnExpr("row_number() OVER (ORDER BY g.events DESC, g.workspace_id) AS by_events")
	rows := []WorkspaceGrowth{}
	err := s.DB.NewSelect().TableExpr("(?) AS r", ranked).
		ColumnExpr("r.workspace_id, r.created, r.events").
		Where("r.by_created <= ? OR r.by_events <= ?", maxTopWorkspaces, maxTopWorkspaces).
		OrderExpr("r.created DESC, r.events DESC, r.workspace_id").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.WorkspaceID
	}
	refs, err := s.workspaceRefs(ctx, ids)
	for i := range rows {
		rows[i].Workspace = refs[rows[i].WorkspaceID]
	}
	return rows, err
}

// workspaceRefs names workspaces by internal ID.
func (s *Store) workspaceRefs(ctx context.Context, ids []int64) (map[int64]WorkspaceRef, error) {
	out := map[int64]WorkspaceRef{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID       int64  `bun:"id"`
		PublicID string `bun:"public_id"`
		Name     string `bun:"name"`
	}
	err := s.DB.NewSelect().Model((*Workspace)(nil)).Column("w.id", "w.public_id", "w.name").
		Where("w.id IN (?)", bun.In(ids)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.ID] = WorkspaceRef{ID: r.PublicID, Name: r.Name}
	}
	return out, err
}

// workspaceRefsByPublicID names workspaces by public ID (an event
// payload's workspace); one that's gone keeps its ID alone.
func (s *Store) workspaceRefsByPublicID(ctx context.Context, ids []string) (map[string]WorkspaceRef, error) {
	out := map[string]WorkspaceRef{}
	for _, id := range ids {
		out[id] = WorkspaceRef{ID: id}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		PublicID string `bun:"public_id"`
		Name     string `bun:"name"`
	}
	err := s.DB.NewSelect().Model((*Workspace)(nil)).Column("w.public_id", "w.name").
		Where("w.public_id IN (?)", bun.In(ids)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.PublicID] = WorkspaceRef{ID: r.PublicID, Name: r.Name}
	}
	return out, err
}

// WorkspaceInvitations are a workspace's invitations over
// InvitationsLookback: sent, accepted and revoked (from their events),
// and expired unanswered (workspace_invitations, kept 30 days after).
type WorkspaceInvitations struct {
	Workspace WorkspaceRef `json:"workspace" bun:"-"`
	PublicID  string       `json:"-" bun:"workspace"`
	Sent      int64        `json:"sent" bun:"sent"`
	Accepted  int64        `json:"accepted" bun:"accepted"`
	Revoked   int64        `json:"revoked" bun:"revoked"`
	Expired   int64        `json:"expired" bun:"expired"`
}

// IndexWorkspaceInvitations lists the workspaces sending the most
// invitations in [from, to), with what became of them; a deleted
// workspace by its ID.
func (s *Store) IndexWorkspaceInvitations(ctx context.Context, from, to time.Time) ([]WorkspaceInvitations, error) {
	rows := []WorkspaceInvitations{}
	err := s.eventsIn(from, to, "workspace.invitation.created", "workspace.invitation.accepted", "workspace.invitation.revoked").
		ColumnExpr("e.payload->>'workspace' AS workspace").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'workspace.invitation.created') AS sent").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'workspace.invitation.accepted') AS accepted").
		ColumnExpr("count(*) FILTER (WHERE e.name = 'workspace.invitation.revoked') AS revoked").
		Where("e.payload->>'workspace' IS NOT NULL").
		GroupExpr("1").
		Having("count(*) FILTER (WHERE e.name = 'workspace.invitation.created') > 0").
		OrderExpr("sent DESC, 1").
		Limit(maxTopWorkspaces).
		Scan(ctx, &rows)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.PublicID
	}
	// Expired unanswered in the window (workspace_invitations_expires_at_idx).
	var expired []struct {
		Workspace string `bun:"workspace"`
		Expired   int64  `bun:"expired"`
	}
	err = s.DB.NewSelect().TableExpr("workspace_invitations AS i").
		Join("JOIN workspaces AS w ON w.id = i.workspace_id").
		ColumnExpr("w.public_id AS workspace, count(*) AS expired").
		Where("i.expires_at >= ? AND i.expires_at < ?", from, to).
		Where("w.public_id IN (?)", bun.In(ids)).
		GroupExpr("w.public_id").
		Scan(ctx, &expired)
	if err != nil {
		return nil, err
	}
	refs, err := s.workspaceRefsByPublicID(ctx, ids)
	for i := range rows {
		rows[i].Workspace = refs[rows[i].PublicID]
		for _, x := range expired {
			if x.Workspace == rows[i].PublicID {
				rows[i].Expired = x.Expired
			}
		}
	}
	return rows, err
}

// ThrowawayRow is an account that created workspaces and deleted them
// within ThrowawayWithin, and the invitations those workspaces sent.
type ThrowawayRow struct {
	Account     AccountRef `json:"account" bun:"-"`
	UserID      int64      `json:"-" bun:"user_id"`
	Workspaces  int64      `json:"workspaces" bun:"workspaces"`
	Invitations int64      `json:"invitations" bun:"invitations"`
	LastDeleted time.Time  `json:"lastDeleted" bun:"last_deleted"`
}

// throwaways groups the workspaces deleted in [from, to) within
// ThrowawayWithin of being created by their creator, keeping those with at
// least min. Each deletion finds its creation, and the invitations the
// workspace sent, by its public ID (events_payload_workspace_idx).
func (s *Store) throwaways(from, to time.Time, min int) *bun.SelectQuery {
	within := fmt.Sprintf("%d seconds", int(ThrowawayWithin.Seconds()))
	invitations := s.DB.NewSelect().TableExpr("events AS i").ColumnExpr("count(*)").
		Where(`i.payload \? 'workspace' AND i.payload->>'workspace' = d.payload->>'workspace'`).
		Where("i.name = 'workspace.invitation.created'")
	deleted := s.DB.NewSelect().TableExpr("events AS d").
		Join(`JOIN events AS c ON c.payload \? 'workspace' AND c.payload->>'workspace' = d.payload->>'workspace' AND c.name = 'workspace.created' AND c.created_at <= d.created_at AND c.created_at > d.created_at - ?::interval`, within).
		ColumnExpr("c.actor_id AS user_id, d.created_at AS deleted_at").
		ColumnExpr("(?) AS invitations", invitations).
		Where("d.name = 'workspace.deleted'").
		Where("d.created_at >= ?", from).
		Where("d.created_at < ?", to).
		Where("c.actor_id IS NOT NULL")
	return s.DB.NewSelect().TableExpr("(?) AS t", deleted).
		ColumnExpr("t.user_id").
		ColumnExpr("count(*) AS workspaces").
		ColumnExpr("sum(t.invitations)::bigint AS invitations").
		ColumnExpr("max(t.deleted_at) AS last_deleted").
		GroupExpr("t.user_id").
		Having("count(*) >= ?", min)
}

// IndexThrowaways lists the accounts whose workspaces were deleted in
// [from, to) within ThrowawayWithin of being made, most first.
func (s *Store) IndexThrowaways(ctx context.Context, from, to time.Time) ([]ThrowawayRow, error) {
	rows := []ThrowawayRow{}
	if err := s.throwaways(from, to, 1).OrderExpr("workspaces DESC, last_deleted DESC").Limit(maxReviewRows).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.UserID
	}
	refs, err := s.accountRefs(ctx, ids)
	for i := range rows {
		rows[i].Account = refs[rows[i].UserID]
	}
	return rows, err
}

// CountThrowawayAccounts counts the accounts with ThrowawayWorkspaces or
// more workspaces deleted in [from, to) within ThrowawayWithin of being
// made.
func (s *Store) CountThrowawayAccounts(ctx context.Context, from, to time.Time) (int64, error) {
	return s.countGroups(ctx, s.throwaways(from, to, ThrowawayWorkspaces))
}

// CountWorkspaceEstimate is PostgreSQL's running count of live workspaces
// (pg_stat_user_tables): close, not exact, and read without counting the
// table.
func (s *Store) CountWorkspaceEstimate(ctx context.Context) (int64, error) {
	var n int64
	err := s.DB.NewSelect().TableExpr("pg_stat_user_tables").
		ColumnExpr("coalesce(max(n_live_tup), 0)").
		Where("relname = 'workspaces'").
		Where("schemaname = current_schema()").
		Scan(ctx, &n)
	return n, err
}
