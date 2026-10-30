package store

import (
	"context"
	"github.com/uptrace/bun"
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
type authAttempt struct {
	bun.BaseModel `bun:"table:auth_attempts,alias:a"`
	Key           string    `bun:",pk"`
	Attempts      int       `bun:",notnull"`
	ResetsAt      time.Time `bun:",notnull"`
}

func (s *Store) CreateSession(ctx context.Context, userID int64, hash, csrf, oldHash string, expiry time.Time) error {
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
		_, err := tx.NewInsert().Model(&session).Exec(ctx)
		return err
	})
}

// Session refreshes idle time and returns sql.ErrNoRows for unknown, expired, or idle sessions.
func (s *Store) Session(ctx context.Context, hash string) (Session, error) {
	active := s.DB.NewUpdate().Model((*sessionRecord)(nil)).
		Set("last_seen_at = now()").
		Where("token_hash = ? AND expires_at > now() AND last_seen_at > now() - interval '30 minutes'", hash).
		Returning("user_id, csrf_token, created_at, expires_at")
	var session Session
	err := s.DB.NewSelect().
		With("active", active).
		TableExpr("active AS a").
		Join("JOIN users AS u ON u.id = a.user_id").
		ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at, a.csrf_token, a.created_at, a.expires_at").
		Scan(ctx, &session.User.ID, &session.User.PublicID, &session.User.Name, &session.User.Email, &session.User.CreatedAt, &session.CSRF, &session.CreatedAt, &session.ExpiresAt)
	return session, err
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.DB.NewDelete().Model((*sessionRecord)(nil)).Where("token_hash = ?", hash).Exec(ctx)
	return err
}
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, hash string) error {
	_, err := s.DB.NewDelete().Model((*sessionRecord)(nil)).Where("user_id = ? AND token_hash <> ?", userID, hash).Exec(ctx)
	return err
}

// Persistent limits work across restarts and multiple API processes. No proxy header is trusted.
func (s *Store) AllowAuth(ctx context.Context, key string, limit int) (bool, error) {
	attempt := authAttempt{Key: key, Attempts: 1}
	err := s.DB.NewInsert().Model(&attempt).
		Value("resets_at", "now() + interval '15 minutes'").
		On("CONFLICT (key) DO UPDATE").
		Set("attempts = CASE WHEN a.resets_at <= now() THEN 1 ELSE a.attempts + 1 END").
		Set("resets_at = CASE WHEN a.resets_at <= now() THEN now() + interval '15 minutes' ELSE a.resets_at END").
		Returning("attempts").
		Scan(ctx)
	return attempt.Attempts <= limit, err
}
func (s *Store) Cleanup(ctx context.Context) error {
	if _, err := s.DB.NewDelete().Model((*authAttempt)(nil)).Where("resets_at <= now()").Exec(ctx); err != nil {
		return err
	}
	_, err := s.DB.NewDelete().Model((*sessionRecord)(nil)).
		Where("expires_at <= now() OR last_seen_at <= now() - interval '30 minutes'").
		Exec(ctx)
	return err
}
