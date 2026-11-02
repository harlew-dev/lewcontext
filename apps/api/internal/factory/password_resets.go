package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"time"
)

type PasswordReset struct {
	bun.BaseModel `bun:"table:password_resets,alias:pr"`
	UserID        int64     `bun:",pk"`
	TokenHash     string    `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
	// Token is the raw token in the link; like a real one, only its digest is stored.
	Token string `bun:"-"`
}

// PasswordResets builds live reset links that expire in 30 minutes. Tokens
// come from crypto/rand, like session tokens. Set UserID, or use ResetFor.
var PasswordResets = Define(func(f *gofakeit.Faker) PasswordReset {
	token := mustToken()
	return PasswordReset{
		TokenHash: auth.Digest(token),
		ExpiresAt: time.Now().Add(30 * time.Minute),
		Token:     token,
	}
})

// ResetFor makes the link the user's.
func ResetFor(userID int64) func(*PasswordReset) {
	return func(r *PasswordReset) { r.UserID = userID }
}

// ExpiredReset is past its expiry.
func ExpiredReset(r *PasswordReset) {
	r.ExpiresAt = time.Now().Add(-time.Second)
}
