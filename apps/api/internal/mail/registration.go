package mail

import (
	"context"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"time"
)

// AccountExistsPerAccount caps the "You already have an account" emails one
// account gets: 5 a day. Unlike every other limit it is counted here, in the
// listener, because a limit on the request would give the account away: only
// taken emails would be refused. The requester never sees it, and nothing
// they were promised is held back (docs/api/mail.md).
var AccountExistsPerAccount = struct {
	Name   string
	Limit  int
	Window time.Duration
}{"account-exists:per-account", 5, 24 * time.Hour}

// RegistrationStore is what the sign-up listener needs: *store.Store.
type RegistrationStore interface {
	IssueRegistrationCode(ctx context.Context, request, codeHash string) (email string, existing *store.User, err error)
	CountHit(ctx context.Context, key string, window time.Duration) (hits int, resetsAt time.Time, err error)
	Record(ctx context.Context, e events.Event, scope store.EventScope) error
}

// Registration is the listener that answers auth.registration.requested.
// It gives the sign-up a code and, for an email without an account, sends
// it; for an email with one, it sends "You already have an account" (within
// AccountExistsPerAccount) and the code, which nobody is sent, can never be
// entered. A blocked address gets no code (the code page's hint says why to
// its owner). Either way the sign-up looks the same to whoever started it.
// limits is false with RATE_LIMITS=off.
//
// Like the reset link, the code is made here and only its HMAC is stored. A
// rerun makes a new code, which replaces the first. Requests older than a
// code's lifetime are dropped.
func Registration(s RegistrationStore, m *Mailer, keys *auth.Keys, origin string, limits bool) func(context.Context, events.RegistrationRequested, events.Delivered) error {
	return func(ctx context.Context, e events.RegistrationRequested, d events.Delivered) error {
		if time.Since(d.OccurredAt) > auth.EmailCodeLifetime {
			return nil
		}
		code, err := auth.GenerateEmailCode()
		if err != nil {
			return err
		}
		email, existing, err := s.IssueRegistrationCode(ctx, d.EventID, keys.EmailCodeDigest(code))
		if errors.Is(err, store.ErrNoRegistration) {
			return nil
		}
		if err != nil {
			return err
		}
		if existing == nil {
			if err := m.Send(ctx, email, VerifyEmail, VerifyEmailData{Code: auth.FormatEmailCode(code)}); err != nil {
				return done(err)
			}
			return s.Record(ctx, events.RegistrationSent{Email: email, Request: d.EventID}, store.EventScope{})
		}
		return sendAccountExists(ctx, s, m, *existing, origin, d, limits)
	}
}

// sendAccountExists tells the account that someone tried to sign up with its
// email, unless it has had AccountExistsPerAccount of these today.
func sendAccountExists(ctx context.Context, s RegistrationStore, m *Mailer, u store.User, origin string, d events.Delivered, limits bool) error {
	allowed, err := allowAccountExists(ctx, s, u, d, limits)
	if err != nil || !allowed {
		return err
	}
	data := AccountExistsData{Name: u.Name, LoginURL: origin + "/auth/login?notice=account-exists", ResetURL: origin + "/auth/forgot-password"}
	if err := m.Send(ctx, u.Email, AccountExists, data); err != nil {
		return done(err)
	}
	sent := events.RegistrationSent{Email: u.Email, Request: d.EventID, ExistingAccount: true, User: u.PublicID}
	return s.Record(ctx, sent, store.EventScope{UserID: u.ID})
}

// allowAccountExists counts an "already have an account" email against the
// account's AccountExistsPerAccount, on the delivery's first attempt only, so
// retrying a failed send doesn't spend more of it. The first refusal in a
// window records rate_limit.reached, as a request's would.
func allowAccountExists(ctx context.Context, s RegistrationStore, u store.User, d events.Delivered, limits bool) (bool, error) {
	if !limits || d.Attempt > 1 {
		return true, nil
	}
	l := AccountExistsPerAccount
	hits, resetsAt, err := s.CountHit(ctx, l.Name+":"+auth.Digest(u.PublicID), l.Window)
	if err != nil || hits != l.Limit+1 {
		return hits <= l.Limit, err
	}
	reached := events.RateLimitReached{Limit: l.Name + ":block", User: u.PublicID, Window: int(l.Window.Seconds()), ResetsAt: resetsAt}
	return false, s.Record(ctx, reached, store.EventScope{UserID: u.ID})
}

// EmailVerificationStore is what the signed-in verification listener needs:
// *store.Store.
type EmailVerificationStore interface {
	IssueEmailVerification(ctx context.Context, publicID, codeHash string, expires time.Time) (store.User, error)
	Record(ctx context.Context, e events.Event, scope store.EventScope) error
}

// EmailVerification is the listener that answers
// auth.email_verification.requested: a code for the account, sent to the
// email it has, replacing any earlier one. An account verified meanwhile
// gets nothing.
func EmailVerification(s EmailVerificationStore, m *Mailer, keys *auth.Keys) func(context.Context, events.EmailVerificationRequested, events.Delivered) error {
	return func(ctx context.Context, e events.EmailVerificationRequested, d events.Delivered) error {
		if time.Since(d.OccurredAt) > auth.EmailCodeLifetime {
			return nil
		}
		code, err := auth.GenerateEmailCode()
		if err != nil {
			return err
		}
		user, err := s.IssueEmailVerification(ctx, e.User, keys.EmailCodeDigest(code), time.Now().Add(auth.EmailCodeLifetime))
		if errors.Is(err, store.ErrAlreadyVerified) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := m.Send(ctx, user.Email, VerifyEmail, VerifyEmailData{Code: auth.FormatEmailCode(code)}); err != nil {
			return done(err)
		}
		return s.Record(ctx, events.EmailVerificationSent{User: user.PublicID, Request: d.EventID}, store.EventScope{UserID: user.ID})
	}
}
