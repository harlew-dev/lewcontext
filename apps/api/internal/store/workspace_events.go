package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"strings"
	"time"
)

// A workspace's Logs tab (docs/api/events.md#the-workspace-log): the events
// recorded against one workspace, as owners and admins read them. Unlike
// the admin dashboard's list this one never sees another workspace, never
// returns a payload as stored (events.WorkspaceLogDetails projects it) and
// never returns an IP, user agent or request ID.

// WorkspaceEventFilter narrows a workspace's log. httpapi validates it and
// expands group IDs: Names holds only events in the log (events.InWorkspaceLog);
// empty means all of them. Zero values don't filter.
type WorkspaceEventFilter struct {
	Names   []string
	Actor   string // usr_…: what the account did
	Subject string // usr_…: whom it is about (a member's role change, removal)
	Account string // usr_…: what they did or what was done to them
	Key     string // apk_…: the key's own lifecycle and what it wrote
	Project string // prj_…, matched in the payload
	Search  string // matched in the fields the log shows
	From    *time.Time
	To      *time.Time
	Before  string // evt_…: only rows older than this one
}

// WorkspaceLogRef names an account in the log. Name is empty for an
// account that is gone.
type WorkspaceLogRef struct {
	PublicID string `json:"id"`
	Name     string `json:"name,omitempty"`
}

// WorkspaceEvent is an event as the workspace log shows it. Actor is who
// did it (null for a workspace API key, or once the account is deleted),
// ApiKey the non-secret credential for automation, Subject whom it is
// about. Details is the projection of the payload.
type WorkspaceEvent struct {
	PublicID  string           `json:"id"`
	Name      string           `json:"name"`
	CreatedAt time.Time        `json:"createdAt"`
	Actor     *WorkspaceLogRef `json:"actor"`
	ApiKey    string           `json:"apiKey,omitempty"`
	Subject   *WorkspaceLogRef `json:"subject"`
	// RequestID is the request it was recorded in (req_…), the reference in
	// a 500's requestId and the server's logs; empty outside a request.
	// Several events can share one.
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details"`
}

// WorkspaceEventDetail is one event of the log and the others recorded in
// the same request.
type WorkspaceEventDetail struct {
	WorkspaceEvent
	Related []WorkspaceEvent `json:"related"`
}

// maxRelatedWorkspaceEvents bounds the other events of a request.
const maxRelatedWorkspaceEvents = 20

// WorkspaceEventPage is one page of the log and the cursor for the next,
// older one (nil at the end).
type WorkspaceEventPage struct {
	Data []WorkspaceEvent `json:"data"`
	Next *string          `json:"next"`
}

// WorkspaceEventRefs name the log's person and project filters, so a badge
// reads "Ada Lovelace" rather than an ID. A person is named only when they
// are, or were, in this workspace (a member, or in one of its events), so
// the filter can't be used to look up any account's name; otherwise the ID
// alone. Nil without the filter.
type WorkspaceEventRefs struct {
	Actor   *WorkspaceLogRef `json:"actor"`
	Subject *WorkspaceLogRef `json:"subject"`
	Account *WorkspaceLogRef `json:"account"`
	Project *WorkspaceLogRef `json:"project"`
	Key     *WorkspaceLogRef `json:"key"`
}

// workspaceEventRow is an event as scanned.
type workspaceEventRow struct {
	PublicID       string        `bun:"public_id"`
	Name           string        `bun:"name"`
	CreatedAt      time.Time     `bun:"created_at"`
	ActorID        sql.NullInt64 `bun:"actor_id"`
	UserID         sql.NullInt64 `bun:"user_id"`
	ApiKeyPublicID string        `bun:"api_key_public_id"`
	RequestID      string        `bun:"request_id"`
	Payload        []byte        `bun:"payload"`
}

// searchChanges are the changed fields a search matches, beside the
// payload's own text fields (events.WorkspaceLogSearchFields).
var searchChanges = []string{"name", "description", "status", "role"}

// IndexWorkspaceEvents returns up to perPage events of workspaceID matching
// f, newest first, and the cursor for the next page. ErrNoCursor when
// f.Before isn't an event of this workspace's log.
func (s *Store) IndexWorkspaceEvents(ctx context.Context, workspaceID int64, f WorkspaceEventFilter, perPage int) (WorkspaceEventPage, error) {
	q := s.workspaceLogEvents().OrderExpr(newestFirst).Limit(int64(perPage + 1))
	q, err := s.scopeWorkspaceEvents(ctx, q, workspaceID, f)
	if errors.Is(err, errNoMatch) {
		return WorkspaceEventPage{Data: []WorkspaceEvent{}}, nil
	}
	if err != nil {
		return WorkspaceEventPage{}, err
	}
	var rows []workspaceEventRow
	if err := q.Scan(ctx, &rows); err != nil {
		return WorkspaceEventPage{}, err
	}
	page := WorkspaceEventPage{}
	if len(rows) > perPage {
		rows = rows[:perPage]
		page.Next = &rows[perPage-1].PublicID
	}
	page.Data, err = s.nameWorkspaceEvents(ctx, rows)
	return page, err
}

