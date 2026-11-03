package mail

import (
	"context"
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"strings"
	"time"
)

// EmailChangeStore supplies issuance, immutable destinations and the shared
// informational cap. No listener returns destination-account identity to UI.
type EmailChangeStore interface {
	AccountNoticeStore
	IssueEmailChangeCode(context.Context, string, string) (store.EmailChangeMail, error)
	FindEmailChangeRecipient(context.Context, string, time.Time) (store.User, error)
	FindEmailChangeNotice(context.Context, string, string) (store.EmailChangeNotice, error)
}

// EmailChange answers requested changes in either configuration mode. The
// event snapshots the mode so differently configured workers cannot issue an
// unsolicited code for a direct completion.
func EmailChange(s EmailChangeStore, m *Mailer, keys *auth.Keys, origin string, limits bool) func(context.Context, events.EmailChangeRequested, events.Delivered) error {
	return func(ctx context.Context, e events.EmailChangeRequested, d events.Delivered) error {
		if time.Since(d.OccurredAt) > auth.EmailCodeLifetime {
			return nil
		}
		notice, err := s.FindEmailChangeNotice(ctx, d.EventID, e.EventName())
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !mailAddress(notice.Email) {
			return nil
		}
		var existing *store.User
		if e.VerificationRequired {
			code, err := auth.GenerateEmailCode()
			if err != nil {
				return err
			}
			issued, err := s.IssueEmailChangeCode(ctx, d.EventID, keys.EmailCodeDigest(code))
			if errors.Is(err, store.ErrNoEmailChange) {
				return nil
			}
			if err != nil {
				return err
			}
			existing = issued.Existing
			if existing == nil {
				if err := m.Send(ctx, issued.Email, EmailChangeCode, VerifyEmailData{Code: auth.FormatEmailCode(code)}); err != nil {
					return done(err)
				}
				return s.Record(ctx, events.EmailChangeSent{User: notice.User.PublicID, Request: d.EventID, Kind: events.EmailChangeMailCode}, store.EventScope{UserID: notice.User.ID})
			}
		}
		// Recheck ownership for both modes: late deliveries must not warn an
		// account created after the request, including the changed account itself.
		u, err := s.FindEmailChangeRecipient(ctx, notice.Email, d.OccurredAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if u.ID == notice.User.ID || (existing != nil && existing.ID != u.ID) {
			return nil
		}
		allowed, err := allowAccountExists(ctx, s, u, d, limits)
		if err != nil || !allowed {
			return err
		}
		if err := m.Send(ctx, notice.Email, EmailChangeExists, EmailChangeNoticeData{SettingsURL: origin + "/app/settings"}); err != nil {
			return done(err)
		}
		return s.Record(ctx, events.EmailChangeSent{User: notice.User.PublicID, Request: d.EventID, ExistingAccount: true, Kind: events.EmailChangeMailExisting}, store.EventScope{UserID: notice.User.ID})
	}
}

// EmailChangedNotice is registered independently for each inbox. Retrying one
// failed delivery cannot resend the other, and never resolves a current email.
func EmailChangedNotice(s EmailChangeStore, m *Mailer, origin string, previous bool) func(context.Context, events.EmailChanged, events.Delivered) error {
	return func(ctx context.Context, e events.EmailChanged, d events.Delivered) error {
		notice, err := s.FindEmailChangeNotice(ctx, d.EventID, e.EventName())
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		email, template := notice.Email, EmailChangedCurrent
		kind := events.EmailChangeMailCurrent
		if previous {
			email, template = notice.PreviousEmail, EmailChangedPrevious
			kind = events.EmailChangeMailPrevious
		}
		if !mailAddress(email) {
			return nil
		}
		if err := m.Send(ctx, email, template, EmailChangeNoticeData{SettingsURL: origin + "/app/settings"}); err != nil {
			return done(err)
		}
		return s.Record(ctx, events.EmailChangeSent{User: notice.User.PublicID, Request: d.EventID, Kind: kind}, store.EventScope{UserID: notice.User.ID})
	}
}

func mailAddress(email string) bool {
	return email != "" && email != store.Redacted && !strings.HasPrefix(email, auth.EmailPseudonymPrefix)
}
