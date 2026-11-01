package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// emailVerification is a signed-in account's code for verifying the email
// it already has: one per account, so a new code replaces the old.
type emailVerification struct {
	bun.BaseModel `bun:"table:email_verifications,alias:ev"`
	UserID        int64     `bun:",pk"`
	CodeHash      string    `bun:",notnull"`
	Attempts      int       `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

var (
	// ErrAlreadyVerified means the account's email is already verified.
	ErrAlreadyVerified = errors.New("email already verified")
	// ErrNoEmailVerification means no live code exists for the account: none
	// was sent, it expired, or wrong codes used it up.
	ErrNoEmailVerification = errors.New("no email verification code")
)

// EmailVerificationResult is a code checked against an account's. OK: the
// email is verified, and User the account. Ended: the fifth wrong code
// deleted it, so a new one is needed.
type EmailVerificationResult struct {
	OK, Ended bool
	User      User
}

// IssueEmailVerification makes codeHash the code of the unverified account
// with publicID until expires, replacing any earlier one, and returns the
// account. ErrAlreadyVerified for a verified (or deleted) account.
func (s *Store) IssueEmailVerification(ctx context.Context, publicID, codeHash string, expires time.Time) (User, error) {
	var u User
	err := s.DB.NewSelect().Model(&u).Where("public_id = ? AND email_verified_at IS NULL", publicID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrAlreadyVerified
	}
	if err != nil {
		return u, err
	}
	row := emailVerification{UserID: u.ID, CodeHash: codeHash, ExpiresAt: expires}
	_, err = s.DB.NewInsert().Model(&row).
		On("CONFLICT (user_id) DO UPDATE").
		Set("code_hash = EXCLUDED.code_hash").
		Set("attempts = 0").
		Set("created_at = EXCLUDED.created_at").
		Set("expires_at = EXCLUDED.expires_at").
		Exec(ctx)
	return u, err
}

// VerifyEmail checks digest (the HMAC of the code typed) against the
// account's live code, with the account's row locked. The right one marks the
// email verified, deletes the code and records auth.email_verified; a wrong
// one counts a try, and the fifth deletes it (Ended). ErrAlreadyVerified or
// ErrNoEmailVerification when there's nothing to check.
func (s *Store) VerifyEmail(ctx context.Context, userID int64, digest string) (EmailVerificationResult, error) {
	var result EmailVerificationResult
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u := &result.User
		err := tx.NewSelect().Model(u).ColumnExpr(userColumns).
			ColumnExpr("(?) AS mfa_enabled_at", mfaEnabledAt(tx)).
			Where("u.id = ?", userID).For("UPDATE OF u").Scan(ctx)
		if err != nil {
			return err
		}
		if u.EmailVerifiedAt != nil {
			return ErrAlreadyVerified
		}
		var row emailVerification
		err = tx.NewSelect().Model(&row).Where("user_id = ? AND expires_at > now()", userID).For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoEmailVerification
		}
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(digest), []byte(row.CodeHash)) != 1 {
			result.Ended, err = countCodeAttempt(ctx, tx, (*emailVerification)(nil), "user_id = ?", userID, row.Attempts)
			return err
		}
		result.OK = true
		return markEmailVerified(ctx, s, tx, u, events.VerifiedByCode)
	})
	return result, err
}

// markEmailVerified sets u's email_verified_at, deletes any code it was
// waiting on and records auth.email_verified. The caller holds u's row lock
// and knows it isn't verified yet.
func markEmailVerified(ctx context.Context, s *Store, tx bun.Tx, u *User, via string) error {
	verified := time.Now()
	if _, err := tx.NewUpdate().Model((*User)(nil)).Set("email_verified_at = ?", verified).Where("id = ?", u.ID).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.NewDelete().Model((*emailVerification)(nil)).Where("user_id = ?", u.ID).Exec(ctx); err != nil {
		return err
	}
	u.EmailVerifiedAt = &verified
	return s.record(ctx, tx, events.EmailVerified{User: u.PublicID, Via: via}, EventScope{UserID: u.ID})
}
