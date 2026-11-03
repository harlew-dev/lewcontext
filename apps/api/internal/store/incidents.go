package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// ErrIncidentConflict means the selected incident or comment changed. Reload;
// never automatically apply a decision to a replacement cycle.
var ErrIncidentConflict = errors.New("incident changed; reload before resolving")

// IncidentParent is exactly one internal parent key, obtained from a scoped
// lookup. Writers always lock the parent before its cycle and message.
type IncidentParent struct {
	AlertID    int64
	AppErrorID int64
}

// IncidentTarget identifies a thread at the admin boundary, by public ID only.
type IncidentTarget struct {
	Kind     string
	PublicID string
}

// IncidentPerson is current account attribution, preserved across admin revocation.
type IncidentPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// IncidentCycle is one opening through resolution; never a copy of comment text.
type IncidentCycle struct {
	AlertHealth
	Level               string     `json:"level" bun:",nullzero,default:'warn'"`
	PeakValue           float64    `json:"peakValue"`
	AcknowledgedAt      *time.Time `json:"acknowledgedAt"`
	AcknowledgedBy      *int64     `json:"-"`
	Notified            string     `json:"notified" bun:",nullzero,default:'quiet'"`
	ReminderAnchor      time.Time  `json:"-" bun:",nullzero,default:current_timestamp"`
	bun.BaseModel       `bun:"table:incident_cycles,alias:ic"`
	ID                  int64           `json:"-" bun:",pk,autoincrement"`
	PublicID            string          `json:"id" bun:",nullzero"`
	AlertID             int64           `json:"-" bun:",nullzero"`
	AppErrorID          int64           `json:"-" bun:",nullzero"`
	Number              int             `json:"number"`
	OpenedAt            time.Time       `json:"openedAt"`
	ResolvedAt          *time.Time      `json:"resolvedAt"`
	ResolutionSource    *string         `json:"resolutionSource"`
	ResolvedBy          *int64          `json:"-"`
	ResolutionMessageID *int64          `json:"-"`
	Resolver            *IncidentPerson `json:"resolver" bun:"-"`
	ResolutionMessage   *string         `json:"resolutionMessage" bun:"-"`
}

// IncidentThread is the newest cycle summaries included in a parent detail.
type IncidentThread struct {
	CurrentCycle IncidentCycle   `json:"currentCycle"`
	Cycles       []IncidentCycle `json:"cycles"`
	NextCycle    *string         `json:"nextCycle"`
}

// IncidentCyclePage is newest-first cycle history without message bodies.
type IncidentCyclePage struct {
	Data []IncidentCycle `json:"data"`
	Next *string         `json:"next"`
}

func incidentCycles(db bun.IDB, parent IncidentParent) *bun.SelectQuery {
	q := db.NewSelect().Model((*IncidentCycle)(nil))
	if parent.AlertID != 0 && parent.AppErrorID == 0 {
		return q.Where("ic.alert_id = ?", parent.AlertID)
	}
	if parent.AppErrorID != 0 && parent.AlertID == 0 {
		return q.Where("ic.app_error_id = ?", parent.AppErrorID)
	}
	return q.Where("FALSE")
}

// CreateIncidentCycle appends a cycle in the caller's transaction. The caller
// MUST already hold the parent row lock (and alert advisory lock for lifecycle
// changes). Initial parent creation and this insert commit together.
func CreateIncidentCycle(ctx context.Context, tx bun.Tx, parent IncidentParent, openedAt time.Time) (IncidentCycle, error) {
	var number int
	if err := incidentCycles(tx, parent).ColumnExpr("coalesce(max(ic.number), 0) + 1").Scan(ctx, &number); err != nil {
		return IncidentCycle{}, err
	}
	row := IncidentCycle{AlertID: parent.AlertID, AppErrorID: parent.AppErrorID, Number: number, OpenedAt: openedAt}
	err := tx.NewInsert().Model(&row).Returning("*").Scan(ctx)
	return row, err
}

// FindCurrentIncidentCycle reads the highest numbered cycle. Lock requires the
// caller to hold the parent lock and use its transaction.
func FindCurrentIncidentCycle(ctx context.Context, db bun.IDB, parent IncidentParent, lock bool) (IncidentCycle, error) {
	var out IncidentCycle
	q := incidentCycles(db, parent).OrderExpr("ic.number DESC").Limit(1)
	if lock {
		q = q.For("UPDATE")
	}
	err := q.Scan(ctx, &out)
	return out, err
}

