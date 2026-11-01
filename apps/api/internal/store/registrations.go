package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// pendingRegistration is a sign-up waiting for the code sent to its email
// (EMAIL_VERIFICATION=on): no account exists until the code is right. It
// belongs to the browser that started it (TokenHash, its cookie's digest),
// so one email can have several. RequestID is the evt_ ID of its latest
// auth.registration.requested, by which the listener finds it. CodeHash is
// the HMAC of its code, empty until the listener runs; for an email that
// already has an account, the HMAC of a code nobody is sent.
type pendingRegistration struct {
	bun.BaseModel `bun:"table:pending_registrations,alias:preg"`
	ID            int64  `bun:",pk,autoincrement"`
	Email         string `bun:",notnull"`
	Name          string `bun:",notnull"`
	PasswordHash  string `bun:",notnull"`
	TokenHash     string `bun:",notnull"`
	RequestID     string `bun:",notnull"`
	CodeHash      string `bun:",nullzero"`
	Attempts      int    `bun:",notnull"`
	// The policy versions agreed to (POLICIES=on), or nil.
	TermsOfServiceVersion *time.Time
	PrivacyPolicyVersion  *time.Time
	CreatedAt             time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt             time.Time `bun:",notnull"`
}

// ErrNoRegistration means a sign-up is unknown, expired, used up by wrong
// codes, or already completed.
var ErrNoRegistration = errors.New("no pending registration")

// NewRegistration is a sign-up as typed, its password already hashed, the
// digest of the token in its browser's cookie, and the policy versions it
// agreed to (POLICIES=on), or none.
type NewRegistration struct {
	Email, Name, PasswordHash, TokenHash string
	Policies                             PolicyVersions
}

// PendingRegistration is what the sign-up's own browser may see of it.
type PendingRegistration struct {
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// RegistrationResult is a code checked against a sign-up. OK: the code was
// right, and User the new account, unless Exists (an account took the email
// since the code was sent) or Blocked (the address was blocked since; its
// sign-ups are deleted). Ended: the fifth wrong code deleted the sign-up.
type RegistrationResult struct {
	OK, Ended, Exists, Blocked bool
	User                       User
}

// CreatePendingRegistration saves a sign-up waiting for its code, for
// auth.EmailCodeLifetime, and records auth.registration.requested, whose
// listener sends the code. It never reads users, so it does the same work
// whether or not an account uses the email.
func (s *Store) CreatePendingRegistration(ctx context.Context, in NewRegistration) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		request, err := s.recordID(ctx, tx, events.RegistrationRequested{Email: in.Email}, EventScope{})
		if err != nil {
			return err
		}
		row := pendingRegistration{
			Email: in.Email, Name: in.Name, PasswordHash: in.PasswordHash,
			TokenHash: in.TokenHash, RequestID: request,
			ExpiresAt: time.Now().Add(auth.EmailCodeLifetime),
		}
		row.TermsOfServiceVersion, row.PrivacyPolicyVersion = in.Policies.dates()
		_, err = tx.NewInsert().Model(&row).Exec(ctx)
		return err
	})
}

// FindPendingRegistration is the live sign-up of the browser whose cookie
// has the digest tokenHash, or ErrNoRegistration.
func (s *Store) FindPendingRegistration(ctx context.Context, tokenHash string) (PendingRegistration, error) {
	var p PendingRegistration
	err := s.DB.NewSelect().Model((*pendingRegistration)(nil)).Column("email", "expires_at").
		Where("token_hash = ? AND expires_at > now()", tokenHash).
		Scan(ctx, &p.Email, &p.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNoRegistration
	}
	return p, err
}

// ResendPendingRegistration asks for a new code for the browser's live
// sign-up: a new auth.registration.requested, a new lifetime and no tries
// used. The old code stops working at once, since the row forgets it, and
// a late delivery of the old request finds nothing (its request ID is
// gone). ErrNoRegistration without a live sign-up.
func (s *Store) ResendPendingRegistration(ctx context.Context, tokenHash string) (PendingRegistration, error) {
	var p PendingRegistration
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row pendingRegistration
		err := tx.NewSelect().Model(&row).Where("token_hash = ? AND expires_at > now()", tokenHash).For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoRegistration
		}
		if err != nil {
			return err
		}
		request, err := s.recordID(ctx, tx, events.RegistrationRequested{Email: row.Email}, EventScope{})
		if err != nil {
			return err
		}
		p = PendingRegistration{Email: row.Email, ExpiresAt: time.Now().Add(auth.EmailCodeLifetime)}
		_, err = tx.NewUpdate().Model((*pendingRegistration)(nil)).
			Set("request_id = ?", request).
			Set("code_hash = NULL").
			Set("attempts = 0").
			Set("expires_at = ?", p.ExpiresAt).
			Where("id = ?", row.ID).
			Exec(ctx)
		return err
	})
	return p, err
}

