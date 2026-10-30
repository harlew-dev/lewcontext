package store

import (
	"context"
	"github.com/uptrace/bun"
)

// CreateDemoAccount inserts a user and their projects in one transaction.
func (s *Store) CreateDemoAccount(ctx context.Context, name, email, hash string, projects []Project) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		a := account{User: User{Name: name, Email: email}, PasswordHash: hash}
		if _, err := tx.NewInsert().Model(&a).Returning("id").Exec(ctx); err != nil {
			return err
		}
		for i := range projects {
			projects[i].UserID = a.ID
		}
		_, err := tx.NewInsert().Model(&projects).Exec(ctx)
		return err
	})
}
