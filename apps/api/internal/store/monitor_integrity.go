package store

import (
	"context"
	"slices"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// The Integrity page's queries (docs/api/monitoring.md#integrity): things
// that should never be true, and duplicates from retries. Each check is
// one bounded query through an index, returning how many rows break it
// (counted up to MaxCheckCount) and the first MaxCheckSamples of them by
// public ID. Rows with no time of their own (workspaces, invitations) are
// checked only for the workspaces with an event in the window, as the
// Workspaces page counts them: the writes that could break them record
// events.

// Bounds of a check's answer.
const (
	// MaxCheckCount: a check counts at most this many rows, and says so.
	MaxCheckCount = 1000
	// MaxCheckSamples: a check names at most this many of them.
	MaxCheckSamples = 10
	// CleanupGrace is how long an expired row may wait for the cleanup
	// loop (every few minutes) before it's a sign the loop stopped.
	CleanupGrace = 30 * time.Minute
	// DeliveryOverdue is how long past its time a pending delivery may
	// wait before the event worker counts as stalled.
	DeliveryOverdue = 10 * time.Minute
	// DuplicateWithin: the same create twice this close together is a
	// double submit or a retry.
	DuplicateWithin = 10 * time.Second
)

// CheckResult is an integrity check as found: how many rows break it
// (Capped when there were more than MaxCheckCount) and some of them by
// public ID (or, for rows without one, what they are).
type CheckResult struct {
	Count   int64    `json:"count"`
	Capped  bool     `json:"capped,omitempty"`
	Samples []string `json:"samples"`
}

// checkRows reads a check: q selects one text column, a sample's ID (""
// for rows without one), in the order samples are named.
func checkRows(ctx context.Context, q *bun.SelectQuery) (CheckResult, error) {
	var ids []string
	if err := q.Limit(MaxCheckCount+1).Scan(ctx, &ids); err != nil {
		return CheckResult{Samples: []string{}}, err
	}
	return checkOf(ids), nil
}

// checkOf counts ids, keeping the first MaxCheckSamples distinct ones.
func checkOf(ids []string) CheckResult {
	out := CheckResult{Count: int64(len(ids)), Samples: []string{}}
	if out.Count > MaxCheckCount {
		out.Count, out.Capped = MaxCheckCount, true
	}
	for _, id := range ids {
		if id != "" && len(out.Samples) < MaxCheckSamples && !slices.Contains(out.Samples, id) {
			out.Samples = append(out.Samples, id)
		}
	}
	return out
}

// IndexActiveWorkspaces lists the internal IDs of the workspaces with an
// event in [from, to), for the workspace checks: read once per page and
// passed to each (never shown).
func (s *Store) IndexActiveWorkspaces(ctx context.Context, from, to time.Time) ([]int64, error) {
	ids := []int64{}
	err := s.activeWorkspaces(from, to).Scan(ctx, &ids)
	return ids, err
}

// CountOwnerlessWorkspaces finds the workspaces of workspaces (internal
// IDs: those with an event in the window) that have no owner: a
// membership change or deletion bug.
func (s *Store) CountOwnerlessWorkspaces(ctx context.Context, workspaces []int64) (CheckResult, error) {
	if len(workspaces) == 0 {
		return checkOf(nil), nil
	}
	owner := s.DB.NewSelect().TableExpr("workspace_user AS m").ColumnExpr("1").
		Where("m.workspace_id = w.id AND m.role = 'owner'")
	return checkRows(ctx, s.DB.NewSelect().TableExpr("workspaces AS w").
		ColumnExpr("w.public_id").
		Where("w.id = ANY(?)", pgdialect.Array(workspaces)).
		Where("NOT EXISTS (?)", owner).
		OrderExpr("w.id"))
}

// CountWorkspacesOverQuota finds the workspaces of workspaces holding
// more than a quota allows (projects; members and live invitations; live
// invitations): the quota's lock was bypassed, or the quota was lowered
// (existing rows are kept).
func (s *Store) CountWorkspacesOverQuota(ctx context.Context, workspaces []int64) (CheckResult, error) {
	if len(workspaces) == 0 {
		return checkOf(nil), nil
	}
	counts := s.DB.NewSelect().TableExpr("workspaces AS w").
		ColumnExpr("w.id, w.public_id").
		ColumnExpr("(SELECT count(*) FROM projects AS p WHERE p.workspace_id = w.id) AS projects").
		ColumnExpr("(SELECT count(*) FROM workspace_user AS m WHERE m.workspace_id = w.id) AS members").
		ColumnExpr("(SELECT count(*) FROM workspace_invitations AS i WHERE i.workspace_id = w.id AND i.expires_at > now()) AS invitations").
		Where("w.id = ANY(?)", pgdialect.Array(workspaces))
	quotas := []any{MaxWorkspaceProjects, MaxWorkspaceMembers, MaxPendingInvitations}
	return checkRows(ctx, s.DB.NewSelect().TableExpr("(?) AS q", counts).
		ColumnExpr("q.public_id").
		Where(quotaUsed+" > 100", quotas...).
		OrderExpr("q.id"))
}

// CountMemberInvitations finds live invitations, in the workspaces of
// workspaces, to someone already a member: accepting didn't consume one.
// Samples are the workspaces.
func (s *Store) CountMemberInvitations(ctx context.Context, workspaces []int64) (CheckResult, error) {
	if len(workspaces) == 0 {
		return checkOf(nil), nil
	}
	member := s.DB.NewSelect().TableExpr("users AS u").ColumnExpr("1").
		Join("JOIN workspace_user AS m ON m.user_id = u.id").
		Where("u.email = i.email AND m.workspace_id = i.workspace_id")
	return checkRows(ctx, s.DB.NewSelect().TableExpr("workspace_invitations AS i").
		ColumnExpr("w.public_id").
		Join("JOIN workspaces AS w ON w.id = i.workspace_id").
		Where("i.workspace_id = ANY(?)", pgdialect.Array(workspaces)).
		Where("i.expires_at > now()").
		Where("EXISTS (?)", member).
		OrderExpr("i.id"))
}

// CountAdminsWithoutMFA finds admins without MFA whose setup window isn't
// open (it's set by grant and by an MFA reset): a path turned MFA off for
// an admin. The admins table is a handful of rows.
func (s *Store) CountAdminsWithoutMFA(ctx context.Context) (CheckResult, error) {
	return checkRows(ctx, s.DB.NewSelect().Model((*adminRecord)(nil)).
		ColumnExpr("u.public_id").
		Join("JOIN users AS u ON u.id = adm.user_id").
		Where("(?) IS NULL", mfaEnabledAt(s.DB)).
		Where("adm.mfa_setup_until IS NULL OR adm.mfa_setup_until <= now()").
		OrderExpr("adm.user_id"))
}

// expiringTables are the tables the cleanup loop (Store.Cleanup) empties
// of expired rows, each with an index on expires_at. Fixed names, never
// input.
var expiringTables = []string{
	"sessions", "admin_sessions", "mfa_challenges", "password_resets",
	"pending_registrations", "email_verifications", "email_block_links",
}

// CountExpiredRows counts rows that expired over CleanupGrace ago in each
// table the cleanup loop empties (through its expires_at index): if there
// are any, the loop isn't running. Samples are the tables holding them.
// Sessions ended by being idle are left out: last_seen_at has no index.
func (s *Store) CountExpiredRows(ctx context.Context) (CheckResult, error) {
	out := CheckResult{Samples: []string{}}
	for _, table := range expiringTables {
		expired := s.DB.NewSelect().TableExpr("? AS t", bun.Ident(table)).ColumnExpr("1").
			Where("t.expires_at < now() - make_interval(secs => ?)", CleanupGrace.Seconds()).
			Limit(MaxCheckCount + 1)
		var n int64
		if err := s.DB.NewSelect().TableExpr("(?) AS x", expired).ColumnExpr("count(*)").Scan(ctx, &n); err != nil {
			return out, err
		}
		if n > 0 {
			out.Count += n
			out.Samples = append(out.Samples, table)
		}
	}
	if out.Count > MaxCheckCount {
		out.Count, out.Capped = MaxCheckCount, true
	}
	return out, nil
}

// CountEndedLimitWindows counts rate-limit windows that ended over
// CleanupGrace ago (rate_limits_resets_at_idx): the cleanup loop isn't
// running. Their keys are digests, so nothing is named.
func (s *Store) CountEndedLimitWindows(ctx context.Context) (CheckResult, error) {
	return checkRows(ctx, s.DB.NewSelect().Model((*rateLimitRecord)(nil)).
		ColumnExpr("''").
		Where("l.resets_at < now() - make_interval(secs => ?)", CleanupGrace.Seconds()))
}

// CountOverdueDeliveries finds pending deliveries whose time passed
// DeliveryOverdue ago (event_deliveries_pending_idx), oldest first: the
// event worker is stalled, or every server is down. Samples are their
// events.
func (s *Store) CountOverdueDeliveries(ctx context.Context) (CheckResult, error) {
	return checkRows(ctx, s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		ColumnExpr("e.public_id").
		Join("JOIN events AS e ON e.id = d.event_id").
		Where("d.delivered_at IS NULL AND d.failed_at IS NULL").
		Where("d.next_attempt_at < now() - make_interval(secs => ?)", DeliveryOverdue.Seconds()).
		OrderExpr("d.next_attempt_at, d.event_id"))
}

// CountUnmatchedDeliveries finds deliveries that failed in [from, to)
// because no listener had their name (event_deliveries_failed_idx): a
// listener renamed with deliveries queued. Samples are their events.
func (s *Store) CountUnmatchedDeliveries(ctx context.Context, from, to time.Time) (CheckResult, error) {
	return checkRows(ctx, s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		ColumnExpr("e.public_id").
		Join("JOIN events AS e ON e.id = d.event_id").
		Where("d.failed_at >= ? AND d.failed_at < ?", from, to).
		Where("d.last_error LIKE 'no listener %'").
		OrderExpr("d.failed_at DESC"))
}

// CountUnknownEventNames finds events in [from, to) whose name isn't one
// of names, the catalogue (events_created_idx holds the name): old code, or
// a write outside the store's record. Samples are the events.
func (s *Store) CountUnknownEventNames(ctx context.Context, from, to time.Time, names []string) (CheckResult, error) {
	return checkRows(ctx, s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("e.public_id").
		Where("e.created_at >= ? AND e.created_at < ?", from, to).
		Where("e.name NOT IN (?)", bun.In(names)).
		OrderExpr("e.created_at DESC"))
}

// Duplicates and retries: whether creates and sends happen twice.

// duplicateCreates are the creates a double submit or a retry would
// repeat.
var duplicateCreates = []string{"workspace.created", "project.created", "workspace.invitation.created"}

// duplicateKey is what makes two creates the same: a workspace's name; a
// project's name or an invitation's email, in its workspace. A fixed
// expression, never input.
const duplicateKey = "CASE WHEN e.name = 'workspace.created' THEN lower(e.payload->>'name') " +
	"ELSE (e.payload->>'workspace') || '/' || lower(coalesce(e.payload->>'name', e.payload->>'email')) END"

// Duplicates are near-duplicate creates in a window: how many, how many of
// each name, and the later event of some.
type Duplicates struct {
	CheckResult
	ByName []NameCount `json:"byName"`
}

// CountDuplicateCreates finds creates (duplicateCreates) in [from, to)
// recorded by the same actor for the same name or email within
// DuplicateWithin of the one before (events_name_created_idx): a double
// submit, or a client retrying a create that had succeeded. Each later
// one counts once.
func (s *Store) CountDuplicateCreates(ctx context.Context, from, to time.Time) (Duplicates, error) {
	creates := s.eventsIn(from, to, duplicateCreates...).
		ColumnExpr("e.public_id, e.name, e.created_at").
		ColumnExpr("lag(e.created_at) OVER (PARTITION BY e.actor_id, e.name, " + duplicateKey + " ORDER BY e.created_at, e.id) AS previous").
		Where("e.actor_id IS NOT NULL")
	var rows []struct {
		PublicID string `bun:"public_id"`
		Name     string `bun:"name"`
	}
	err := s.DB.NewSelect().TableExpr("(?) AS x", creates).
		ColumnExpr("x.public_id, x.name").
		Where("x.created_at - x.previous <= make_interval(secs => ?)", DuplicateWithin.Seconds()).
		OrderExpr("x.created_at DESC").
		Limit(MaxCheckCount+1).
		Scan(ctx, &rows)
	ids := make([]string, len(rows))
	byName := map[string]int{}
	for i, r := range rows {
		ids[i] = r.PublicID
		byName[r.Name]++
	}
	out := Duplicates{CheckResult: checkOf(ids), ByName: []NameCount{}}
	for _, name := range duplicateCreates {
		if byName[name] > 0 {
			out.ByName = append(out.ByName, NameCount{Name: name, Count: byName[name]})
		}
	}
	return out, err
}

// CountDuplicateSends finds requests (a reset or invitation's created
// event, by its evt_ ID) sent more than once in [from, to): at-least-once
// delivery sending twice. Samples are the requests.
func (s *Store) CountDuplicateSends(ctx context.Context, from, to time.Time) (CheckResult, error) {
	return checkRows(ctx, s.eventsIn(from, to, "auth.password_reset.sent", "workspace.invitation.sent").
		ColumnExpr("e.payload->>'request' AS request").
		Where(`e.payload \? 'request'`).
		GroupExpr("e.name, e.payload->>'request'").
		Having("count(*) > 1").
		OrderExpr("max(e.created_at) DESC"))
}

// CountRetriedDeliveries finds deliveries of events recorded in [from, to)
// that were delivered on a later attempt: the attempt before may have sent
// too (a timeout after the mail went out). The first attempt's error isn't
// kept once one succeeds, so a timeout can't be told from another failure.
// Read from the window's first event on through event_deliveries' key.
// Samples are their events.
func (s *Store) CountRetriedDeliveries(ctx context.Context, from, to time.Time) (CheckResult, error) {
	first := s.DB.NewSelect().Model((*eventRecord)(nil)).Column("e.id").
		Where("e.created_at >= ?", from).
		OrderExpr("e.created_at").Limit(1)
	return checkRows(ctx, s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		ColumnExpr("e.public_id").
		Join("JOIN events AS e ON e.id = d.event_id").
		Where("d.event_id >= (?)", first).
		Where("d.delivered_at IS NOT NULL AND d.attempts > 1").
		Where("e.created_at < ?", to).
		OrderExpr("d.event_id DESC"))
}

// RouteCount is a count for a route pattern.
type RouteCount struct {
	Route string `json:"route" bun:"route"`
	Count int64  `json:"count" bun:"count"`
}

// IndexTimedOutWrites lists the mutating routes (POST, PATCH, DELETE)
// whose requests the 10 s deadline ended in [from, to), most first: the
// client saw an error, but the change may have committed, and a retry then
// duplicates it.
func (s *Store) IndexTimedOutWrites(ctx context.Context, from, to time.Time) ([]RouteCount, error) {
	rows, err := s.requestRows(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := []RouteCount{}
	err = s.DB.NewSelect().TableExpr("(?) AS x", rows("m.route, m.timeouts")).
		ColumnExpr("x.route, sum(x.timeouts)::bigint AS count").
		Where("x.route LIKE 'POST %' OR x.route LIKE 'PATCH %' OR x.route LIKE 'DELETE %'").
		GroupExpr("x.route").
		Having("sum(x.timeouts) > 0").
		OrderExpr("count DESC, x.route").
		Limit(maxReviewRows).
		Scan(ctx, &out)
	return out, err
}
