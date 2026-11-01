package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

type sessionRecord struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`
	TokenHash     string    `bun:",pk"`
	UserID        int64     `bun:",notnull"`
	CSRFToken     string    `bun:"csrf_token,notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	LastSeenAt    time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

// CreateSession starts a session for the user, replacing the one oldHash
// identifies, and records e (such as auth.logged_in) with it when e isn't nil.
// ErrSuspended for a suspended account, checked under the user's lock,
// which SuspendUser takes too: whatever the path to a session, a suspended
// account never gets one.
func (s *Store) CreateSession(ctx context.Context, userID int64, hash, csrf, oldHash string, expiry time.Time, e events.Event) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Serialize session creation per account so the cap survives concurrent logins.
		if _, err := tx.NewSelect().Model((*User)(nil)).Column("id").Where("id = ?", userID).For("UPDATE").Exec(ctx); err != nil {
			return err
		}
		if err := checkNotSuspended(ctx, tx, userID); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*sessionRecord)(nil)).
			Where("token_hash = ? OR expires_at <= now() OR last_seen_at <= now() - interval '30 minutes'", oldHash).
			Exec(ctx); err != nil {
			return err
		}
		// Cap concurrent sessions per account to five.
		olderThanNewestFour := tx.NewSelect().Model((*sessionRecord)(nil)).Column("token_hash").
			Where("user_id = ?", userID).
			Order("created_at DESC").
			Offset(4)
		if _, err := tx.NewDelete().Model((*sessionRecord)(nil)).Where("token_hash IN (?)", olderThanNewestFour).Exec(ctx); err != nil {
			return err
		}
		session := sessionRecord{TokenHash: hash, UserID: userID, CSRFToken: csrf, ExpiresAt: expiry}
		if _, err := tx.NewInsert().Model(&session).Exec(ctx); err != nil || e == nil {
			return err
		}
		return s.record(ctx, tx, e, EventScope{ActorID: userID, UserID: userID})
	})
}

// FindSession refreshes idle time and returns sql.ErrNoRows for unknown,
// expired, or idle sessions, and for a suspended account's: suspending
// deletes them, and this is the backstop.
func (s *Store) FindSession(ctx context.Context, hash string) (Session, error) {
	active := s.DB.NewUpdate().Model((*sessionRecord)(nil)).
		Set("last_seen_at = now()").
		Where("token_hash = ? AND expires_at > now() AND last_seen_at > now() - interval '30 minutes'", hash).
		Where("NOT EXISTS (?)", suspendedUser(s.DB, "s.user_id")).
		Returning("user_id, csrf_token, created_at, expires_at")
	var session Session
	err := s.DB.NewSelect().
		With("active", active).
		TableExpr("active AS a").
		Join("JOIN users AS u ON u.id = a.user_id").
		ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at, u.email_verified_at, a.csrf_token, a.created_at, a.expires_at").
		ColumnExpr("(?)", mfaEnabledAt(s.DB)).
		Scan(ctx, &session.User.ID, &session.User.PublicID, &session.User.Name, &session.User.Email, &session.User.CreatedAt, &session.User.EmailVerifiedAt, &session.CSRF, &session.CreatedAt, &session.ExpiresAt, &session.User.MFAEnabledAt)
	return session, err
}

// deleteSessions deletes the sessions matching where and counts them.
func deleteSessions(ctx context.Context, db bun.IDB, where string, args ...any) (int, error) {
	result, err := db.NewDelete().Model((*sessionRecord)(nil)).Where(where, args...).Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// DeleteSession signs the user's session out and records auth.logged_out.
func (s *Store) DeleteSession(ctx context.Context, u User, hash string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := deleteSessions(ctx, tx, "token_hash = ?", hash); err != nil {
			return err
		}
		return s.record(ctx, tx, events.LoggedOut{User: u.PublicID}, EventScope{UserID: u.ID})
	})
}

