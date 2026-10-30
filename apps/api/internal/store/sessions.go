package store

import (
	"context"
	"time"
)

func (s *Store) CreateSession(ctx context.Context, userID int64, hash, csrf, oldHash string, expiry time.Time) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize session creation per account so the cap survives concurrent logins.
	if _, err = tx.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1 OR expires_at<=now() OR last_seen_at<=now()-interval '30 minutes'", oldHash); err != nil {
		return err
	}
	// Cap concurrent sessions per account to five.
	if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE token_hash IN (SELECT token_hash FROM sessions WHERE user_id=$1 ORDER BY created_at DESC OFFSET 4)", userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,$4)", hash, userID, csrf, expiry); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Session(ctx context.Context, hash string) (Session, error) {
	var session Session
	err := s.DB.QueryRow(ctx, `WITH active AS (
 UPDATE sessions SET last_seen_at=now() WHERE token_hash=$1 AND expires_at>now() AND last_seen_at>now()-interval '30 minutes'
 RETURNING user_id,csrf_token,created_at,expires_at)
 SELECT u.id,u.name,u.email,u.created_at,a.csrf_token,a.created_at,a.expires_at FROM active a JOIN users u ON u.id=a.user_id`, hash).Scan(&session.User.ID, &session.User.Name, &session.User.Email, &session.User.CreatedAt, &session.CSRF, &session.CreatedAt, &session.ExpiresAt)
	return session, err
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", hash)
	return err
}
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, hash string) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1 AND token_hash<>$2", userID, hash)
	return err
}

// Persistent limits work across restarts and multiple API processes. No proxy header is trusted.
func (s *Store) AllowAuth(ctx context.Context, key string, limit int) (bool, error) {
	var attempts int
	err := s.DB.QueryRow(ctx, `INSERT INTO auth_attempts(key,attempts,resets_at) VALUES($1,1,now()+interval '15 minutes')
 ON CONFLICT(key) DO UPDATE SET attempts=CASE WHEN auth_attempts.resets_at<=now() THEN 1 ELSE auth_attempts.attempts+1 END,
 resets_at=CASE WHEN auth_attempts.resets_at<=now() THEN now()+interval '15 minutes' ELSE auth_attempts.resets_at END RETURNING attempts`, key).Scan(&attempts)
	return attempts <= limit, err
}
func (s *Store) Cleanup(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM auth_attempts WHERE resets_at<=now(); DELETE FROM sessions WHERE expires_at<=now() OR last_seen_at<=now()-interval '30 minutes'")
	return err
}
