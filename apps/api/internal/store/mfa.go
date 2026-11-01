package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// Two-factor authentication: one authenticator (TOTP) per user, its recovery
// codes, and the challenges of sign-ins waiting for a code. Every method that
// checks a code locks the user's mfa_totp row first (after the user row, when
// it takes that), then any challenge, so concurrent requests take turns and a
// code or recovery code can't be used twice.

var (
	// ErrMFAEnabled refuses setting up MFA again while it's on.
	ErrMFAEnabled = errors.New("two-factor authentication is already on")
	// ErrMFANotEnabled refuses turning off, or renewing the codes of, MFA
	// that's off.
	ErrMFANotEnabled = errors.New("two-factor authentication is off")
	// ErrMFANotPending refuses confirming a setup that wasn't started, or
	// started too long ago.
	ErrMFANotPending = errors.New("no two-factor setup is pending")
	// ErrWrongCode is a second factor that didn't match.
	ErrWrongCode = errors.New("wrong code")
	// ErrNoChallenge is a sign-in challenge that's missing, expired, used up
	// or for another purpose.
	ErrNoChallenge = errors.New("no live sign-in challenge")
)

// MFASetupLifetime is how long a started setup can be confirmed.
const MFASetupLifetime = 15 * time.Minute

// MaxChallengeAttempts is how many wrong codes end a sign-in challenge: the
// password has to be entered again.
const MaxChallengeAttempts = 5

// ChallengeLogin is an app sign-in's challenge purpose; the admin door's are
// in admins.go.
const ChallengeLogin = "login"

type mfaTOTP struct {
	bun.BaseModel `bun:"table:mfa_totp,alias:t"`
	UserID        int64 `bun:",pk"`
	Secret        []byte
	LastUsedStep  int64
	ConfirmedAt   *time.Time
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}
type recoveryCode struct {
	bun.BaseModel `bun:"table:mfa_recovery_codes,alias:rc"`
	ID            int64 `bun:",pk,autoincrement"`
	UserID        int64
	CodeHash      string
	UsedAt        *time.Time
}
type mfaChallenge struct {
	bun.BaseModel `bun:"table:mfa_challenges,alias:c"`
	TokenHash     string `bun:",pk"`
	UserID        int64
	Purpose       string
	Attempts      int
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time
}

// SecondFactor is what someone gave as their second factor: an
// authenticator code, which TOTP checks against the user's sealed secret
// (returning the step it matched), or a recovery code's digest.
type SecondFactor struct {
	TOTP           func(sealed []byte, lastStep int64) (step int64, ok bool)
	RecoveryDigest string
}

// Method names the factor for auth.mfa.checked.
func (f SecondFactor) Method() string {
	if f.TOTP == nil {
		return events.MFAMethodRecoveryCode
	}
	return events.MFAMethodTOTP
}

// FactorUsed is how a check went: RecoveryCodesLeft is set when a recovery
// code was used.
type FactorUsed struct {
	RecoveryCodesLeft *int
}

// MFAChallengeResult is a sign-in challenge's answer: OK with the user, or
// a wrong code, Ended when that was its last try.
type MFAChallengeResult struct {
	User  User
	OK    bool
	Ended bool
	FactorUsed
}

// mfaEnabledAt is the subquery giving users.mfa_enabled_at: when the user's
// authenticator was confirmed, or null.
func mfaEnabledAt(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().Model((*mfaTOTP)(nil)).Column("t.confirmed_at").Where("t.user_id = u.id")
}

// SetUpMFA stores a new sealed secret, waiting for its first code, replacing
// any setup already pending. ErrMFAEnabled while MFA is on.
func (s *Store) SetUpMFA(ctx context.Context, userID int64, sealed []byte) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		current, err := lockTOTP(ctx, tx, userID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && current.ConfirmedAt != nil {
			return ErrMFAEnabled
		}
		row := mfaTOTP{UserID: userID, Secret: sealed}
		_, err = tx.NewInsert().Model(&row).
			On("CONFLICT (user_id) DO UPDATE").
			Set("secret = EXCLUDED.secret, last_used_step = 0, created_at = now()").
			Exec(ctx)
		return err
	})
}