// scopeWorkspaceEvents adds the workspace, the names of the log and f's
// conditions to q. The workspace is the column, never the payload.
func (s *Store) scopeWorkspaceEvents(ctx context.Context, q *bun.SelectQuery, workspaceID int64, f WorkspaceEventFilter) (*bun.SelectQuery, error) {
	names := f.Names
	if len(names) == 0 {
		names = events.WorkspaceLogNames()
	}
	q = q.Where("e.workspace_id = ?", workspaceID).Where("e.name IN (?)", bun.In(names))
	for column, public := range map[string]string{"e.actor_id": f.Actor, "e.user_id": f.Subject} {
		if public == "" {
			continue
		}
		var id int64
		err := s.DB.NewSelect().Model((*User)(nil)).Column("u.id").Where("u.public_id = ?", public).Scan(ctx, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return q, errNoMatch
		}
		if err != nil {
			return q, err
		}
		q = q.Where(column+" = ?", id)
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
	if f.Key != "" {
		// The key's own lifecycle events name it in the payload; what it
		// wrote carries it in its own column.
		q = q.WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where(`e.payload \? 'key' AND e.payload->>'key' = ?`, f.Key).WhereOr("e.api_key_public_id = ?", f.Key)
		})
	}
	if f.Project != "" {
		q = q.Where(`e.payload \? 'project' AND e.payload->>'project' = ?`, f.Project)
	}
	// Every word must match: an event's label or name ("api key"), a field
	// the log shows, or the name of who did it or whom it is about.
	for _, word := range strings.Fields(f.Search) {
		q = s.matchWorkspaceEventWord(q, word)
	}
	if f.From != nil {
		q = q.Where("e.created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("e.created_at < ?", *f.To)
	}
	if f.Before != "" {
		// The cursor must be one of this log's own rows, so it can't be
		// used to find out whether another workspace's event exists.
		var cursor eventRecord
		err := s.DB.NewSelect().Model(&cursor).Column("e.id", "e.created_at").
			Where("e.public_id = ?", f.Before).Where("e.workspace_id = ?", workspaceID).
			Where("e.name IN (?)", bun.In(events.WorkspaceLogNames())).Scan(ctx)
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

// matchWorkspaceEventWord requires word to be found in the event: its label
// or name, a payload field the log shows (events.WorkspaceLogSearchFields,
// the changes' values), a real invitation address (never one the retention
// rules removed), or the name of the actor or subject.
func (s *Store) matchWorkspaceEventWord(q *bun.SelectQuery, word string) *bun.SelectQuery {
	pattern := containsPattern(word)
	byName := func(column string) *bun.SelectQuery {
		return s.DB.NewSelect().Model((*User)(nil)).ColumnExpr("1").
			Where("u.id = "+column).Where(`u.name ILIKE ? ESCAPE '\'`, pattern)
	}
	return q.WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
		if names := events.WorkspaceLogNamesMatching(word); len(names) > 0 {
			q = q.WhereOr("e.name IN (?)", bun.In(names))
		}
		for _, field := range events.WorkspaceLogSearchFields() {
			q = q.WhereOr(`e.payload->>? ILIKE ? ESCAPE '\'`, field, pattern)
		}
		for _, field := range searchChanges {
			q = q.WhereOr(`e.payload #>> ?::text[] ILIKE ? ESCAPE '\'`, "{changes,"+field+",from}", pattern).
				WhereOr(`e.payload #>> ?::text[] ILIKE ? ESCAPE '\'`, "{changes,"+field+",to}", pattern)
		}
		return q.
			WhereOr(`e.payload->>'email' ILIKE ? ESCAPE '\' AND e.payload->>'email' NOT LIKE 'hmac:%' AND e.payload->>'email' <> '[deleted]'`, pattern).
			WhereOr("EXISTS (?)", byName("e.actor_id")).
			WhereOr("EXISTS (?)", byName("e.user_id"))
	})
}

// nameWorkspaceEvents turns rows into log entries, naming actors and
// subjects with one query for the page and projecting each payload.
func (s *Store) nameWorkspaceEvents(ctx context.Context, rows []workspaceEventRow) ([]WorkspaceEvent, error) {
	var ids []int64
	for _, row := range rows {
		ids = appendValid(ids, row.ActorID, row.UserID)
	}
	users, err := s.refsByID(ctx, (*User)(nil), "u", "u.public_id, u.name", ids, nil)
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceEvent, len(rows))
	for i, row := range rows {
		details, err := events.WorkspaceLogDetails(row.Name, row.Payload)
		if err != nil {
			return nil, err
		}
		out[i] = WorkspaceEvent{
			PublicID: row.PublicID, Name: row.Name, CreatedAt: row.CreatedAt,
			ApiKey: row.ApiKeyPublicID, RequestID: row.RequestID, Details: details,
			Actor: logRef(users, row.ActorID), Subject: logRef(users, row.UserID),
		}
	}
	return out, nil
}

