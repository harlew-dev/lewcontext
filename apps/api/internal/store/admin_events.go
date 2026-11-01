package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/uptrace/bun"
	"time"
)

// The admin dashboard's Events page: the audit trail, filtered and paged by
// a cursor. Like admin_views.go, these queries see every account and
// workspace, so they're only reachable behind the admin key and an admin
// session. Every filter has an index (migration 007), and nothing is
// counted: a page is the rows older than its cursor.

// AdminEventFilter narrows the Events list and its summary. httpapi
// validates and expands it: Names holds catalogue names (groups already
// expanded), Hide the admin reads to leave out when no name was picked.
// Zero values don't filter.
type AdminEventFilter struct {
	Names     []string
	Hide      []string
	Failed    bool   // only events recording a failed check (adminEventFailures)
	Account   string // usr_…: what the account did (actor) or is about (user)
	Workspace string // wsp_…, matched in the payload, so it outlives the workspace
	Email     string // lower-case, matched with the payload's email
	IP        string // an address or a CIDR, matched with events.ip
	From, To  *time.Time
	Before    string // evt_…: only rows older than this one
}

// ErrNoCursor means a cursor names an event that doesn't exist (pruned, or
// made up).
var ErrNoCursor = errors.New("unknown cursor")

// AdminRef names an account or workspace inside an event. Name and Email
// are empty once it's deleted: only the public ID from the payload is left.
type AdminRef struct {
	PublicID string `json:"id"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
}

// AdminEvent is an event as the Events list shows it. Actor is who acted,
// User the account it is about and Workspace the workspace, each read from
// its column or, once that's empty, from the payload. Failed: it records a
// failed check (adminEventFailures).
type AdminEvent struct {
	ID        int64           `json:"-"`
	PublicID  string          `json:"id"`
	Name      string          `json:"name"`
	CreatedAt time.Time       `json:"createdAt"`
	Failed    bool            `json:"failed"`
	Actor     *AdminRef       `json:"actor"`
	User      *AdminRef       `json:"user"`
	Workspace *AdminRef       `json:"workspace"`
	IP        *string         `json:"ip"`
	Payload   json.RawMessage `json:"payload"`
}

// AdminEventPage is one page of events and the cursor for the next, older
// one (nil at the end).
type AdminEventPage struct {
	Data []AdminEvent `json:"data"`
	Next *string      `json:"next"`
}

// AdminDelivery is one listener's delivery of an event.
type AdminDelivery struct {
	Listener      string     `json:"listener"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt *time.Time `json:"nextAttemptAt"`
	DeliveredAt   *time.Time `json:"deliveredAt"`
	FailedAt      *time.Time `json:"failedAt"`
	LastError     string     `json:"lastError"`
}

// AdminEventDetail is one event's page: the row, its user agent, its
// deliveries, and related events (the one its request field names, and
// those naming it).
type AdminEventDetail struct {
	AdminEvent
	UserAgent  string          `json:"userAgent"`
	Deliveries []AdminDelivery `json:"deliveries"`
	Related    []AdminEvent    `json:"related"`
}

// maxRelatedEvents bounds the related events a detail lists.
const maxRelatedEvents = 20

// adminEventRow is an event as scanned, before its references are named.
type adminEventRow struct {
	ID          int64           `bun:"id"`
	PublicID    string          `bun:"public_id"`
	Name        string          `bun:"name"`
	CreatedAt   time.Time       `bun:"created_at"`
	Failed      bool            `bun:"failed"`
	ActorID     sql.NullInt64   `bun:"actor_id"`
	UserID      sql.NullInt64   `bun:"user_id"`
	WorkspaceID sql.NullInt64   `bun:"workspace_id"`
	IP          sql.NullString  `bun:"ip"`
	Payload     json.RawMessage `bun:"payload"`
	UserAgent   string          `bun:"user_agent"`
}

// adminEvents selects events with their columns as the dashboard reads them.
func adminEvents(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("e.id, e.public_id, e.name, e.created_at, e.actor_id, e.user_id, e.workspace_id").
		ColumnExpr("host(e.ip) AS ip, e.payload, e.user_agent").
		ColumnExpr("? AS failed", failedCondition())
}

// newestFirst orders events newest first; the id breaks ties, so rows
// sharing a created_at never repeat or go missing across pages.
const newestFirst = "e.created_at DESC, e.id DESC"

// IndexAdminEvents returns up to perPage events matching f, newest first,
// and the cursor for the next page. ErrNoCursor when f.Before is unknown.
func (s *Store) IndexAdminEvents(ctx context.Context, f AdminEventFilter, perPage int) (AdminEventPage, error) {
	q := adminEvents(s.DB).OrderExpr(newestFirst).Limit(int64(perPage + 1))
	q, err := s.filterAdminEvents(ctx, q, f)
	if errors.Is(err, errNoMatch) {
		return AdminEventPage{Data: []AdminEvent{}}, nil
	}
	if err != nil {
		return AdminEventPage{}, err
	}
	var rows []adminEventRow
	if err := q.Scan(ctx, &rows); err != nil {
		return AdminEventPage{}, err
	}
	page := AdminEventPage{}
	if len(rows) > perPage {
		rows = rows[:perPage]
		page.Next = &rows[perPage-1].PublicID
	}
	page.Data, err = s.nameEventRefs(ctx, rows)
	return page, err
}

