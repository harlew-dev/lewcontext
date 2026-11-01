package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// Suspended accounts (docs/api/admin.md#suspending-an-account): a
// user_suspensions row while suspended. A suspended account has no session
// (they are deleted with the suspension, FindSession and FindAdminSession
// never find one, and CreateSession and CreateAdminSession refuse it), gets
// no reset link (IssuePasswordReset finds no account, ResetPassword refuses
// a link) and is never an admin (SuspendUser refuses one, GrantAdmin a
// suspended account). Its workspaces keep working for everyone else.

type userSuspension struct {
	bun.BaseModel `bun:"table:user_suspensions,alias:us"`
	UserID        int64     `bun:",pk"`
	Reason        string    `bun:",notnull"`
	SuspendedBy   int64     `bun:",nullzero"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

var (
	// ErrSuspended refuses a session, a reset or admin for a suspended
	// account.
	ErrSuspended = errors.New("the account is suspended")
	// ErrAlreadySuspended: SuspendUser on an account already suspended.
	ErrAlreadySuspended = errors.New("the account is already suspended")
	// ErrNotSuspended: UnsuspendUser on an account that isn't.
	ErrNotSuspended = errors.New("the account isn't suspended")
)

// suspendedUser is a subquery that finds userColumn's suspension, for
// "NOT EXISTS (?)". userColumn is a constant column name, never input.
func suspendedUser(db bun.IDB, userColumn string) *bun.SelectQuery {
	return db.NewSelect().Model((*userSuspension)(nil)).ColumnExpr("1").Where("us.user_id = " + userColumn)
}

// checkNotSuspended returns ErrSuspended when userID is suspended, inside
// tx (under the user's row lock, which SuspendUser also takes).
func checkNotSuspended(ctx context.Context, db bun.IDB, userID int64) error {
	suspended, err := db.NewSelect().Model((*userSuspension)(nil)).Where("user_id = ?", userID).Exists(ctx)
	if err == nil && suspended {
		err = ErrSuspended
	}
	return err
}

// Suspension is what suspending an account does beyond ending its access:
// RevokeInvitations also revokes every live invitation it sent (the
// dashboard's default).
type Suspension struct {
	Reason            string
	RevokeInvitations bool
}

// Suspended is what a suspension ended: the sessions signed out (app and
// admin ones) and the invitations revoked.
type Suspended struct {
	User               User
	SessionsRevoked    int
	InvitationsRevoked int
}

// SuspendUser suspends the account publicID, in one transaction with its
// user row locked: the suspension, every session (and admin session) of
// it deleted, its reset link and sign-in challenges deleted, and, with
// in.RevokeInvitations, every live invitation it sent revoked
// (workspace.invitation.revoked each). It records auth.suspended and,
// with an admin (the dashboard), admin.user.suspended; with a nil admin
// it's the command line (by cli, the reason in auth.suspended).
// sql.ErrNoRows for an unknown account, ErrAlreadySuspended, and
// ErrActionOnAdmin for an admin, from the command line too (revoke admin
// first); from the dashboard, ErrActionOnSelf.
func (s *Store) SuspendUser(ctx context.Context, publicID string, in Suspension, admin *AdminAction) (Suspended, error) {
	var done Suspended
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u, err := s.lockSuspensionTarget(ctx, tx, publicID, admin)
		if err != nil {
			return err
		}
		if err := checkNotSuspended(ctx, tx, u.ID); errors.Is(err, ErrSuspended) {
			return ErrAlreadySuspended
		} else if err != nil {
			return err
		}
		row := userSuspension{UserID: u.ID, Reason: in.Reason}
		if admin != nil {
			row.SuspendedBy = admin.Admin.ID
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		done = Suspended{User: u}
		if done.SessionsRevoked, err = endAccess(ctx, tx, u.ID); err != nil {
			return err
		}
		if in.RevokeInvitations {
			if done.InvitationsRevoked, err = s.revokeInvitations(ctx, tx, "i.invited_by = ? AND i.expires_at > now()", u.ID); err != nil {
				return err
			}
		}
		return s.recordSuspension(ctx, tx, done, in.Reason, admin)
	})
	return done, err
}

// lockSuspensionTarget locks the account to suspend or unsuspend:
// lockActionTarget's checks from the dashboard; from the command line
// (admin nil) only that it exists and, to suspend, isn't an admin.
func (s *Store) lockSuspensionTarget(ctx context.Context, tx bun.Tx, publicID string, admin *AdminAction) (User, error) {
	if admin != nil {
		return lockActionTarget(ctx, tx, publicID, *admin)
	}
	return lockActionTarget(ctx, tx, publicID, AdminAction{})
}

// endAccess deletes every way back in a suspended account had: its app and
// admin sessions (counted together), its reset link and its sign-in
// challenges.
func endAccess(ctx context.Context, tx bun.Tx, userID int64) (int, error) {
	sessions, err := deleteSessions(ctx, tx, "user_id = ?", userID)
	if err != nil {
		return 0, err
	}
	adminSessions, err := deleteAdminSessions(ctx, tx, userID)
	if err != nil {
		return 0, err
	}
	if err := deletePasswordReset(ctx, tx, userID); err != nil {
		return 0, err
	}
	_, err = tx.NewDelete().Model((*mfaChallenge)(nil)).Where("user_id = ?", userID).Exec(ctx)
	return sessions + adminSessions, err
}

// recordSuspension records auth.suspended and, from the dashboard,
// admin.user.suspended.
func (s *Store) recordSuspension(ctx context.Context, tx bun.Tx, done Suspended, reason string, admin *AdminAction) error {
	fact := events.Suspended{User: done.User.PublicID, By: events.SuspendedByCLI, SessionsRevoked: done.SessionsRevoked, InvitationsRevoked: done.InvitationsRevoked, Reason: reason}
	scope := EventScope{UserID: done.User.ID}
	if admin != nil {
		fact.By, fact.Reason, scope.ActorID = events.SuspendedByAdmin, "", admin.Admin.ID
	}
	if err := s.record(ctx, tx, fact, scope); err != nil || admin == nil {
		return err
	}
	e := events.AdminUserSuspended{Admin: admin.Admin.PublicID, User: done.User.PublicID, Reason: reason, SessionsRevoked: done.SessionsRevoked, InvitationsRevoked: done.InvitationsRevoked}
	return s.record(ctx, tx, e, EventScope{ActorID: admin.Admin.ID})
}

// UnsuspendUser lifts the suspension of the account publicID: it can sign
// in again. It records auth.unsuspended and, with an admin,
// admin.user.unsuspended. sql.ErrNoRows for an unknown account,
// ErrNotSuspended; from the dashboard, ErrActionOnSelf (an admin is never
// suspended, so ErrActionOnAdmin can't arise but is kept).
func (s *Store) UnsuspendUser(ctx context.Context, publicID, reason string, admin *AdminAction) (User, error) {
	var u User
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if u, err = s.lockSuspensionTarget(ctx, tx, publicID, admin); err != nil {
			return err
		}
		res, err := tx.NewDelete().Model((*userSuspension)(nil)).Where("user_id = ?", u.ID).Exec(ctx)
		if err != nil {
			return err
		}
		if err := oneRow(res); errors.Is(err, sql.ErrNoRows) {
			return ErrNotSuspended
		} else if err != nil {
			return err
		}
		fact := events.Unsuspended{User: u.PublicID, By: events.SuspendedByCLI, Reason: reason}
		scope := EventScope{UserID: u.ID}
		if admin != nil {
			fact.By, fact.Reason, scope.ActorID = events.SuspendedByAdmin, "", admin.Admin.ID
		}
		if err := s.record(ctx, tx, fact, scope); err != nil || admin == nil {
			return err
		}
		e := events.AdminUserUnsuspended{Admin: admin.Admin.PublicID, User: u.PublicID, Reason: reason}
		return s.record(ctx, tx, e, EventScope{ActorID: admin.Admin.ID})
	})
	return u, err
}

// UserSuspension is an account's suspension as admins see it: why, by
// whom (nil from the command line or once that account is gone) and when.
type UserSuspension struct {
	Reason      string       `json:"reason"`
	SuspendedBy *AdminPerson `json:"suspendedBy"`
	SuspendedAt time.Time    `json:"suspendedAt"`
}

// findUserSuspension returns the account's suspension, or nil.
func findUserSuspension(ctx context.Context, db bun.IDB, userID int64) (*UserSuspension, error) {
	var row struct {
		Reason    string         `bun:"reason"`
		CreatedAt time.Time      `bun:"created_at"`
		ByID      sql.NullString `bun:"by_id"`
		ByName    sql.NullString `bun:"by_name"`
		ByEmail   sql.NullString `bun:"by_email"`
	}
	err := db.NewSelect().Model((*userSuspension)(nil)).
		ColumnExpr("us.reason, us.created_at, u.public_id AS by_id, u.name AS by_name, u.email AS by_email").
		Join("LEFT JOIN users AS u ON u.id = us.suspended_by").
		Where("us.user_id = ?", userID).
		Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	found := &UserSuspension{Reason: row.Reason, SuspendedAt: row.CreatedAt}
	if row.ByID.Valid {
		found.SuspendedBy = &AdminPerson{PublicID: row.ByID.String, Name: row.ByName.String, Email: row.ByEmail.String}
	}
	return found, nil
}
