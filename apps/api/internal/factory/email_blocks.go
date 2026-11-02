package factory

import (
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"strings"
	"time"
)

type EmailBlockLink struct {
	bun.BaseModel `bun:"table:email_block_links,alias:ebl"`
	ID            int64     `bun:",pk,autoincrement"`
	TokenHash     string    `bun:",notnull"`
	Email         string    `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
	// Token is the raw token in the link; like a real one, only its digest is stored.
	Token string `bun:"-"`
}

// EmailBlockLinks builds live block links, as an invitation's, to a unique
// example.test address, working for a year. Use BlockLinkFor for another.
var EmailBlockLinks = Define(func(f *gofakeit.Faker) EmailBlockLink {
	token := mustToken()
	return EmailBlockLink{
		TokenHash: auth.Digest(token),
		Email:     fmt.Sprintf("invitee.%s@example.test", strings.ToLower(f.LetterN(10))),
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
		Token:     token,
	}
})

// BlockLinkFor sends the link to email.
func BlockLinkFor(email string) func(*EmailBlockLink) {
	return func(l *EmailBlockLink) { l.Email = email }
}

// ExpiredBlockLink is past its expiry.
func ExpiredBlockLink(l *EmailBlockLink) {
	l.ExpiresAt = time.Now().Add(-time.Second)
}

type EmailBlock struct {
	bun.BaseModel `bun:"table:email_blocks,alias:eb"`
	ID            int64     `bun:",pk,autoincrement"`
	Email         string    `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// EmailBlocks builds blocked addresses at example.test. Use Blocked for a
// given one.
var EmailBlocks = Define(func(f *gofakeit.Faker) EmailBlock {
	return EmailBlock{Email: fmt.Sprintf("blocked.%s@example.test", strings.ToLower(f.LetterN(10)))}
})

// Blocked blocks email.
func Blocked(email string) func(*EmailBlock) {
	return func(b *EmailBlock) { b.Email = strings.ToLower(email) }
}