// EnableMFA confirms the pending setup with its first authenticator code,
// replaces the recovery codes with codeDigests, signs out every other session
// of the user's (sessionHash is this one), closes an admin's setup window at
// the door and records auth.mfa.enabled.
// ErrMFANotPending without a setup from the last 15 minutes, ErrMFAEnabled
// when it's already on, ErrWrongCode when the code doesn't match (nothing
// changes then).
func (s *Store) EnableMFA(ctx context.Context, u User, sessionHash string, f SecondFactor, codeDigests []string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockUser(ctx, tx, u.ID); err != nil {
			return err
		}
		row, err := lockTOTP(ctx, tx, u.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMFANotPending
		}
		if err != nil {
			return err
		}
		if row.ConfirmedAt != nil {
			return ErrMFAEnabled
		}
		if time.Since(row.CreatedAt) > MFASetupLifetime {
			return ErrMFANotPending
		}
		if f.TOTP == nil {
			return ErrWrongCode
		}
		step, ok := f.TOTP(row.Secret, row.LastUsedStep)
		if !ok {
			return ErrWrongCode
		}
		_, err = tx.NewUpdate().Model((*mfaTOTP)(nil)).
			Set("confirmed_at = now(), last_used_step = ?", step).
			Where("user_id = ?", u.ID).
			Exec(ctx)
		if err != nil {
			return err
		}
		if err := replaceRecoveryCodes(ctx, tx, u.ID, codeDigests); err != nil {
			return err
		}
		others, err := deleteSessions(ctx, tx, "user_id = ? AND token_hash <> ?", u.ID, sessionHash)
		if err != nil {
			return err
		}
		// An admin's setup window at the door closes once MFA is on.
		if _, err := tx.NewUpdate().Model((*adminRecord)(nil)).Set("mfa_setup_until = NULL").Where("user_id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, events.MFAEnabled{User: u.PublicID, OtherSessionsRevoked: others}, EventScope{UserID: u.ID})
	})
}

// DisableMFA turns MFA off after checking a second factor: the secret,
// recovery codes and sign-in challenges go, and auth.mfa.disabled is
// recorded. ErrMFANotEnabled when it's off, ErrAdminNeedsMFA for an admin,
// ErrWrongCode when the factor doesn't match (nothing changes then).
func (s *Store) DisableMFA(ctx context.Context, u User, f SecondFactor) (FactorUsed, error) {
	var used FactorUsed
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockUser(ctx, tx, u.ID); err != nil {
			return err
		}
		if admin, err := isAdmin(ctx, tx, u.ID); err != nil || admin {
			if err == nil {
				err = ErrAdminNeedsMFA
			}
			return err
		}
		factor, err := useConfirmedFactor(ctx, tx, u.ID, f)
		if err != nil {
			return err
		}
		used = *factor
		if err := deleteMFA(ctx, tx, u.ID); err != nil {
			return err
		}
		return s.record(ctx, tx, events.MFADisabled{User: u.PublicID, Reason: events.MFADisabledByUser}, EventScope{UserID: u.ID})
	})
	return used, err
}

// CreateRecoveryCodes replaces every recovery code with codeDigests after
// checking a second factor (an old recovery code works for it), and records
// auth.recovery_codes.created. Errors as DisableMFA's.
func (s *Store) CreateRecoveryCodes(ctx context.Context, u User, f SecondFactor, codeDigests []string) (FactorUsed, error) {
	var used FactorUsed
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockUser(ctx, tx, u.ID); err != nil {
			return err
		}
		factor, err := useConfirmedFactor(ctx, tx, u.ID, f)
		if err != nil {
			return err
		}
		used = *factor
		if err := replaceRecoveryCodes(ctx, tx, u.ID, codeDigests); err != nil {
			return err
		}
		e := events.RecoveryCodesCreated{User: u.PublicID, Count: len(codeDigests)}
		return s.record(ctx, tx, e, EventScope{UserID: u.ID})
	})
	return used, err
}

