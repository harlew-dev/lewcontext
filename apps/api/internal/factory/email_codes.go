package factory

import (
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"net/http"
	"time"
)

type PendingRegistration struct {
	bun.BaseModel `bun:"table:pending_registrations,alias:preg"`
	ID            int64 `bun:",pk,autoincrement"`
	Email         string
	Name          string
	PasswordHash  string
	TokenHash     string
	RequestID     string
	CodeHash      string `bun:",nullzero"`
	Attempts      int
	// The policy versions agreed to: nil unless RegistrationAgreedTo.
	TermsOfServiceVersion *time.Time
	PrivacyPolicyVersion  *time.Time
	CreatedAt             time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt             time.Time `bun:",notnull"`
	// Token is the raw token in the browser's cookie, and Code the code
	// emailed; only their digests are stored (Code's under Keys).
	Token string `bun:"-"`
	Code  string `bun:"-"`
}

// PendingRegistrations builds live sign-ups waiting for their code, with
// the code already sent. Set Email to sign up a particular address.
var PendingRegistrations = Define(func(f *gofakeit.Faker) PendingRegistration {
	token, code := mustToken(), mustEmailCode()
	return PendingRegistration{
		Email:        fmt.Sprintf("%s.%s@example.test", slug(f.FirstName()), f.LetterN(8)),
		Name:         f.Name(),
		PasswordHash: mustHash(),
		TokenHash:    auth.Digest(token),
		RequestID:    "evt_" + f.LetterN(14),
		CodeHash:     Keys.EmailCodeDigest(code),
		ExpiresAt:    time.Now().Add(auth.EmailCodeLifetime),
		Token:        token,
		Code:         code,
	}
})

// RegistrationFor signs up email.
func RegistrationFor(email string) func(*PendingRegistration) {
	return func(p *PendingRegistration) { p.Email = email }
}

// RegistrationAgreedTo is a sign-up that agreed to these policy versions.
func RegistrationAgreedTo(terms, privacy string) func(*PendingRegistration) {
	return func(p *PendingRegistration) {
		p.TermsOfServiceVersion, p.PrivacyPolicyVersion = mustDate(terms), mustDate(privacy)
	}
}

// ExpiredRegistration is past its expiry.
func ExpiredRegistration(p *PendingRegistration) {
	p.ExpiresAt = time.Now().Add(-time.Second)
}

// RegistrationCookie is the cookie the sign-up's browser holds.
func (p PendingRegistration) Cookie() *http.Cookie {
	return auth.RegistrationCookie(p.Token, false)
}

type EmailVerification struct {
	bun.BaseModel `bun:"table:email_verifications,alias:ev"`
	UserID        int64 `bun:",pk"`
	CodeHash      string
	Attempts      int
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
	// Code is the code emailed; only its digest (under Keys) is stored.
	Code string `bun:"-"`
}

// EmailVerifications builds a live code for an account verifying its email.
// Use VerificationFor.
var EmailVerifications = Define(func(f *gofakeit.Faker) EmailVerification {
	code := mustEmailCode()
	return EmailVerification{CodeHash: Keys.EmailCodeDigest(code), ExpiresAt: time.Now().Add(auth.EmailCodeLifetime), Code: code}
})

// VerificationFor makes the code the user's.
func VerificationFor(userID int64) func(*EmailVerification) {
	return func(v *EmailVerification) { v.UserID = userID }
}

// mustEmailCode panics only if the system random source fails.
func mustEmailCode() string {
	code, err := auth.GenerateEmailCode()
	if err != nil {
		panic(fmt.Sprintf("generate email code: %v", err))
	}
	return code
}
