package store

import (
	"context"
	"github.com/uptrace/bun"
	"strings"
	"time"
)

// userEmailHistory exists solely to locate retained erasure references. It is
// neither a login alias nor a reservation of an address for its former owner.
type userEmailHistory struct {
	bun.BaseModel `bun:"table:user_email_history,alias:eh"`
	ID            int64 `bun:",pk,autoincrement"`
	UserID        int64
	Email         string
	ReleasedAt    time.Time
}

// cleanupEmailHistory bounds each sweep and checks every indexed address
// location, including pseudonyms. Retained references, not elapsed years,
// decide when a former raw address can be removed.
func (s *Store) cleanupEmailHistory(ctx context.Context, pseudonym func(string) string) error {
	var after int64
	for {
		var rows []userEmailHistory
		if err := s.DB.NewSelect().Model(&rows).Where("id > ?", after).Order("id").Limit(100).Scan(ctx); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			after = row.ID
			keep := false
			for _, field := range []string{"email", "previousEmail", "filters,email", "q"} {
				q := emailReferences(s.DB, field, row.Email, pseudonym).Where("e.created_at <= ?", row.ReleasedAt)
				found, err := q.Exists(ctx)
				if err != nil {
					return err
				}
				if found {
					keep = true
					break
				}
			}
			if !keep {
				if _, err := s.DB.NewDelete().Model((*userEmailHistory)(nil)).Where("id = ?", row.ID).Exec(ctx); err != nil {
					return err
				}
			}
		}
	}
}

// emailReferences uses fixed expressions matching the erasure indexes.
func emailReferences(db bun.IDB, field, email string, pseudonym func(string) string) *bun.SelectQuery {
	matches := bun.In([]string{strings.ToLower(email), pseudonym(email)})
	q := db.NewSelect().Model((*eventRecord)(nil))
	switch field {
	case "email":
		return q.Where(`e.payload \? 'email' AND lower(e.payload->>'email') IN (?)`, matches)
	case "previousEmail":
		return q.Where(`e.payload \? 'previousEmail' AND lower(e.payload->>'previousEmail') IN (?)`, matches)
	case "filters,email":
		return q.Where(`e.payload->'filters' \? 'email' AND lower(e.payload #>> '{filters,email}') IN (?)`, matches)
	default:
		return q.Where(`e.payload \? 'q' AND lower(e.payload->>'q') IN (?)`, matches)
	}
}