// ResetMFA is the operator's way back in for someone who lost their
// authenticator and recovery codes (admin reset-mfa): it turns MFA off,
// signs out every session (admin sessions too), reopens an admin's 24-hour
// setup window at the door, and records auth.mfa.disabled with reason reset
// and no actor. sql.ErrNoRows for an unknown email, ErrMFANotEnabled when
// the account has no authenticator, set up or pending.
func (s *Store) ResetMFA(ctx context.Context, email string) (User, error) {
	var u User
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := tx.NewSelect().Model(&u).Where("email = ?", email).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		if _, err := lockTOTP(ctx, tx, u.ID); errors.Is(err, sql.ErrNoRows) {
			return ErrMFANotEnabled
		} else if err != nil {
			return err
		}
		if err := deleteMFA(ctx, tx, u.ID); err != nil {
			return err
		}
		if _, err := deleteSessions(ctx, tx, "user_id = ?", u.ID); err != nil {
			return err
		}
		if _, err := deleteAdminSessions(ctx, tx, u.ID); err != nil {
			return err
		}
		// An admin sets up a new authenticator at their next admin sign-in.
		_, err := tx.NewUpdate().Model((*adminRecord)(nil)).
			Set("mfa_setup_until = ?", time.Now().Add(auth.AdminSetupWindow)).
			Where("user_id = ?", u.ID).
			Exec(ctx)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, events.MFADisabled{User: u.PublicID, Reason: events.MFADisabledReset}, EventScope{UserID: u.ID})
	})
	return u, err
}

// CreateMFAChallenge starts a sign-in's wait for its second factor.
func (s *Store) CreateMFAChallenge(ctx context.Context, userID int64, hash, purpose string, expires time.Time) error {
	c := mfaChallenge{TokenHash: hash, UserID: userID, Purpose: purpose, ExpiresAt: expires}
	_, err := s.DB.NewInsert().Model(&c).Exec(ctx)
	return err
}

// FindMFAChallenge returns the user (ID and public ID) of the live challenge
// hash with purpose, or ErrNoChallenge.
func (s *Store) FindMFAChallenge(ctx context.Context, hash, purpose string) (User, error) {
	var u User
	err := s.DB.NewSelect().Model(&u).ColumnExpr("u.id, u.public_id").
		Join("JOIN mfa_challenges AS c ON c.user_id = u.id").
		Where("c.token_hash = ? AND c.purpose = ? AND c.expires_at > now()", hash, purpose).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNoChallenge
	}
	return u, err
}

// AnswerMFAChallenge checks the second factor of userID's live challenge
// hash with purpose (FindMFAChallenge). The right one deletes the challenge
// and returns OK with the whole user. A wrong one counts a try, and the
// fifth deletes the challenge (Ended): the password is needed again.
// ErrNoChallenge when it's gone, expired, or its user has turned MFA off.
func (s *Store) AnswerMFAChallenge(ctx context.Context, hash, purpose string, userID int64, f SecondFactor) (MFAChallengeResult, error) {
	var result MFAChallengeResult
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		used, err := useConfirmedFactor(ctx, tx, userID, f)
		if errors.Is(err, ErrMFANotEnabled) {
			_, err := tx.NewDelete().Model((*mfaChallenge)(nil)).Where("token_hash = ?", hash).Exec(ctx)
			if err == nil {
				err = ErrNoChallenge
			}
			return err
		}
		if err != nil && !errors.Is(err, ErrWrongCode) {
			return err
		}
		var challenge mfaChallenge
		err = tx.NewSelect().Model(&challenge).
			Where("token_hash = ? AND purpose = ? AND user_id = ? AND expires_at > now()", hash, purpose, userID).
			For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoChallenge
		}
		if err != nil {
			return err
		}
		if used == nil && challenge.Attempts+1 < MaxChallengeAttempts {
			_, err := tx.NewUpdate().Model((*mfaChallenge)(nil)).Set("attempts = attempts + 1").Where("token_hash = ?", hash).Exec(ctx)
			return err
		}
		if _, err := tx.NewDelete().Model((*mfaChallenge)(nil)).Where("token_hash = ?", hash).Exec(ctx); err != nil {
			return err
		}
		if used == nil {
			result.Ended = true
			return nil
		}
		result.OK, result.FactorUsed = true, *used
		return tx.NewSelect().Model(&result.User).
			ColumnExpr(userColumns).
			ColumnExpr("(?) AS mfa_enabled_at", mfaEnabledAt(tx)).
			Where("u.id = ?", userID).Scan(ctx)
	})
	return result, err
}

