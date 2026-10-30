package store

import (
	"context"
	"github.com/uptrace/bun"
)

func (s *Store) CreateUser(ctx context.Context, name, email, hash string) (User, error) {
	a := account{User: User{Name: name, Email: email}, PasswordHash: hash}
	_, err := s.DB.NewInsert().Model(&a).Returning("*").Exec(ctx)
	return a.User, err
}

// FindUser returns sql.ErrNoRows when no account uses the email.
func (s *Store) FindUser(ctx context.Context, email string) (User, string, error) {
	var a account
	err := s.DB.NewSelect().Model(&a).Where("email = ?", email).Scan(ctx)
	return a.User, a.PasswordHash, err
}

// PasswordHash returns the user's stored password hash.
func (s *Store) PasswordHash(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := s.DB.NewSelect().Model((*account)(nil)).Column("password_hash").Where("id = ?", userID).Scan(ctx, &hash)
	return hash, err
}

// ChangePassword replaces the user's password hash and deletes every session
// of theirs, in one transaction with the user row locked. replace receives the
// current hash and returns the new one; an error from it changes nothing.
func (s *Store) ChangePassword(ctx context.Context, userID int64, replace func(current string) (string, error)) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var current string
		if err := tx.NewSelect().Model((*account)(nil)).Column("password_hash").Where("id = ?", userID).For("UPDATE").Scan(ctx, &current); err != nil {
			return err
		}
		hash, err := replace(current)
		if err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*account)(nil)).Set("password_hash = ?", hash).Where("id = ?", userID).Exec(ctx); err != nil {
			return err
		}
		_, err = tx.NewDelete().Model((*sessionRecord)(nil)).Where("user_id = ?", userID).Exec(ctx)
		return err
	})
}