// DeleteOtherSessions signs out every session of the user's except hash and
// records auth.other_sessions_revoked.
func (s *Store) DeleteOtherSessions(ctx context.Context, u User, hash string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		count, err := deleteSessions(ctx, tx, "user_id = ? AND token_hash <> ?", u.ID, hash)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, events.OtherSessionsRevoked{User: u.PublicID, Count: count}, EventScope{UserID: u.ID})
	})
}

// Cleanup deletes used-up rate-limit windows, ended sessions (admin ones
// too), expired reset and block links, expired IP blocks, sign-in challenges, sign-ups and
// email codes, MFA setups left unconfirmed, and invitations that expired over 30 days ago (until then they're listed as expired, so
// whoever sent one can see why it wasn't used).
func (s *Store) Cleanup(ctx context.Context) error {
	if _, err := s.DB.NewDelete().Model((*mfaChallenge)(nil)).Where("expires_at <= now()").Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*mfaTOTP)(nil)).
		Where("confirmed_at IS NULL AND created_at <= ?", time.Now().Add(-MFASetupLifetime)).
		Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*rateLimitRecord)(nil)).Where("resets_at <= now()").Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*sessionRecord)(nil)).
		Where("expires_at <= now() OR last_seen_at <= now() - interval '30 minutes'").
		Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*adminSessionRecord)(nil)).
		Where("expires_at <= now() OR last_seen_at <= now() - make_interval(secs => ?)", adminIdleSeconds).
		Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*passwordReset)(nil)).Where("expires_at <= now()").Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*pendingRegistration)(nil)).Where("expires_at <= now()").Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*emailVerification)(nil)).Where("expires_at <= now()").Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*emailBlockLink)(nil)).Where("expires_at <= now()").Exec(ctx); err != nil {
		return err
	}
	if _, err := s.DB.NewDelete().Model((*ipBlockRecord)(nil)).Where("expires_at <= now()").Exec(ctx); err != nil {
		return err
	}
	_, err := s.DB.NewDelete().Model((*invitationRecord)(nil)).Where("expires_at <= now() - interval '30 days'").Exec(ctx)
	return err
}

// RevokeAllSessions signs every account out, app and admin sessions alike,
// ends every sign-in waiting for its code (the app's and the admin door's,
// so a password typed before can't become a session after), and records
// auth.sessions_revoked_all with how many of each, with no actor (the
// operator's command line, admin sessions revoke-all): for a suspected leak
// of the session tables. Everyone signs in again with their password (and
// code).
func (s *Store) RevokeAllSessions(ctx context.Context) (events.SessionsRevokedAll, error) {
	var e events.SessionsRevokedAll
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if e.Sessions, err = deleteSessions(ctx, tx, "TRUE"); err != nil {
			return err
		}
		if e.AdminSessions, err = deleteAll(ctx, tx, (*adminSessionRecord)(nil)); err != nil {
			return err
		}
		if e.Challenges, err = deleteAll(ctx, tx, (*mfaChallenge)(nil)); err != nil {
			return err
		}
		return s.record(ctx, tx, e, EventScope{})
	})
	return e, err
}

// deleteAll deletes every row of model's table, returning how many.
func deleteAll(ctx context.Context, tx bun.Tx, model any) (int, error) {
	result, err := tx.NewDelete().Model(model).Where("TRUE").Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// CountSessions counts what revoke-all would end, for its preview: the app
// and admin sessions, and the sign-ins waiting for a code.
func (s *Store) CountSessions(ctx context.Context) (events.SessionsRevokedAll, error) {
	var counts events.SessionsRevokedAll
	for _, count := range []struct {
		model any
		into  *int
	}{
		{(*sessionRecord)(nil), &counts.Sessions},
		{(*adminSessionRecord)(nil), &counts.AdminSessions},
		{(*mfaChallenge)(nil), &counts.Challenges},
	} {
		n, err := s.DB.NewSelect().Model(count.model).Count(ctx)
		if err != nil {
			return counts, err
		}
		*count.into = int(n)
	}
	return counts, nil
}
