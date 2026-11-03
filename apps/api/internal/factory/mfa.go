package factory

import (
	"crypto/rand"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"net/http"
	"time"
)

// AppKey is the APP_KEY that factories seal MFA secrets and
// digest recovery codes with: random per process, so only a server given
// this key (config.Config.AppKey, as the integration tests do) can
// read them.
var AppKey = randomKey()

// Keys are AppKey's keys.
var Keys = mustKeys(AppKey)

type MFATOTP struct {
	bun.BaseModel `bun:"table:mfa_totp,alias:t"`
	UserID        int64 `bun:",pk"`
	Secret        []byte
	LastUsedStep  int64
	ConfirmedAt   *time.Time
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	// Plain is the secret before sealing, for making codes with
	// auth.TOTPCode. Set it before TOTPFor, which seals it.
	Plain []byte `bun:"-"`
}

// MFATOTPs builds confirmed authenticators (MFA on). Use TOTPFor, which sets
// the user and seals the secret for them; PendingTOTP leaves setup pending.
var MFATOTPs = Define(func(f *gofakeit.Faker) MFATOTP {
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		panic(fmt.Sprintf("generate TOTP secret: %v", err))
	}
	confirmed := time.Now()
	return MFATOTP{Plain: secret, ConfirmedAt: &confirmed}
})

// TOTPFor makes the authenticator the user's, sealing Plain for them.
func TOTPFor(userID int64) func(*MFATOTP) {
	return func(t *MFATOTP) {
		sealed, err := Keys.Seal(t.Plain, userID)
		if err != nil {
			panic(fmt.Sprintf("seal TOTP secret: %v", err))
		}
		t.UserID, t.Secret = userID, sealed
	}
}

// PendingTOTP is a setup waiting for its first code.
func PendingTOTP(t *MFATOTP) {
	t.ConfirmedAt = nil
}

// Code is the authenticator's code now.
func (t MFATOTP) Code() string {
	return auth.TOTPCode(t.Plain, auth.TOTPStep(time.Now()))
}

type RecoveryCode struct {
	bun.BaseModel `bun:"table:mfa_recovery_codes,alias:rc"`
	ID            int64 `bun:",pk,autoincrement"`
	UserID        int64
	CodeHash      string
	UsedAt        *time.Time
	// Code is the code as shown to the user; only its digest is stored.
	Code string `bun:"-"`
}

// RecoveryCodes builds unused recovery codes, digested with Keys. Use
// RecoveryCodesFor.
var RecoveryCodes = Define(func(f *gofakeit.Faker) RecoveryCode {
	codes, err := auth.GenerateRecoveryCodes()
	if err != nil {
		panic(fmt.Sprintf("generate recovery code: %v", err))
	}
	normal, _ := auth.NormalizeRecoveryCode(codes[0])
	return RecoveryCode{CodeHash: Keys.RecoveryCodeDigest(normal), Code: codes[0]}
})

// RecoveryCodesFor makes the codes the user's.
func RecoveryCodesFor(userID int64) func(*RecoveryCode) {
	return func(c *RecoveryCode) { c.UserID = userID }
}

type MFAChallenge struct {
	bun.BaseModel `bun:"table:mfa_challenges,alias:c"`
	TokenHash     string `bun:",pk"`
	UserID        int64
	EmailVersion  int64
	Purpose       string
	Attempts      int
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time
	// Token is the raw cookie value; only its digest is stored.
	Token string `bun:"-"`
}

// MFAChallenges builds live sign-in challenges. Use ChallengeFor.
var MFAChallenges = Define(func(f *gofakeit.Faker) MFAChallenge {
	token := mustToken()
	return MFAChallenge{TokenHash: auth.Digest(token), Purpose: "login", ExpiresAt: time.Now().Add(auth.MFAChallengeLifetime), Token: token}
})

// ChallengeFor makes the challenge the user's sign-in.
func ChallengeFor(userID int64) func(*MFAChallenge) {
	return func(c *MFAChallenge) { c.UserID = userID }
}

// ExpiredChallenge is past its expiry.
func ExpiredChallenge(c *MFAChallenge) {
	c.ExpiresAt = time.Now().Add(-time.Second)
}

// Cookie is the challenge cookie a browser would send in development.
func (c MFAChallenge) Cookie() *http.Cookie {
	return auth.MFACookie(c.Token, false)
}

func randomKey() []byte {
	key := make([]byte, auth.AppKeySize)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("generate encryption key: %v", err))
	}
	return key
}
func mustKeys(key []byte) *auth.Keys {
	keys, err := auth.NewKeys(key)
	if err != nil {
		panic(err)
	}
	return keys
}