// logRef names the account in column, or nil without one.
func logRef(users refIndex, column sql.NullInt64) *WorkspaceLogRef {
	if !column.Valid {
		return nil
	}
	r, ok := users.byID[column.Int64]
	if !ok {
		return nil
	}
	return &WorkspaceLogRef{PublicID: r.PublicID, Name: r.Name}
}

// FindWorkspaceEventRefs names the person, project and key filters of
// workspaceID's log; empty is no filter.
func (s *Store) FindWorkspaceEventRefs(ctx context.Context, workspaceID int64, f WorkspaceEventFilter) (WorkspaceEventRefs, error) {
	var refs WorkspaceEventRefs
	var err error
	for _, p := range []struct {
		id   string
		into **WorkspaceLogRef
	}{{f.Actor, &refs.Actor}, {f.Subject, &refs.Subject}, {f.Account, &refs.Account}} {
		if p.id == "" {
			continue
		}
		if *p.into, err = s.findWorkspacePerson(ctx, workspaceID, p.id); err != nil {
			return refs, err
		}
	}
	if f.Project != "" {
		refs.Project = &WorkspaceLogRef{PublicID: f.Project}
		var name string
		err := s.DB.NewSelect().Model((*Project)(nil)).Column("p.name").
			Where("p.workspace_id = ? AND p.public_id = ?", workspaceID, f.Project).Scan(ctx, &name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return refs, err
		}
		refs.Project.Name = name
	}
	if f.Key != "" {
		refs.Key = &WorkspaceLogRef{PublicID: f.Key}
		var name string
		err := s.DB.NewSelect().Model((*apiKeyRecord)(nil)).Column("ak.name").
			Where("ak.workspace_id = ? AND ak.public_id = ?", workspaceID, f.Key).Scan(ctx, &name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return refs, err
		}
		refs.Key.Name = name
	}
	return refs, nil
}

// FindWorkspaceEvent returns the event with publicID from workspaceID's log
// and the other events recorded in its request, or sql.ErrNoRows: an event
// of another workspace, one the log hides and one that doesn't exist look
// alike.
func (s *Store) FindWorkspaceEvent(ctx context.Context, workspaceID int64, publicID string) (WorkspaceEventDetail, error) {
	var row workspaceEventRow
	err := s.workspaceLogEvents().Where("e.workspace_id = ?", workspaceID).
		Where("e.name IN (?)", bun.In(events.WorkspaceLogNames())).
		Where("e.public_id = ?", publicID).Scan(ctx, &row)
	if err != nil {
		return WorkspaceEventDetail{}, err
	}
	named, err := s.nameWorkspaceEvents(ctx, []workspaceEventRow{row})
	if err != nil {
		return WorkspaceEventDetail{}, err
	}
	detail := WorkspaceEventDetail{WorkspaceEvent: named[0], Related: []WorkspaceEvent{}}
	if row.RequestID == "" {
		return detail, nil
	}
	var others []workspaceEventRow
	err = s.workspaceLogEvents().Where("e.workspace_id = ?", workspaceID).
		Where("e.name IN (?)", bun.In(events.WorkspaceLogNames())).
		Where("e.request_id = ? AND e.public_id <> ?", row.RequestID, publicID).
		OrderExpr(newestFirst).Limit(maxRelatedWorkspaceEvents).Scan(ctx, &others)
	if err != nil {
		return detail, err
	}
	if len(others) > 0 {
		detail.Related, err = s.nameWorkspaceEvents(ctx, others)
	}
	return detail, err
}

// workspaceLogEvents selects the columns a log entry is built from.
func (s *Store) workspaceLogEvents() *bun.SelectQuery {
	return s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("e.public_id, e.name, e.created_at, e.actor_id, e.user_id, e.api_key_public_id, e.request_id, e.payload")
}

// findWorkspacePerson names the account with publicID when it is a member of
// the workspace or appears in its log; the ID alone otherwise.
func (s *Store) findWorkspacePerson(ctx context.Context, workspaceID int64, publicID string) (*WorkspaceLogRef, error) {
	ref := &WorkspaceLogRef{PublicID: publicID}
	var name string
	err := s.DB.NewSelect().Model((*User)(nil)).Column("u.name").
		Where("u.public_id = ?", publicID).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.
				Where("EXISTS (?)", s.DB.NewSelect().Model((*membership)(nil)).ColumnExpr("1").Where("m.user_id = u.id AND m.workspace_id = ?", workspaceID)).
				WhereOr("EXISTS (?)", s.DB.NewSelect().Model((*eventRecord)(nil)).ColumnExpr("1").Where("e.workspace_id = ? AND (e.actor_id = u.id OR e.user_id = u.id)", workspaceID))
		}).Scan(ctx, &name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	ref.Name = name
	return ref, nil
}
