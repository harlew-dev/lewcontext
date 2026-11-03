package apitest

import (
	"context"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"net/http"
	"strings"
	"testing"
	"time"
)

// MFAAccount is a user with MFA on: its authenticator and two recovery codes.
type MFAAccount struct {
	User  factory.User
	TOTP  factory.MFATOTP
	Codes []factory.RecoveryCode
}

// WithMFA creates a user with MFA on.
func WithMFA(t *testing.T, db bun.IDB, f *gofakeit.Faker) MFAAccount {
	t.Helper()
	ctx := context.Background()
	user := Must(factory.Users.CreateOne(ctx, db, f))(t)
	totp := Must(factory.MFATOTPs.State(factory.TOTPFor(user.ID)).CreateOne(ctx, db, f))(t)
	codes := Must(factory.RecoveryCodes.State(factory.RecoveryCodesFor(user.ID)).Create(ctx, db, f, 2))(t)
	return MFAAccount{User: user, TOTP: totp, Codes: codes}
}

// CodeAt is the authenticator's code offset steps from now.
func (a MFAAccount) CodeAt(offset int64) string {
	return auth.TOTPCode(a.TOTP.Plain, auth.TOTPStep(time.Now())+offset)
}

// StartMFALogin signs in with the password and returns the challenge cookie.
func StartMFALogin(t *testing.T, h http.Handler, email string) *http.Cookie {
	t.Helper()
	w := Call(t, h, "POST", "/api/auth/login", LoginBody(email), "", nil, 200)
	if strings.TrimSpace(w.Body.String()) != `{"mfaRequired":true}` {
		t.Fatalf("login with MFA on: %s", w.Body.String())
	}
	if CookieNamed(w, auth.CookieName(false)) != nil {
		t.Fatal("the password alone started a session")
	}
	challenge := CookieNamed(w, auth.MFACookieName(false))
	if challenge == nil || !challenge.HttpOnly || challenge.SameSite != http.SameSiteStrictMode || challenge.MaxAge != 300 {
		t.Fatalf("challenge cookie: %+v", challenge)
	}
	return challenge
}