// errNoMatch means a filter can match nothing (an unknown account), so the
// query needn't run.
var errNoMatch = errors.New("filter matches nothing")

// filterAdminEvents adds f's conditions to q.
func (s *Store) filterAdminEvents(ctx context.Context, q *bun.SelectQuery, f AdminEventFilter) (*bun.SelectQuery, error) {
	if len(f.Names) > 0 {
		q = q.Where("e.name IN (?)", bun.In(f.Names))
	}
	if len(f.Hide) > 0 {
		q = q.Where("e.name NOT IN (?)", bun.In(f.Hide))
	}
	// The names first, so the planner can use the name index for them.
	if f.Failed {
		q = q.Where("e.name IN (?)", bun.In(FailureNames())).Where("?", failedCondition())
	}
	if f.Account != "" {
		var id int64
		err := s.DB.NewSelect().Model((*User)(nil)).Column("u.id").Where("u.public_id = ?", f.Account).Scan(ctx, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return q, errNoMatch
		}
		if err != nil {
			return q, err
		}
		q = q.Where("(e.actor_id = ? OR e.user_id = ?)", id, id)
	}
	// The payload conditions repeat the partial indexes' predicates, so the
	// planner can use them.
	if f.Workspace != "" {
		q = q.Where(`e.payload \? 'workspace' AND e.payload->>'workspace' = ?`, f.Workspace)
	}
	if f.Email != "" {
		q = q.Where(`e.payload \? 'email' AND lower(e.payload->>'email') = ?`, f.Email)
	}
	if f.IP != "" {
		q = q.Where("e.ip IS NOT NULL AND e.ip <<= ?::inet", f.IP)
	}
	if f.From != nil {
		q = q.Where("e.created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("e.created_at < ?", *f.To)
	}
	if f.Before != "" {
		var cursor eventRecord
		err := s.DB.NewSelect().Model(&cursor).Column("e.id", "e.created_at").Where("e.public_id = ?", f.Before).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return q, ErrNoCursor
		}
		if err != nil {
			return q, err
		}
		q = q.Where("(e.created_at, e.id) < (?, ?)", cursor.CreatedAt, cursor.ID)
	}
	return q, nil
}

// FindAdminEvent returns the event with publicID, its deliveries and related
// events, or sql.ErrNoRows.
func (s *Store) FindAdminEvent(ctx context.Context, publicID string) (AdminEventDetail, error) {
	var row adminEventRow
	if err := adminEvents(s.DB).Where("e.public_id = ?", publicID).Scan(ctx, &row); err != nil {
		return AdminEventDetail{}, err
	}
	named, err := s.nameEventRefs(ctx, []adminEventRow{row})
	if err != nil {
		return AdminEventDetail{}, err
	}
	d := AdminEventDetail{AdminEvent: named[0], UserAgent: row.UserAgent}
	if d.Deliveries, err = s.deliveriesOf(ctx, row.ID); err != nil {
		return d, err
	}
	d.Related, err = s.relatedEvents(ctx, row)
	return d, err
}

// relatedEvents are the event named in row's request field and the events
// naming row in theirs (a sent event names its request), newest first.
func (s *Store) relatedEvents(ctx context.Context, row adminEventRow) ([]AdminEvent, error) {
	var payload struct {
		Request string `json:"request"`
	}
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return nil, err
	}
	q := adminEvents(s.DB).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			q = q.Where(`e.payload \? 'request' AND e.payload->>'request' = ?`, row.PublicID)
			if payload.Request != "" {
				q = q.WhereOr("e.public_id = ?", payload.Request)
			}
			return q
		}).
		OrderExpr(newestFirst).
		Limit(maxRelatedEvents)
	var rows []adminEventRow
	if err := q.Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return s.nameEventRefs(ctx, rows)
}

// deliveriesOf lists an event's deliveries by listener.
func (s *Store) deliveriesOf(ctx context.Context, eventID int64) ([]AdminDelivery, error) {
	var rows []deliveryRecord
	err := s.DB.NewSelect().Model(&rows).Where("d.event_id = ?", eventID).OrderExpr("d.listener").Scan(ctx)
	deliveries := make([]AdminDelivery, len(rows))
	for i, r := range rows {
		deliveries[i] = adminDelivery(r)
	}
	return deliveries, err
}

// eventRefs are the public IDs an event's payload may name, for references
// whose column is empty.
type eventRefs struct {
	Admin     string `json:"admin"`
	User      string `json:"user"`
	Workspace string `json:"workspace"`
}

