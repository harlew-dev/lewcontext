package mail

import (
	"context"
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"time"
)

// InvitationStore is what the invitation listener needs: *store.Store.
type InvitationStore interface {
	IssueInvitationToken(ctx context.Context, publicID, hash string) (store.InvitationMail, error)
	Record(ctx context.Context, e events.Event, scope store.EventScope) error
}

// SendInvitation is the listener that answers
// workspace.invitation.created: it issues the invitation's link, emails it to
// the invitee, and records workspace.invitation.sent. An invitation revoked,
// accepted or expired meanwhile gets nothing.
//
// As with reset links, the token is made here, so it is never stored: the
// database keeps its digest and the mailbox the link. A delivery that runs
// again issues a new link, which replaces the first.
func SendInvitation(s InvitationStore, m *Mailer, origin string) func(context.Context, events.WorkspaceInvitationCreated, events.Delivered) error {
	return func(ctx context.Context, e events.WorkspaceInvitationCreated, d events.Delivered) error {
		if time.Since(d.OccurredAt) > store.InvitationLifetime {
			return nil
		}
		token, err := auth.Token()
		if err != nil {
			return err
		}
		inv, err := s.IssueInvitationToken(ctx, e.Invitation, auth.Digest(token))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		data := WorkspaceInvitationData{
			Workspace:    inv.WorkspaceName,
			Inviter:      inv.SenderName,
			InviterEmail: inv.SenderEmail,
			Role:         withArticle(inv.Role),
			URL:          origin + "/auth/invitations/" + token,
			Home:         origin + "/",
		}
		if err := m.Send(ctx, inv.Email, WorkspaceInvitation, data); err != nil {
			return err
		}
		sent := events.WorkspaceInvitationSent{Invitation: inv.Invitation, Workspace: inv.Workspace, Request: d.EventID}
		return s.Record(ctx, sent, store.EventScope{WorkspaceID: inv.WorkspaceID})
	}
}

// withArticle is a role as the email words it: "an admin", "a member".
func withArticle(role auth.Role) string {
	if role == auth.RoleAdmin || role == auth.RoleOwner {
		return "an " + string(role)
	}
	return "a " + string(role)
}
