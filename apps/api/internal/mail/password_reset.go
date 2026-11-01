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

// ResetLinkLifetime is how long a reset link works.
const ResetLinkLifetime = 30 * time.Minute

// ResetStore is what the password reset listener needs: *store.Store.
type ResetStore interface {
	IssuePasswordReset(ctx context.Context, email, hash string, expires time.Time) (store.User, error)
	Record(ctx context.Context, e events.Event, scope store.EventScope) error
}

// PasswordReset is the listener that answers auth.password_reset.requested:
// it issues a reset link for the account using the email, sends it, and
// records auth.password_reset.sent. An email without an account gets nothing.
//
// The token is made here, not in the request, so it is never stored: the
// database keeps its digest and the mailbox the link. A delivery that runs
// again issues a new link, which replaces the first. Requests older than a
// link's lifetime are dropped, since retries can come an hour later.
func PasswordReset(s ResetStore, m *Mailer, origin string) func(context.Context, events.PasswordResetRequested, events.Delivered) error {
	return func(ctx context.Context, e events.PasswordResetRequested, d events.Delivered) error {
		if time.Since(d.OccurredAt) > ResetLinkLifetime {
			return nil
		}
		token, err := auth.Token()
		if err != nil {
			return err
		}
		user, err := s.IssuePasswordReset(ctx, e.Email, auth.Digest(token), time.Now().Add(ResetLinkLifetime))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		link := origin + "/auth/forgot-password/" + token
		if err := m.Send(ctx, user.Email, ForgotPassword, ForgotPasswordData{Name: user.Name, URL: link}); err != nil {
			return done(err)
		}
		sent := events.PasswordResetSent{User: user.PublicID, Email: user.Email, Request: d.EventID}
		return s.Record(ctx, sent, store.EventScope{UserID: user.ID})
	}
}
