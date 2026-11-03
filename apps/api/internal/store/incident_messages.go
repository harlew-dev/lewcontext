package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
)

// MaxIncidentMessages includes redacted tombstones in the per-cycle quota.
const MaxIncidentMessages = 1000

// IncidentMessage is plain accepted text, or a redacted tombstone. Person
// fields are current joins, never identity snapshots stored with the text.
type IncidentMessage struct {
	bun.BaseModel `bun:"table:incident_messages,alias:im"`
	ID            int64           `json:"-" bun:",pk,autoincrement"`
	PublicID      string          `json:"id" bun:",nullzero"`
	CycleID       int64           `json:"-"`
	AuthorID      *int64          `json:"-"`
	Body          *string         `json:"body"`
	CreatedAt     time.Time       `json:"createdAt" bun:",nullzero"`
	RedactedAt    *time.Time      `json:"redactedAt"`
	RedactedBy    *int64          `json:"-"`
	Author        *IncidentPerson `json:"author" bun:"-"`
	Redactor      *IncidentPerson `json:"redactor" bun:"-"`
}

// IncidentMessagePage is newest-first comments; clients render each page in
// chronological order. A cursor belongs to exactly the selected cycle.
type IncidentMessagePage struct {
	Data []IncidentMessage `json:"data"`
	Next *string           `json:"next"`
}

var errIncidentBody = errors.New("comment must contain 1 to 2,000 valid characters")