// IssueRegistrationCode stores codeHash as the code of the live sign-up
// that request (an auth.registration.requested's evt_ ID) made, and returns
// its email and, when an account already uses the email, that account. It
// is the only place a sign-up's email is looked up, after the request has
// been answered. ErrNoRegistration when the sign-up has expired, finished,
// or asked for a newer code since.
func (s *Store) IssueRegistrationCode(ctx context.Context, request, codeHash string) (email string, existing *User, err error) {
	res, err := s.DB.NewUpdate().Model((*pendingRegistration)(nil)).
		Set("code_hash = ?", codeHash).
		Where("request_id = ? AND expires_at > now()", request).
		Returning("email").
		Exec(ctx, &email)
	if err != nil {
		return "", nil, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return "", nil, errors.Join(ErrNoRegistration, err)
	}
	var u User
	err = s.DB.NewSelect().Model(&u).Where("email = ?", email).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return email, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return email, &u, nil
}

// CompleteRegistration checks digest (the HMAC of the code typed) against
// the browser's live sign-up, recording auth.registration.checked. Every
// sign-up has a code hash, a real one or one nobody was sent, so a wrong
// code costs the same whether or not the email has an account. A wrong one
// counts a try, and the fifth deletes the sign-up (Ended). The right one
// creates the account, verified, deletes every sign-up for the email and
// records auth.registered; if an account took the email meanwhile, it
// creates nothing (Exists), nor for an address blocked since its code was
// sent (Blocked), checked under the email's lock as BlockEmail checks for an
// account. ErrNoRegistration without a live sign-up.
func (s *Store) CompleteRegistration(ctx context.Context, tokenHash, digest string) (RegistrationResult, error) {
	var result RegistrationResult
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row pendingRegistration
		err := tx.NewSelect().Model(&row).Where("token_hash = ? AND expires_at > now()", tokenHash).For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoRegistration
		}
		if err != nil {
			return err
		}
		result.OK = subtle.ConstantTimeCompare([]byte(digest), []byte(row.CodeHash)) == 1
		checked := events.RegistrationChecked{Email: row.Email, OK: result.OK, Attempt: row.Attempts + 1}
		if err := s.record(ctx, tx, checked, EventScope{}); err != nil {
			return err
		}
		if !result.OK {
			result.Ended, err = countCodeAttempt(ctx, tx, (*pendingRegistration)(nil), "id = ?", row.ID, row.Attempts)
			return err
		}
		if _, err := tx.NewDelete().Model((*pendingRegistration)(nil)).Where("email = ?", row.Email).Exec(ctx); err != nil {
			return err
		}
		err = refuseBlockedEmail(ctx, tx, row.Email)
		if errors.Is(err, ErrEmailBlocked) {
			result.Blocked = true
			return nil
		}
		if err != nil {
			return err
		}
		verified := time.Now()
		a := account{User: User{Name: row.Name, Email: row.Email, EmailVerifiedAt: &verified}, PasswordHash: row.PasswordHash}
		// Agreed when the sign-up was submitted, not when the code came.
		policies := row.policies()
		a.agreeTo(policies, row.CreatedAt)
		res, err := tx.NewInsert().Model(&a).On("CONFLICT (email) DO NOTHING").Returning("*").Exec(ctx)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			result.Exists = true
			return err
		}
		result.User = a.User
		e := events.Registered{User: a.PublicID, Name: a.Name, Email: a.Email, VerifiedBy: events.VerifiedByCode, Policies: policies.event()}
		return s.record(ctx, tx, e, EventScope{ActorID: a.ID, UserID: a.ID})
	})
	return result, err
}

// countCodeAttempt counts a wrong code against the row model matching where,
// which has used attempts so far, or deletes it on the last allowed try:
// ended is true when it did.
func countCodeAttempt(ctx context.Context, tx bun.Tx, model any, where string, id any, attempts int) (ended bool, err error) {
	if attempts+1 < auth.EmailCodeMaxAttempts {
		_, err := tx.NewUpdate().Model(model).Set("attempts = attempts + 1").Where(where, id).Exec(ctx)
		return false, err
	}
	_, err = tx.NewDelete().Model(model).Where(where, id).Exec(ctx)
	return true, err
}
