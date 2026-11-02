package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"time"
)

// UpdateEmailChange checks only the emailed code against the originating
// session's pending destination; it never accepts a candidate from the body.
func (s *Store) UpdateEmailChange(ctx context.Context, userID int64, sessionHash, requestID, digest string, completionLimit EmailChangeCompletionLimit) (EmailChangeResult, error) {
	var result EmailChangeResult
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		a, err := lockEmailChangeAccount(ctx, tx, userID, sessionHash)
		if err != nil {
			return err
		}
		var row emailChange
		err = tx.NewSelect().Model(&row).Where("user_id = ? AND session_hash = ? AND request_id = ? AND expires_at > clock_timestamp()", userID, sessionHash, requestID).For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoEmailChange
		}
		if err != nil {
			return err
		}
		if row.PreviousEmail != a.Email || row.EmailVersion != a.EmailVersion {
			return ErrNoEmailChange
		}
		if row.CodeHash == "" {
			result.Waiting = true
			return nil
		}
		ok := subtle.ConstantTimeCompare([]byte(row.CodeHash), []byte(digest)) == 1
		if err := s.record(ctx, tx, events.EmailChangeChecked{User: a.PublicID, OK: ok, Attempt: row.Attempts + 1}, EventScope{UserID: userID}); err != nil {
			return err
		}
		if !ok {
			result.Ended, err = countCodeAttempt(ctx, tx, (*emailChange)(nil), "user_id = ?", userID, row.Attempts)
			return err
		}
		result.User = a.User
		err = s.completeEmailChange(ctx, tx, &result.User, row.Email, true, completionLimit)
		if errors.Is(err, ErrEmailUnavailable) {
			result.Unavailable = true
			return deleteEmailChange(ctx, tx, userID)
		}
		var limited *emailChangeLimited
		if errors.As(err, &limited) {
			result.LimitedUntil = &limited.ResetsAt
			return s.recordEmailChangeLimit(ctx, tx, a.User, completionLimit, limited)
		}
		result.OK = err == nil
		return err
	})
	return result, err
}

// completeEmailChange is shared by code and verification-off completion.
// Caller holds account/session/pending locks. Checking availability never
// locks the destination's owner: crossed requests must not deadlock.
func (s *Store) completeEmailChange(ctx context.Context, tx bun.Tx, u *User, email string, verified bool, completionLimit EmailChangeCompletionLimit) error {
	if err := database.LockEmail(ctx, tx, email); err != nil {
		return err
	}
	taken, err := tx.NewSelect().Model((*User)(nil)).Where("email = ?", email).Exists(ctx)
	if err != nil {
		return err
	}
	blocked, err := emailBlocked(ctx, tx, email)
	if err != nil {
		return err
	}
	if taken || blocked {
		return ErrEmailUnavailable
	}
	if err := reserveEmailChangeCompletion(ctx, tx, completionLimit); err != nil {
		return err
	}
	var released time.Time
	if err := tx.NewSelect().ColumnExpr("clock_timestamp()").Scan(ctx, &released); err != nil {
		return err
	}
	previous := u.Email
	history := userEmailHistory{UserID: u.ID, Email: previous, ReleasedAt: released}
	if _, err := tx.NewInsert().Model(&history).Exec(ctx); err != nil {
		return err
	}
	u.Email, u.EmailVersion, u.EmailChangedAt = email, u.EmailVersion+1, &released
	u.EmailVerifiedAt = nil
	via := events.VerifiedByNone
	if verified {
		u.EmailVerifiedAt = &released
		via = events.VerifiedByCode
	}
	if _, err := tx.NewUpdate().Model(u).Column("email", "email_version", "email_changed_at", "email_verified_at").Where("id = ?", u.ID).Exec(ctx); err != nil {
		return err
	}
	if err := deleteEmailChange(ctx, tx, u.ID); err != nil {
		return err
	}
	for _, model := range []any{(*passwordReset)(nil), (*emailVerification)(nil), (*mfaChallenge)(nil)} {
		if _, err := tx.NewDelete().Model(model).Where("user_id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
	}
	if _, err := tx.NewDelete().Model((*mfaTOTP)(nil)).Where("user_id = ? AND confirmed_at IS NULL", u.ID).Exec(ctx); err != nil {
		return err
	}
	sessions, err := deleteSessions(ctx, tx, "user_id = ?", u.ID)
	if err != nil {
		return err
	}
	admins, err := deleteAdminSessions(ctx, tx, u.ID)
	if err != nil {
		return err
	}
	return s.record(ctx, tx, events.EmailChanged{User: u.PublicID, Email: email, PreviousEmail: previous, VerifiedBy: via, SessionsRevoked: sessions, AdminSessionsRevoked: admins}, EventScope{UserID: u.ID})
}

func deleteEmailChange(ctx context.Context, db bun.IDB, userID int64) error {
	_, err := db.NewDelete().Model((*emailChange)(nil)).Where("user_id = ?", userID).Exec(ctx)
	return err
}
