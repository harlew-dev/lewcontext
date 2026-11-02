package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"strings"
	"time"
)

type EmailChange struct {
	bun.BaseModel `bun:"table:email_changes,alias:ec"`
	UserID        int64 `bun:",pk"`
	SessionHash   string
	PreviousEmail string
	Email         string
	EmailVersion  int64
	RequestID     string
	CodeHash      string `bun:",nullzero"`
	Attempts      int
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Code          string `bun:"-"`
}

// EmailChanges builds issued, live requests. Set the user/session with
// EmailChangeFor; EmailChangeSending and ExpiredEmailChange cover other states.
var EmailChanges = Define(func(f *gofakeit.Faker) EmailChange {
	code, now := mustEmailCode(), time.Now().UTC()
	return EmailChange{Email: strings.ToLower(f.LetterN(12)) + "@example.test", RequestID: "evt_" + f.LetterN(14), CodeHash: Keys.EmailCodeDigest(code), Code: code, CreatedAt: now, ExpiresAt: now.Add(auth.EmailCodeLifetime)}
})

func EmailChangeFor(u User, session Session) func(*EmailChange) {
	return func(p *EmailChange) {
		p.UserID, p.SessionHash, p.PreviousEmail, p.EmailVersion = u.ID, session.TokenHash, u.Email, u.EmailVersion
	}
}
func EmailChangeSending(p *EmailChange) { p.CodeHash, p.Code = "", "" }
func ExpiredEmailChange(p *EmailChange) {
	p.CreatedAt = time.Now().Add(-16 * time.Minute)
	p.ExpiresAt = p.CreatedAt.Add(auth.EmailCodeLifetime)
}

type UserEmailHistory struct {
	bun.BaseModel `bun:"table:user_email_history,alias:eh"`
	ID            int64 `bun:",pk,autoincrement"`
	UserID        int64
	Email         string
	ReleasedAt    time.Time
}

// UserEmailHistories builds former-address erasure references.
var UserEmailHistories = Define(func(f *gofakeit.Faker) UserEmailHistory {
	return UserEmailHistory{Email: strings.ToLower(f.LetterN(12)) + "@example.test", ReleasedAt: time.Now().Add(-time.Hour)}
})
