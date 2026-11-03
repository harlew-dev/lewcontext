package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"time"
)

// EmailChangeMail is the listener's private issuance result. Existing belongs
// only to the listener, never to an API response or the requester's export.
type EmailChangeMail struct {
	User     User
	Email    string
	Existing *User
}

// IssueEmailChangeCode correlates an issuance/retry with the latest live
// request, current account generation and originating session. Retries do not
// extend the deadline or recreate a missing row.
func (s *Store) IssueEmailChangeCode(ctx context.Context, request, codeHash string) (EmailChangeMail, error) {
	var out EmailChangeMail
	var candidate emailChange
	err := s.DB.NewSelect().Model(&candidate).Where("request_id = ?", request).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNoEmailChange
	}
	if err != nil {
		return out, err
	}
	err = s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		a, err := lockEmailChangeAccount(ctx, tx, candidate.UserID, candidate.SessionHash)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrSuspended) {
			return ErrNoEmailChange
		}
		if err != nil {
			return err
		}
		var row emailChange
		err = tx.NewSelect().Model(&row).Where("user_id = ? AND request_id = ? AND expires_at > clock_timestamp()", a.ID, request).For("UPDATE").Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoEmailChange
		}
		if err != nil {
			return err
		}
		if row.SessionHash != candidate.SessionHash || row.PreviousEmail != a.Email || row.EmailVersion != a.EmailVersion {
			return ErrNoEmailChange
		}
		if _, err := tx.NewUpdate().Model((*emailChange)(nil)).Set("code_hash = ?", codeHash).Where("user_id = ?", a.ID).Exec(ctx); err != nil {
			return err
		}
		out.User, out.Email = a.User, row.Email
		var existing User
		err = tx.NewSelect().Model(&existing).Where("email = ?", row.Email).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err == nil {
			out.Existing = &existing
		}
		return err
	})
	return out, err
}

// FindEmailChangeRecipient returns only an account that already owned email
// when the request occurred. A later registration or A→B→A must get no notice
// about an earlier ownership period.
func (s *Store) FindEmailChangeRecipient(ctx context.Context, email string, requestedAt time.Time) (User, error) {
	var u User
	err := s.DB.NewSelect().Model(&u).Where("email = ? AND created_at <= ?", email, requestedAt).
		Where("email_changed_at IS NULL OR email_changed_at <= ?", requestedAt).Scan(ctx)
	return u, err
}

// FindEmailChangeNotice reloads a queued event's destinations rather than
// trusting a previously decoded payload: erasure may have redacted/deleted it.
// The event and its account must still exist; minimised data cannot be mailed.
type EmailChangeNotice struct {
	User                 User
	Email, PreviousEmail string
}

func (s *Store) FindEmailChangeNotice(ctx context.Context, event, name string) (EmailChangeNotice, error) {
	var out EmailChangeNotice
	err := s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("u.id, u.public_id, e.payload->>'email', coalesce(e.payload->>'previousEmail', '')").
		Join("JOIN users AS u ON u.id = e.user_id").
		Where("e.public_id = ? AND e.name = ? AND e.minimised_at IS NULL", event, name).
		Scan(ctx, &out.User.ID, &out.User.PublicID, &out.Email, &out.PreviousEmail)
	return out, err
}
