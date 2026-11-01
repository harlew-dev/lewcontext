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

// seenEvery is how stale users.last_seen_at may get before a session check
// moves it: active accounts are counted by the day, so to the hour is enough.
const seenEvery = time.Hour

// CreateSession starts a session for the user, replacing the one oldHash
// identifies, and records e (such as auth.logged_in) with it when e isn't nil.
func (s *Store) CreateSession(ctx context.Context, userID int64, hash, csrf, oldHash string, expiry time.Time, e events.Event) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Serialize session creation per account so the cap survives concurrent logins.
		if _, err := tx.NewSelect().Model((*User)(nil)).Column("id").Where("id = ?", userID).For("UPDATE").Exec(ctx); err != nil {
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

// FindSession refreshes idle time and returns sql.ErrNoRows for unknown, expired, or idle sessions.
// In the same statement it moves the account's users.last_seen_at, only
// when that's over an hour old (seenEvery), so an active account costs one
// row write an hour, and no event (docs/api/monitoring.md).
func (s *Store) FindSession(ctx context.Context, hash string) (Session, error) {
	active := s.DB.NewUpdate().Model((*sessionRecord)(nil)).
		Set("last_seen_at = now()").
		Where("token_hash = ? AND expires_at > now() AND last_seen_at > now() - interval '30 minutes'", hash).
		Returning("user_id, csrf_token, created_at, expires_at")
	seen := s.DB.NewUpdate().Model((*User)(nil)).
		Set("last_seen_at = now()").
		Where("u.id IN (SELECT a.user_id FROM active AS a)").
		Where("u.last_seen_at IS NULL OR u.last_seen_at < now() - make_interval(secs => ?)", int(seenEvery.Seconds()))
	var session Session
	err := s.DB.NewSelect().
		With("active", active).
		With("seen", seen).
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
// too), expired reset and block links, sign-in challenges, sign-ups and
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
	_, err := s.DB.NewDelete().Model((*invitationRecord)(nil)).Where("expires_at <= now() - interval '30 days'").Exec(ctx)
	return err
}
