package store

import (
	"context"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// maxLastOwnerWorkspaces bounds the workspaces a LastOwnerError lists.
const maxLastOwnerWorkspaces = 100

// WorkspaceSummary names a workspace, for messages that list workspaces.
type WorkspaceSummary struct {
	PublicID string `bun:"public_id" json:"id"`
	Name     string `json:"name"`
}

// LastOwnerError refuses deleting an account that is the only owner of
// Workspaces (at most 100). It matches ErrLastOwner with errors.Is.
type LastOwnerError struct {
	Workspaces []WorkspaceSummary
}

func (e LastOwnerError) Error() string        { return ErrLastOwner.Error() }
func (e LastOwnerError) Is(target error) bool { return target == ErrLastOwner }

// CreateUser creates an account with its email unverified, for sign-up with
// EMAIL_VERIFICATION off, and records auth.registered. policies are the
// versions agreed to (POLICIES=on), recorded as accepted now, or none.
// ErrEmailBlocked for an address on the block list, checked under the
// email's lock.
func (s *Store) CreateUser(ctx context.Context, name, email, hash string, policies PolicyVersions) (User, error) {
	a := account{User: User{Name: name, Email: email}, PasswordHash: hash}
	if err := a.agreeTo(policies, time.Now()); err != nil {
		return User{}, err
	}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := refuseBlockedEmail(ctx, tx, email); err != nil {
			return err
		}
		if _, err := tx.NewInsert().Model(&a).Returning("*").Exec(ctx); err != nil {
			return err
		}
		e := events.Registered{User: a.PublicID, Name: a.Name, Email: a.Email, VerifiedBy: events.VerifiedByNone, Policies: policies.event()}
		return s.record(ctx, tx, e, EventScope{ActorID: a.ID, UserID: a.ID})
	})
	return a.User, err
}

// FindUser returns sql.ErrNoRows when no account uses the email. The user's
// MFAEnabledAt says whether signing in needs a second factor, and
// Suspended whether it may sign in at all.
func (s *Store) FindUser(ctx context.Context, email string) (User, string, error) {
	var a account
	err := s.DB.NewSelect().Model(&a).ColumnExpr("u.*").ColumnExpr("(?) AS mfa_enabled_at", mfaEnabledAt(s.DB)).
		ColumnExpr("EXISTS (?) AS suspended", suspendedUser(s.DB, "u.id")).
		Where("email = ?", email).Scan(ctx)
	return a.User, a.PasswordHash, err
}

// UpdateProfile saves the user's name and records auth.profile_updated when
// it changed.
func (s *Store) UpdateProfile(ctx context.Context, u User, name string) (User, error) {
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var before string
		if err := tx.NewSelect().Model((*User)(nil)).Column("name").Where("id = ?", u.ID).For("UPDATE").Scan(ctx, &before); err != nil {
			return err
		}
		u.Name = name
		if _, err := tx.NewUpdate().Model(&u).Column("name").Where("id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
		changes := events.Changes{}
		changes.Add("name", before, u.Name)
		if len(changes) == 0 {
			return nil
		}
		return s.record(ctx, tx, events.ProfileUpdated{User: u.PublicID, Changes: changes}, EventScope{UserID: u.ID})
	})
	return u, err
}

// FindPasswordHash returns the user's stored password hash.
func (s *Store) FindPasswordHash(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := s.DB.NewSelect().Model((*account)(nil)).Column("password_hash").Where("id = ?", userID).Scan(ctx, &hash)
	return hash, err
}

// PasswordConfirmation is what confirms a password change: with MFA on the
// second factor Factor, and with it off Password, which checks the current
// hash.
type PasswordConfirmation struct {
	Factor   SecondFactor
	Password func(current string) error
}

// PasswordConfirmed says which check ChangePassword ran: the second factor
// (MFA, with how it was used once the change committed) or the password.
type PasswordConfirmed struct {
	MFA bool
	FactorUsed
}

// ChangePassword replaces the user's password hash, deletes their reset link
// and every session of theirs (sessionHash is the one making the change, and
// every admin session) and records
// auth.password_changed, in one transaction with the user row locked.
// confirm is checked first, against the locked rows: ErrWrongCode for a
// wrong second factor, or the password check's error. replace then receives
// the current hash and returns the new one. An error from either changes
// nothing, so a code isn't used up.
func (s *Store) ChangePassword(ctx context.Context, u User, sessionHash string, confirm PasswordConfirmation, replace func(current string) (string, error)) (PasswordConfirmed, error) {
	var confirmed PasswordConfirmed
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var current string
		if err := tx.NewSelect().Model((*account)(nil)).Column("password_hash").Where("id = ?", u.ID).For("UPDATE").Scan(ctx, &current); err != nil {
			return err
		}
		used, err := useConfirmedFactor(ctx, tx, u.ID, confirm.Factor)
		confirmed.MFA = !errors.Is(err, ErrMFANotEnabled)
		if !confirmed.MFA {
			err = confirm.Password(current)
		}
		if err != nil {
			return err
		}
		hash, err := replace(current)
		if err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*account)(nil)).Set("password_hash = ?", hash).Where("id = ?", u.ID).Exec(ctx); err != nil {
			return err
		}
		// A reset link requested before the change must not undo it.
		if err := deletePasswordReset(ctx, tx, u.ID); err != nil {
			return err
		}
		others, err := deleteSessions(ctx, tx, "user_id = ? AND token_hash <> ?", u.ID, sessionHash)
		if err != nil {
			return err
		}
		if _, err := deleteSessions(ctx, tx, "token_hash = ?", sessionHash); err != nil {
			return err
		}
		if _, err := deleteAdminSessions(ctx, tx, u.ID); err != nil {
			return err
		}
		if used != nil {
			confirmed.FactorUsed = *used
		}
		return s.record(ctx, tx, events.PasswordChanged{User: u.PublicID, OtherSessionsRevoked: others}, EventScope{UserID: u.ID})
	})
	return confirmed, err
}

// DeleteAccount deletes the user, with their sessions, memberships, reset
// link, admin row and admin sessions, and the events about them (ON DELETE
// CASCADE), records auth.account_deleted, and erases the account's email
// (and the name beside it) from the events that remain
// (docs/compliance.md#erasure): pseudonym is auth.Keys.EmailPseudonym, so
// events already minimised are found too. It locks the user's row first,
// as everything that locks both a user and a workspace does
// (CreateInvitation's lockInviter, SuspendUser), so the two orders never
// meet in a deadlock; then every workspace they own, in id order, then
// refuses with a LastOwnerError if any of them would be left without an
// owner. Every membership change takes the same workspace lock, so two
// co-owners deleting their accounts at once can't both leave; and taking the
// locks in id order means two such deletions can't deadlock.
func (s *Store) DeleteAccount(ctx context.Context, u User, pseudonym func(string) string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		target, err := lockAccount(ctx, tx, "u.id = ?", u.ID)
		if err != nil {
			return err
		}
		if _, err := lockOwnedWorkspaces(ctx, tx, u.ID); err != nil {
			return err
		}
		if err := refuseLastOwner(ctx, tx, u.ID); err != nil {
			return err
		}
		return s.eraseAccount(ctx, tx, target, pseudonym)
	})
}
