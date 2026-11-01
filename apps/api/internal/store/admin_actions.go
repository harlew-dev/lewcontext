package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
)

// The admin dashboard's account-and-access actions
// (plans/features/admin/actions.md). Each runs in one transaction with its
// admin.* event (the admin, the target and the reason) and the store's own
// events for the change, which carry the admin as actor (the request's
// context, events.WithActor). The admin.* events have no user or workspace
// scope, as admins' reads don't: deleting the account or workspace acted on
// keeps the record. Never on yourself, and never on another admin apart from
// signing them out: suspending an admin or resetting an admin's MFA stays on
// the command line, so one compromised admin account can't lock the others
// out.

// AdminAction is an admin acting from the dashboard, and the reason they
// gave (10 to 500 characters, checked by the controller).
type AdminAction struct {
	Admin  User
	Reason string
}

var (
	// ErrActionOnSelf refuses an admin action on the admin's own account.
	ErrActionOnSelf = errors.New("an admin can't act on their own account")
	// ErrActionOnAdmin refuses an action on another admin: only signing one
	// out is allowed from the dashboard.
	ErrActionOnAdmin = errors.New("only the command line acts on another admin")
	// ErrNothingToRevoke: the workspace has no live invitation.
	ErrNothingToRevoke = errors.New("nothing to revoke")
)

// lockActionTarget locks the account publicID for an admin's action on it
// and returns it. sql.ErrNoRows when there is none; ErrActionOnSelf for the
// acting admin; ErrActionOnAdmin for another admin.
func lockActionTarget(ctx context.Context, tx bun.Tx, publicID string, a AdminAction) (User, error) {
	var u User
	if err := tx.NewSelect().Model(&u).Where("u.public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
		return u, err
	}
	if u.ID == a.Admin.ID {
		return u, ErrActionOnSelf
	}
	admin, err := isAdmin(ctx, tx, u.ID)
	if err == nil && admin {
		err = ErrActionOnAdmin
	}
	return u, err
}

// RevokeUserSessions signs every session of the account publicID out
// (Sign out everywhere), and ends any sign-in waiting for its code,
// recording admin.user.signed_out. Errors as lockActionTarget's.
func (s *Store) RevokeUserSessions(ctx context.Context, publicID string, a AdminAction) (int, error) {
	var count int
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u, err := lockActionTarget(ctx, tx, publicID, a)
		if err != nil {
			return err
		}
		if count, err = deleteSessions(ctx, tx, "user_id = ?", u.ID); err != nil {
			return err
		}
		if err := deleteChallenges(ctx, tx, u.ID); err != nil {
			return err
		}
		e := events.AdminUserSignedOut{Admin: a.Admin.PublicID, User: u.PublicID, Reason: a.Reason, SessionsRevoked: count}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
	return count, err
}

