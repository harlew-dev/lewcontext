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

var (
	// ErrNoEmailChange means no live change belongs to this account/session.
	ErrNoEmailChange = errors.New("no pending email change")
	// ErrEmailUnchanged refuses the account's own canonical address.
	ErrEmailUnchanged = errors.New("email is unchanged")
	// ErrEmailUnavailable deliberately does not distinguish taken and blocked.
	ErrEmailUnavailable = errors.New("email cannot be used")
	// ErrEmailChangeProofChanged asks for another submission after MFA changed
	// between middleware and the account lock, before checking a factor under
	// the wrong password/MFA guessing budget.
	ErrEmailChangeProofChanged = errors.New("email-change proof requirement changed")
)

type emailChange struct {
	bun.BaseModel `bun:"table:email_changes,alias:ec"`
	UserID        int64 `bun:",pk"`
	SessionHash   string
	PreviousEmail string
	Email         string
	EmailVersion  int64
	RequestID     string
	CodeHash      string `bun:",nullzero"`
	Attempts      int
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// PendingEmailChange contains only what the originating browser may see.
type PendingEmailChange struct {
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expiresAt"`
	State     string    `json:"state"`
	// RequestID lets the controller meter a snapshot before taking row locks.
	// Completion rechecks this correlation; it never reaches the browser.
	RequestID string `json:"-"`
}

func (p emailChange) pending() *PendingEmailChange {
	state := "sending"
	if p.CodeHash != "" {
		state = "code"
	}
	return &PendingEmailChange{Email: p.Email, ExpiresAt: p.ExpiresAt, State: state, RequestID: p.RequestID}
}

// EmailChangeProof checks the factor chosen by locked state. HTTP request and
// factor limits have already run; completion is reserved transactionally only
// after account/inbox proof and address availability.
type EmailChangeProof struct {
	PasswordConfirmation
	ExpectedMFA     bool
	CompletionLimit EmailChangeCompletionLimit
}

// EmailChangeResult distinguishes committed outcomes without rolling back an
// incorrect code's attempt or the end of an unavailable request.
type EmailChangeResult struct {
	User                            User
	Pending                         *PendingEmailChange
	Confirmed                       PasswordConfirmed
	ProofChecked                    bool
	OK, Ended, Waiting, Unavailable bool
	LimitedUntil                    *time.Time
}

// FindEmailChange returns nil when this session has no live pending change.
func (s *Store) FindEmailChange(ctx context.Context, userID int64, sessionHash string) (*PendingEmailChange, error) {
	var row emailChange
	err := s.DB.NewSelect().Model(&row).
		Where("ec.user_id = ? AND ec.session_hash = ? AND ec.expires_at > clock_timestamp()", userID, sessionHash).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.pending(), nil
}

// lockEmailChangeAccount revalidates middleware's session under the account
// lock. Logout/cap eviction may cascade into the pending row, so the session
// is locked before that row. No account lock is held across SMTP delivery.
func lockEmailChangeAccount(ctx context.Context, tx bun.Tx, userID int64, sessionHash string) (account, error) {
	var a account
	err := tx.NewSelect().Model(&a).ColumnExpr("u.*").
		ColumnExpr("(?) AS mfa_enabled_at", mfaEnabledAt(tx)).
		Where("u.id = ?", userID).For("NO KEY UPDATE OF u").Scan(ctx)
	if err != nil {
		return a, err
	}
	if err := checkNotSuspended(ctx, tx, userID); err != nil {
		return a, err
	}
	var session sessionRecord
	err = tx.NewSelect().Model(&session).
		Where("s.user_id = ? AND s.token_hash = ? AND s.expires_at > clock_timestamp() AND s.last_seen_at > clock_timestamp() - interval '30 minutes'", userID, sessionHash).
		For("UPDATE").Scan(ctx)
	return a, err
}

// CreateEmailChange requests a session-bound code, or completes directly when
// verification is disabled. A refused request leaves the previous one usable.
func (s *Store) CreateEmailChange(ctx context.Context, userID int64, sessionHash, email string, verify bool, proof EmailChangeProof) (EmailChangeResult, error) {
	var result EmailChangeResult
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		a, err := lockEmailChangeAccount(ctx, tx, userID, sessionHash)
		if err != nil {
			return err
		}
		if email == a.Email {
			return ErrEmailUnchanged
		}
		result.User = a.User
		if (a.MFAEnabledAt != nil) != proof.ExpectedMFA {
			return ErrEmailChangeProofChanged
		}
		// A savepoint rolls factor consumption back on direct-mode conflict,
		// while allowing the outer transaction to retain its notification event.
		err = tx.RunInTx(ctx, nil, func(ctx context.Context, change bun.Tx) error {
			used, err := useConfirmedFactor(ctx, change, userID, proof.Factor)
			result.Confirmed.MFA = !errors.Is(err, ErrMFANotEnabled)
			if !result.Confirmed.MFA {
				err = proof.Password(a.PasswordHash)
			}
			result.ProofChecked = true
			if err != nil {
				return err
			}
			if used != nil {
				result.Confirmed.FactorUsed = *used
			}
			request, err := s.recordID(ctx, change, events.EmailChangeRequested{User: a.PublicID, Email: email, VerificationRequired: verify}, EventScope{UserID: userID})
			if err != nil {
				return err
			}
			if !verify {
				if err := s.completeEmailChange(ctx, change, &result.User, email, false, proof.CompletionLimit); err != nil {
					return err
				}
				result.OK = true
				return nil
			}
			var created time.Time
			if err := change.NewSelect().ColumnExpr("clock_timestamp()").Scan(ctx, &created); err != nil {
				return err
			}
			row := emailChange{UserID: userID, SessionHash: sessionHash, PreviousEmail: a.Email, Email: email, EmailVersion: a.EmailVersion, RequestID: request, CreatedAt: created, ExpiresAt: created.Add(auth.EmailCodeLifetime)}
			_, err = change.NewInsert().Model(&row).On("CONFLICT (user_id) DO UPDATE").
				Set("session_hash = EXCLUDED.session_hash, previous_email = EXCLUDED.previous_email, email = EXCLUDED.email, email_version = EXCLUDED.email_version, request_id = EXCLUDED.request_id").
				Set("code_hash = NULL, attempts = 0, created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at").Exec(ctx)
			if err == nil {
				result.Pending = row.pending()
			}
			return err
		})
		if errors.Is(err, ErrEmailUnavailable) {
			result.Unavailable = true
			result.Confirmed.FactorUsed = FactorUsed{}
			return s.record(ctx, tx, events.EmailChangeRequested{User: a.PublicID, Email: email, VerificationRequired: false}, EventScope{UserID: userID})
		}
		var limited *emailChangeLimited
		if errors.As(err, &limited) {
			// Retry only the rolled-back reservation in the outer transaction.
			if err := reserveEmailChangeCompletion(ctx, tx, proof.CompletionLimit); !errors.As(err, &limited) {
				return err
			}
			result.LimitedUntil = &limited.ResetsAt
			result.Confirmed.FactorUsed = FactorUsed{}
			return s.recordEmailChangeLimit(ctx, tx, a.User, proof.CompletionLimit, limited)
		}
		return err
	})
	return result, err
}

// DeleteEmailChange cancels only this session's change, idempotently.
func (s *Store) DeleteEmailChange(ctx context.Context, userID int64, sessionHash string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		a, err := lockEmailChangeAccount(ctx, tx, userID, sessionHash)
		if err != nil {
			return err
		}
		res, err := tx.NewDelete().Model((*emailChange)(nil)).Where("user_id = ? AND session_hash = ?", userID, sessionHash).Exec(ctx)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		return s.record(ctx, tx, events.EmailChangeCancelled{User: a.PublicID}, EventScope{UserID: userID})
	})
}