func nameIncidentMessages(ctx context.Context, db bun.IDB, rows []IncidentMessage) error {
	ids := []int64{}
	for _, row := range rows {
		for _, id := range []*int64{row.AuthorID, row.RedactedBy} {
			if id != nil {
				ids = append(ids, *id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var users []User
	if err := db.NewSelect().Model(&users).Column("id", "public_id", "name").Where("u.id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return err
	}
	people := map[int64]*IncidentPerson{}
	for _, u := range users {
		people[u.ID] = &IncidentPerson{ID: u.PublicID, Name: u.Name}
	}
	for i := range rows {
		if rows[i].AuthorID != nil {
			rows[i].Author = people[*rows[i].AuthorID]
		}
		if rows[i].RedactedBy != nil {
			rows[i].Redactor = people[*rows[i].RedactedBy]
		}
	}
	return nil
}

func findIncidentCycle(ctx context.Context, db bun.IDB, parent IncidentParent, cycle string, lock bool) (IncidentCycle, error) {
	var out IncidentCycle
	q := incidentCycles(db, parent).Where("ic.public_id = ?", cycle)
	if lock {
		q = q.For("UPDATE")
	}
	err := q.Scan(ctx, &out)
	return out, err
}

// IndexIncidentMessages scopes parent, cycle and cursor before reading text.
func (s *Store) IndexIncidentMessages(ctx context.Context, target IncidentTarget, cycle, before string, perPage int) (IncidentMessagePage, error) {
	parent, err := findIncidentParent(ctx, s.DB, target, false)
	if err != nil {
		return IncidentMessagePage{}, err
	}
	selected, err := findIncidentCycle(ctx, s.DB, parent, cycle, false)
	if err != nil {
		return IncidentMessagePage{}, err
	}
	q := s.DB.NewSelect().Model((*IncidentMessage)(nil)).Where("im.cycle_id = ?", selected.ID).OrderExpr("im.created_at DESC, im.id DESC").Limit(int64(perPage + 1))
	if before != "" {
		var cursor IncidentMessage
		err := s.DB.NewSelect().Model(&cursor).Where("im.cycle_id = ? AND im.public_id = ?", selected.ID, before).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return IncidentMessagePage{}, ErrNoCursor
		}
		if err != nil {
			return IncidentMessagePage{}, err
		}
		q = q.Where("(im.created_at, im.id) < (?, ?)", cursor.CreatedAt, cursor.ID)
	}
	page := IncidentMessagePage{Data: []IncidentMessage{}}
	if err := q.Scan(ctx, &page.Data); err != nil {
		return page, err
	}
	if len(page.Data) > perPage {
		page.Data = page.Data[:perPage]
		id := page.Data[perPage-1].PublicID
		page.Next = &id
	}
	return page, nameIncidentMessages(ctx, s.DB, page.Data)
}

// CreateIncidentMessage permits discussion on an old valid cycle, but never
// changes lifecycle state. Parent then cycle locking serialises quota/pruning.
func (s *Store) CreateIncidentMessage(ctx context.Context, target IncidentTarget, cycle, body string, admin User) (IncidentMessage, error) {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > 2000 || !utf8.ValidString(body) {
		return IncidentMessage{}, errIncidentBody
	}
	var out IncidentMessage
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		parent, err := findIncidentParent(ctx, tx, target, true)
		if err != nil {
			return err
		}
		selected, err := findIncidentCycle(ctx, tx, parent, cycle, true)
		if err != nil {
			return err
		}
		n, err := tx.NewSelect().Model((*IncidentMessage)(nil)).Where("im.cycle_id = ?", selected.ID).Count(ctx)
		if err != nil {
			return err
		}
		if n >= MaxIncidentMessages {
			return QuotaError{What: "comments in an incident cycle", Max: MaxIncidentMessages}
		}
		out = IncidentMessage{CycleID: selected.ID, AuthorID: &admin.ID, Body: &body}
		if err := tx.NewInsert().Model(&out).Returning("*").Scan(ctx); err != nil {
			return err
		}
		adminID, cycleID, message := admin.PublicID, cycle, out.PublicID
		var event events.Event
		if target.Kind == "alert" {
			event = events.AdminAlertCommented{Admin: adminID, Cycle: cycleID, Message: message, Alert: target.PublicID}
		} else {
			event = events.AdminErrorCommented{Admin: adminID, Cycle: cycleID, Message: message, Error: target.PublicID}
		}
		return s.record(ctx, tx, event, EventScope{ActorID: admin.ID})
	})
	out.Author = &IncidentPerson{ID: admin.PublicID, Name: admin.Name}
	return out, err
}

// UpdateIncidentMessageRedaction nulls text in place with an audited reason,
// retaining the historical resolution reference and never copying the body.
func (s *Store) UpdateIncidentMessageRedaction(ctx context.Context, target IncidentTarget, message string, a AdminAction) (IncidentMessage, error) {
	var out IncidentMessage
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		parent, err := findIncidentParent(ctx, tx, target, true)
		if err != nil {
			return err
		}
		var selected IncidentCycle
		err = incidentCycles(tx, parent).Where("ic.id IN (?)", tx.NewSelect().Model((*IncidentMessage)(nil)).Column("cycle_id").Where("im.public_id = ?", message)).For("UPDATE").Scan(ctx, &selected)
		if err != nil {
			return err
		}
		err = tx.NewSelect().Model(&out).Where("im.public_id = ? AND im.cycle_id = ?", message, selected.ID).For("UPDATE").Scan(ctx)
		if err != nil {
			return err
		}
		if out.RedactedAt != nil {
			return ErrIncidentConflict
		}
		if err := tx.NewUpdate().Model(&out).Set("body = NULL").Set("redacted_at = clock_timestamp()").Set("redacted_by = ?", a.Admin.ID).WherePK().Returning("*").Scan(ctx); err != nil {
			return err
		}
		out.Body = nil // the row's body is NULL now; never keep the removed text in memory
		adminID, cycleID, message := a.Admin.PublicID, selected.PublicID, out.PublicID
		var event events.Event
		if target.Kind == "alert" {
			event = events.AdminAlertRedacted{Admin: adminID, Cycle: cycleID, Message: message, Alert: target.PublicID, Reason: a.Reason}
		} else {
			event = events.AdminErrorRedacted{Admin: adminID, Cycle: cycleID, Message: message, Error: target.PublicID, Reason: a.Reason}
		}
		return s.record(ctx, tx, event, EventScope{ActorID: a.Admin.ID})
	})
	rows := []IncidentMessage{out}
	if err == nil {
		err = nameIncidentMessages(ctx, s.DB, rows)
	}
	return rows[0], err
}