// ResetUserMFA is admin reset-mfa from the dashboard, for someone who lost
// their authenticator and recovery codes: MFA off (the secret, recovery
// codes and sign-in challenges deleted) and every session signed out,
// recording auth.mfa.disabled (reason reset) and admin.user.mfa_reset.
// Never an admin's (ErrActionOnAdmin): admins must keep MFA, and only the
// command line resets it, reopening their setup window. ErrMFANotEnabled
// without an authenticator, set up or pending; otherwise lockActionTarget's
// errors.
func (s *Store) ResetUserMFA(ctx context.Context, publicID string, a AdminAction) (int, error) {
	var count int
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u, err := lockActionTarget(ctx, tx, publicID, a)
		if err != nil {
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
		if count, err = deleteSessions(ctx, tx, "user_id = ?", u.ID); err != nil {
			return err
		}
		disabled := events.MFADisabled{User: u.PublicID, Reason: events.MFADisabledReset}
		if err := s.record(ctx, tx, disabled, EventScope{ActorID: a.Admin.ID, UserID: u.ID}); err != nil {
			return err
		}
		e := events.AdminUserMFAReset{Admin: a.Admin.PublicID, User: u.PublicID, Reason: a.Reason, SessionsRevoked: count}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
	return count, err
}

// RevokeAdminSessions signs another admin out: every admin session and app
// session of theirs (a lost laptop, a session flagged as moved), and any
// sign-in of theirs waiting for its code, recording
// admin.admin.signed_out. sql.ErrNoRows for an unknown account,
// ErrNotAdmin for one that isn't an admin, ErrActionOnSelf for the acting
// admin (who signs their own other sessions out with
// DeleteOtherAdminSessions).
func (s *Store) RevokeAdminSessions(ctx context.Context, publicID string, a AdminAction) (adminSessions, sessions int, err error) {
	err = s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u, err := lockActionTarget(ctx, tx, publicID, a)
		if err == nil {
			return ErrNotAdmin
		}
		if !errors.Is(err, ErrActionOnAdmin) {
			return err
		}
		if adminSessions, err = deleteAdminSessions(ctx, tx, u.ID); err != nil {
			return err
		}
		if sessions, err = deleteSessions(ctx, tx, "user_id = ?", u.ID); err != nil {
			return err
		}
		if err := deleteChallenges(ctx, tx, u.ID); err != nil {
			return err
		}
		e := events.AdminAdminSignedOut{Admin: a.Admin.PublicID, User: u.PublicID, Reason: a.Reason, AdminSessionsRevoked: adminSessions, SessionsRevoked: sessions}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
	return adminSessions, sessions, err
}

// RevokeWorkspaceInvitations revokes every live invitation of the workspace
// publicID, so their links stop working (an invitation-spam run stopped
// without touching the workspace's data), recording
// workspace.invitation.revoked for each and
// admin.workspace.invitations_revoked. It takes the workspace lock first,
// as RevokeInvitation and accepting do. sql.ErrNoRows for an unknown
// workspace, ErrNothingToRevoke when none is live (nothing is recorded).
func (s *Store) RevokeWorkspaceInvitations(ctx context.Context, publicID string, a AdminAction) (int, error) {
	var count int
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var w Workspace
		if err := tx.NewSelect().Model(&w).Column("w.id", "w.public_id").Where("w.public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		revoked, err := s.revokeInvitations(ctx, tx, "i.workspace_id = ? AND i.expires_at > now()", w.ID)
		if err != nil {
			return err
		}
		if count = revoked; count == 0 {
			return ErrNothingToRevoke
		}
		e := events.AdminWorkspaceInvitationsRevoked{Admin: a.Admin.PublicID, Workspace: w.PublicID, Reason: a.Reason, Count: count}
		return s.record(ctx, tx, e, EventScope{ActorID: a.Admin.ID})
	})
	return count, err
}

// revokedInvitation is a deleted invitation, as RETURNING gives it.
type revokedInvitation struct {
	PublicID    string `bun:"public_id"`
	WorkspaceID int64  `bun:"workspace_id"`
	Email       string `bun:"email"`
}

// revokeInvitations deletes the invitations matching where (on alias i)
// and records workspace.invitation.revoked for each, returning how many.
func (s *Store) revokeInvitations(ctx context.Context, tx bun.Tx, where string, args ...any) (int, error) {
	var rows []revokedInvitation
	err := tx.NewDelete().Model((*invitationRecord)(nil)).
		Where(where, args...).
		Returning("i.public_id, i.workspace_id, i.email").
		Scan(ctx, &rows)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	ids := make([]int64, len(rows))
	for i, inv := range rows {
		ids[i] = inv.WorkspaceID
	}
	var workspaces []Workspace
	if err := tx.NewSelect().Model(&workspaces).Column("w.id", "w.public_id").Where("w.id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return 0, err
	}
	publicIDs := make(map[int64]string, len(workspaces))
	for _, w := range workspaces {
		publicIDs[w.ID] = w.PublicID
	}
	for _, inv := range rows {
		e := events.WorkspaceInvitationRevoked{Invitation: inv.PublicID, Workspace: publicIDs[inv.WorkspaceID], Email: inv.Email}
		if err := s.record(ctx, tx, e, EventScope{WorkspaceID: inv.WorkspaceID}); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// deleteChallenges ends the user's sign-ins waiting for a code (the app's
// and the admin door's), so a sign-out from the dashboard can't be
// followed by a session from a password typed just before.
func deleteChallenges(ctx context.Context, tx bun.Tx, userID int64) error {
	_, err := tx.NewDelete().Model((*mfaChallenge)(nil)).Where("user_id = ?", userID).Exec(ctx)
	return err
}