// lockUser takes the user's row lock, serialising MFA changes per account.
func lockUser(ctx context.Context, tx bun.Tx, userID int64) error {
	_, err := tx.NewSelect().Model((*User)(nil)).Column("id").Where("id = ?", userID).For("UPDATE").Exec(ctx)
	return err
}

// lockTOTP reads and locks the user's authenticator; sql.ErrNoRows without one.
func lockTOTP(ctx context.Context, tx bun.Tx, userID int64) (mfaTOTP, error) {
	var row mfaTOTP
	err := tx.NewSelect().Model(&row).Where("user_id = ?", userID).For("UPDATE").Scan(ctx)
	return row, err
}

// useConfirmedFactor locks the user's confirmed authenticator and uses up
// the second factor f: an authenticator code's step, or a recovery code. A
// nil FactorUsed with ErrWrongCode when it doesn't match. ErrMFANotEnabled
// when MFA is off.
func useConfirmedFactor(ctx context.Context, tx bun.Tx, userID int64, f SecondFactor) (*FactorUsed, error) {
	row, err := lockTOTP(ctx, tx, userID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && row.ConfirmedAt == nil) {
		return nil, ErrMFANotEnabled
	}
	if err != nil {
		return nil, err
	}
	if f.TOTP != nil {
		step, ok := f.TOTP(row.Secret, row.LastUsedStep)
		if !ok {
			return nil, ErrWrongCode
		}
		_, err := tx.NewUpdate().Model((*mfaTOTP)(nil)).Set("last_used_step = ?", step).Where("user_id = ?", userID).Exec(ctx)
		return &FactorUsed{}, err
	}
	result, err := tx.NewUpdate().Model((*recoveryCode)(nil)).Set("used_at = now()").
		Where("user_id = ? AND code_hash = ? AND used_at IS NULL", userID, f.RecoveryDigest).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		if err == nil {
			err = ErrWrongCode
		}
		return nil, err
	}
	unused, err := tx.NewSelect().Model((*recoveryCode)(nil)).Where("user_id = ? AND used_at IS NULL", userID).Count(ctx)
	left := int(unused)
	return &FactorUsed{RecoveryCodesLeft: &left}, err
}

// replaceRecoveryCodes deletes the user's recovery codes and stores digests.
func replaceRecoveryCodes(ctx context.Context, tx bun.Tx, userID int64, digests []string) error {
	if _, err := tx.NewDelete().Model((*recoveryCode)(nil)).Where("user_id = ?", userID).Exec(ctx); err != nil {
		return err
	}
	codes := make([]recoveryCode, len(digests))
	for i, digest := range digests {
		codes[i] = recoveryCode{UserID: userID, CodeHash: digest}
	}
	if len(codes) == 0 {
		return nil
	}
	_, err := tx.NewInsert().Model(&codes).Exec(ctx)
	return err
}

// deleteMFA deletes the user's authenticator, recovery codes and challenges.
func deleteMFA(ctx context.Context, tx bun.Tx, userID int64) error {
	for _, model := range []any{(*mfaTOTP)(nil), (*recoveryCode)(nil), (*mfaChallenge)(nil)} {
		if _, err := tx.NewDelete().Model(model).Where("user_id = ?", userID).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
