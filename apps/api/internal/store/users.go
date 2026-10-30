package store

import (
	"context"
	"forma/api/internal/events"
	"github.com/uptrace/bun"
)

// CreateUser creates an account and records auth.registered.
func (s *Store) CreateUser(ctx context.Context, name, email, hash string) (User, error) {
	a := account{User: User{Name: name, Email: email}, PasswordHash: hash}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&a).Returning("*").Exec(ctx); err != nil {
			return err
		}
		e := events.Registered{User: a.PublicID, Name: a.Name, Email: a.Email}
		return s.record(ctx, tx, e, EventScope{ActorID: a.ID, UserID: a.ID})
	})
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

// ChangePassword replaces the user's password hash, deletes every session of
// theirs (sessionHash is the one making the change) and records
// auth.password_changed, in one transaction with the user row locked.
// replace receives the current hash and returns the new one; an error from it
// changes nothing.
func (s *Store) ChangePassword(ctx context.Context, u User, sessionHash string, replace func(current string) (string, error)) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var current string
		if err := tx.NewSelect().Model((*account)(nil)).Column("password_hash").Where("id = ?", u.ID).For("UPDATE").Scan(ctx, &current); err != nil {
			return err
		}
		hash, err := replace(current)
		if err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*account)(nil)).Set("password_hash = ?", hash).Where("id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
		others, err := deleteSessions(ctx, tx, "user_id = ? AND token_hash <> ?", u.ID, sessionHash)
		if err != nil {
			return err
		}
		if _, err := deleteSessions(ctx, tx, "token_hash = ?", sessionHash); err != nil {
			return err
		}
		return s.record(ctx, tx, events.PasswordChanged{User: u.PublicID, OtherSessionsRevoked: others}, EventScope{UserID: u.ID})
	})
}