// nameEventRefs turns rows into AdminEvents, naming their actor, user and
// workspace with two queries for the whole page rather than one per row.
func (s *Store) nameEventRefs(ctx context.Context, rows []adminEventRow) ([]AdminEvent, error) {
	refs := make([]eventRefs, len(rows))
	var userIDs, workspaceIDs []int64
	var userPublicIDs, workspacePublicIDs []string
	for i, row := range rows {
		if err := json.Unmarshal(row.Payload, &refs[i]); err != nil {
			return nil, err
		}
		userIDs = appendValid(userIDs, row.ActorID, row.UserID)
		workspaceIDs = appendValid(workspaceIDs, row.WorkspaceID)
		userPublicIDs = append(userPublicIDs, refs[i].Admin, refs[i].User)
		workspacePublicIDs = append(workspacePublicIDs, refs[i].Workspace)
	}
	users, err := s.refsByID(ctx, (*User)(nil), "u", "u.public_id, u.name, u.email", userIDs, userPublicIDs)
	if err != nil {
		return nil, err
	}
	workspaces, err := s.refsByID(ctx, (*Workspace)(nil), "w", "w.public_id, w.name", workspaceIDs, workspacePublicIDs)
	if err != nil {
		return nil, err
	}
	out := make([]AdminEvent, len(rows))
	for i, row := range rows {
		out[i] = AdminEvent{
			ID: row.ID, PublicID: row.PublicID, Name: row.Name, CreatedAt: row.CreatedAt, Failed: row.Failed, Payload: row.Payload,
			Actor:     users.ref(row.ActorID, refs[i].Admin),
			User:      users.ref(row.UserID, refs[i].User),
			Workspace: workspaces.ref(row.WorkspaceID, refs[i].Workspace),
		}
		if row.IP.Valid {
			out[i].IP = &row.IP.String
		}
	}
	return out, nil
}

// appendValid appends the non-null IDs.
func appendValid(ids []int64, columns ...sql.NullInt64) []int64 {
	for _, c := range columns {
		if c.Valid {
			ids = append(ids, c.Int64)
		}
	}
	return ids
}

// refIndex finds named records by internal or public ID.
type refIndex struct {
	byID       map[int64]AdminRef
	byPublicID map[string]AdminRef
}

// ref names the record in column, else the one with publicID from the
// payload (by ID alone once it's deleted), else nil.
func (x refIndex) ref(column sql.NullInt64, publicID string) *AdminRef {
	if column.Valid {
		if r, ok := x.byID[column.Int64]; ok {
			return &r
		}
	}
	if publicID == "" {
		return nil
	}
	if r, ok := x.byPublicID[publicID]; ok {
		return &r
	}
	return &AdminRef{PublicID: publicID}
}

// refsByID loads the records of model (users or workspaces) with any of ids
// or publicIDs, selecting columns (public_id, name and, for users, email).
func (s *Store) refsByID(ctx context.Context, model any, alias, columns string, ids []int64, publicIDs []string) (refIndex, error) {
	x := refIndex{byID: map[int64]AdminRef{}, byPublicID: map[string]AdminRef{}}
	publicIDs = nonEmpty(publicIDs)
	if len(ids) == 0 && len(publicIDs) == 0 {
		return x, nil
	}
	var rows []struct {
		ID       int64  `bun:"id"`
		PublicID string `bun:"public_id"`
		Name     string `bun:"name"`
		Email    string `bun:"email"`
	}
	q := s.DB.NewSelect().Model(model).ColumnExpr(alias+".id").ColumnExpr(columns).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			if len(ids) > 0 {
				q = q.WhereOr(alias+".id IN (?)", bun.In(ids))
			}
			if len(publicIDs) > 0 {
				q = q.WhereOr(alias+".public_id IN (?)", bun.In(publicIDs))
			}
			return q
		})
	if err := q.Scan(ctx, &rows); err != nil {
		return x, err
	}
	for _, r := range rows {
		ref := AdminRef{PublicID: r.PublicID, Name: r.Name, Email: r.Email}
		x.byID[r.ID] = ref
		x.byPublicID[r.PublicID] = ref
	}
	return x, nil
}

// nonEmpty drops empty strings.
func nonEmpty(values []string) []string {
	out := values[:0:0]
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// AdminEventRefs name the Events list's account and workspace filters, so
// the page reads "Account: Ada Lovelace" rather than an ID: the ID and name
// only. Nil without the filter; the ID alone once the record is deleted, or
// when it never existed. They name the filtered account or workspace,
// whether or not rows match, and the read records the filter.
type AdminEventRefs struct {
	Account   *AdminRef `json:"account"`
	Workspace *AdminRef `json:"workspace"`
}

// FindAdminEventRefs names account (usr_…) and workspace (wsp_…); "" is no
// filter.
func (s *Store) FindAdminEventRefs(ctx context.Context, account, workspace string) (AdminEventRefs, error) {
	var refs AdminEventRefs
	if account != "" {
		// The name only: the badge needs nothing more (the least data).
		users, err := s.refsByID(ctx, (*User)(nil), "u", "u.public_id, u.name", nil, []string{account})
		if err != nil {
			return refs, err
		}
		refs.Account = users.ref(sql.NullInt64{}, account)
	}
	if workspace != "" {
		workspaces, err := s.refsByID(ctx, (*Workspace)(nil), "w", "w.public_id, w.name", nil, []string{workspace})
		if err != nil {
			return refs, err
		}
		refs.Workspace = workspaces.ref(sql.NullInt64{}, workspace)
	}
	return refs, nil
}
