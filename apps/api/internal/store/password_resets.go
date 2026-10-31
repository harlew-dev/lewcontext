package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// passwordReset is an account's live reset link: one per account, so a new
// request replaces the old link. Only the digest of the link's token is stored.
type passwordReset struct {
	bun.BaseModel `bun:"table:password_resets,alias:pr"`
	UserID        int64     `bun:",pk"`
	TokenHash     string    `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

// ErrResetInvalid means a reset link is unknown, expired, replaced by a newer
// one, or already used.
var ErrResetInvalid = errors.New("password reset link is invalid")

// IssuePasswordReset makes hash the live reset link of the account using
// email until expires, replacing any earlier link. It returns sql.ErrNoRows
// when no account uses the email.
func (s *Store) IssuePasswordReset(ctx context.Context, email, hash string, expires time.Time) (User, error) {
	var u User
	if err := s.DB.NewSelect().Model(&u).Where("email = ?", email).Scan(ctx); err != nil {
		return u, err
	}
	reset := passwordReset{UserID: u.ID, TokenHash: hash, ExpiresAt: expires}
	_, err := s.DB.NewInsert().Model(&reset).
		On("CONFLICT (user_id) DO UPDATE").
		Set("token_hash = EXCLUDED.token_hash").
		Set("created_at = EXCLUDED.created_at").
		Set("expires_at = EXCLUDED.expires_at").
		Exec(ctx)
	return u, err
}

// CheckPasswordReset reports whether hash is a live reset link.
func (s *Store) CheckPasswordReset(ctx context.Context, hash string) (bool, error) {
	return s.DB.NewSelect().Model((*passwordReset)(nil)).
		Where("token_hash = ? AND expires_at > now()", hash).
		Exists(ctx)
}

// ResetPassword gives the account whose live reset link is hash the password
// hash passwordHash, deletes the link and every session of the account's
// (admin sessions too), and
// records auth.password_reset.completed, in one transaction. It returns
// ErrResetInvalid unless hash is a live link.
func (s *Store) ResetPassword(ctx context.Context, hash, passwordHash string) (User, error) {
	var u User
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var userID int64
		err := tx.NewSelect().Model((*passwordReset)(nil)).Column("user_id").
			Where("token_hash = ? AND expires_at > now()", hash).
			Scan(ctx, &userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrResetInvalid
		}
		if err != nil {
			return err
		}
		// Lock the account, then the link, in the same order as
		// ChangePassword. The link is checked again under the lock: a second
		// request with the same token waits for the account, then finds the
		// link gone, so each link works once.
		if err := tx.NewSelect().Model(&u).Where("id = ?", userID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		err = tx.NewSelect().Model((*passwordReset)(nil)).Column("user_id").
			Where("user_id = ? AND token_hash = ? AND expires_at > now()", userID, hash).
			For("UPDATE").
			Scan(ctx, &userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrResetInvalid
		}
		if err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*account)(nil)).Set("password_hash = ?", passwordHash).Where("id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
		if err := deletePasswordReset(ctx, tx, u.ID); err != nil {
			return err
		}
		revoked, err := deleteSessions(ctx, tx, "user_id = ?", u.ID)
		if err != nil {
			return err
		}
		if _, err := deleteAdminSessions(ctx, tx, u.ID); err != nil {
			return err
		}
		e := events.PasswordResetCompleted{User: u.PublicID, OtherSessionsRevoked: revoked}
		return s.record(ctx, tx, e, EventScope{ActorID: u.ID, UserID: u.ID})
	})
	return u, err
}

// deletePasswordReset removes the account's reset link, if it has one.
func deletePasswordReset(ctx context.Context, db bun.IDB, userID int64) error {
	_, err := db.NewDelete().Model((*passwordReset)(nil)).Where("user_id = ?", userID).Exec(ctx)
	return err
}