// ResolveIncidentCycle validates scoped references and closes the current open
// cycle. It does not change its parent or record an event: the lifecycle owner
// does both in this same transaction after its own state/precondition checks.
func ResolveIncidentCycle(ctx context.Context, tx bun.Tx, parent IncidentParent, cycle, message string, adminID int64, at time.Time) (IncidentCycle, error) {
	var selected IncidentCycle
	if err := incidentCycles(tx, parent).Where("ic.public_id = ?", cycle).For("UPDATE").Scan(ctx, &selected); err != nil {
		return selected, err
	}
	var msg IncidentMessage
	if err := tx.NewSelect().Model(&msg).Where("im.cycle_id = ? AND im.public_id = ?", selected.ID, message).For("UPDATE").Scan(ctx); err != nil {
		return selected, err
	}
	current, err := FindCurrentIncidentCycle(ctx, tx, parent, false)
	if err != nil {
		return selected, err
	}
	if current.ID != selected.ID || selected.ResolvedAt != nil || msg.RedactedAt != nil {
		return selected, ErrIncidentConflict
	}
	err = tx.NewUpdate().Model(&selected).Set("resolved_at = ?", at).Set("resolution_source = 'admin'").Set("resolved_by = ?", adminID).Set("resolution_message_id = ?", msg.ID).WherePK().Returning("*").Scan(ctx)
	return selected, err
}

func findIncidentParent(ctx context.Context, db bun.IDB, target IncidentTarget, lock bool) (IncidentParent, error) {
	var id int64
	var q *bun.SelectQuery
	switch target.Kind {
	case "alert":
		q = db.NewSelect().TableExpr("alerts AS p")
	case "error":
		q = db.NewSelect().TableExpr("app_errors AS p")
	default:
		return IncidentParent{}, sql.ErrNoRows
	}
	q = q.Column("p.id").Where("p.public_id = ?", target.PublicID)
	if lock {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx, &id); err != nil {
		return IncidentParent{}, err
	}
	if target.Kind == "alert" {
		return IncidentParent{AlertID: id}, nil
	}
	return IncidentParent{AppErrorID: id}, nil
}

func nameIncidentCycles(ctx context.Context, db bun.IDB, rows []IncidentCycle) error {
	for i := range rows {
		row := &rows[i]
		if row.ResolvedBy != nil {
			var user User
			if err := db.NewSelect().Model(&user).Column("public_id", "name").Where("u.id = ?", *row.ResolvedBy).Scan(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			} else if err == nil {
				row.Resolver = &IncidentPerson{ID: user.PublicID, Name: user.Name}
			}
		}
		if row.ResolutionMessageID != nil {
			var id string
			if err := db.NewSelect().Model((*IncidentMessage)(nil)).Column("public_id").Where("im.id = ?", *row.ResolutionMessageID).Scan(ctx, &id); err != nil {
				return err
			}
			row.ResolutionMessage = &id
		}
	}
	return nil
}

// IndexIncidentCycles reads bounded cycle history, scoping its cursor to parent.
func (s *Store) IndexIncidentCycles(ctx context.Context, target IncidentTarget, before string, perPage int) (IncidentCyclePage, error) {
	parent, err := findIncidentParent(ctx, s.DB, target, false)
	if err != nil {
		return IncidentCyclePage{}, err
	}
	q := incidentCycles(s.DB, parent).OrderExpr("ic.number DESC").Limit(int64(perPage + 1))
	if before != "" {
		var cursor IncidentCycle
		if err := incidentCycles(s.DB, parent).Where("ic.public_id = ?", before).Scan(ctx, &cursor); errors.Is(err, sql.ErrNoRows) {
			return IncidentCyclePage{}, ErrNoCursor
		} else if err != nil {
			return IncidentCyclePage{}, err
		}
		q = q.Where("ic.number < ?", cursor.Number)
	}
	page := IncidentCyclePage{Data: []IncidentCycle{}}
	if err := q.Scan(ctx, &page.Data); err != nil {
		return page, err
	}
	if len(page.Data) > perPage {
		page.Data = page.Data[:perPage]
		id := page.Data[perPage-1].PublicID
		page.Next = &id
	}
	return page, nameIncidentCycles(ctx, s.DB, page.Data)
}

// FindIncidentThread returns current cycle and newest twenty cycle summaries.
func (s *Store) FindIncidentThread(ctx context.Context, target IncidentTarget) (IncidentThread, error) {
	page, err := s.IndexIncidentCycles(ctx, target, "", 20)
	if err != nil {
		return IncidentThread{}, err
	}
	if len(page.Data) == 0 {
		return IncidentThread{Cycles: page.Data}, nil
	}
	return IncidentThread{CurrentCycle: page.Data[0], Cycles: page.Data, NextCycle: page.Next}, nil
}
