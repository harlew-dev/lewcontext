package factory

import (
	"context"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"net/http"
	"time"
)

type Session struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`
	TokenHash     string    `bun:",pk"`
	UserID        int64     `bun:",notnull"`
	CSRFToken     string    `bun:"csrf_token,notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	LastSeenAt    time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
	// Token is the raw cookie value; like a real login, only its digest is stored.
	Token string `bun:"-"`
}

// Sessions builds live sessions. Tokens come from crypto/rand, not the faker,
// so they are as unguessable as real ones. Set UserID, or use SessionFor.
var Sessions = Define(func(f *gofakeit.Faker) Session {
	token, csrf := mustToken(), mustToken()
	now := time.Now()
	return Session{
		TokenHash:  auth.Digest(token),
		CSRFToken:  csrf,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(24 * time.Hour),
		Token:      token,
	}
})

// SessionFor signs a user in without the rate-limited login endpoint.
func SessionFor(ctx context.Context, db bun.IDB, f *gofakeit.Faker, userID int64) (Session, error) {
	return Sessions.State(func(s *Session) { s.UserID = userID }).CreateOne(ctx, db, f)
}

// Cookie is the session cookie a browser would send in development.
func (s Session) Cookie() *http.Cookie {
	return auth.Cookie(s.Token, false)
}

// Expired is past its absolute expiry.
func Expired(s *Session) {
	s.ExpiresAt = time.Now().Add(-time.Second)
}

// Idle has been unused for longer than the 30-minute idle timeout.
func Idle(s *Session) {
	s.LastSeenAt = time.Now().Add(-31 * time.Minute)
}

// mustToken panics only if the system random source fails.
func mustToken() string {
	token, err := auth.Token()
	if err != nil {
		panic(fmt.Sprintf("generate session token: %v", err))
	}
	return token
}
