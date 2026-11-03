package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Time-based one-time passwords (RFC 6238) as authenticator apps make them:
// HMAC-SHA1 of the 30-second step, 6 digits. SHA-1's collision weakness
// doesn't affect HMAC, and it's the one algorithm every app supports.
const (
	TOTPSecretSize = 20 // bytes: 160 bits, RFC 4226's recommended length
	totpPeriod     = 30 // seconds per step
	totpDigits     = 6
)

// totpEncoding is how secrets are shown and put in otpauth:// URIs: base32
// without padding.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a new random secret.
func GenerateTOTPSecret() ([]byte, error) {
	secret := make([]byte, TOTPSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// TOTPStep is the step a moment falls in: Unix time divided by 30 seconds.
func TOTPStep(t time.Time) int64 {
	return t.Unix() / totpPeriod
}

// TOTPCode is the 6-digit code for secret at step (RFC 4226's HOTP with the
// step as its counter).
func TOTPCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

// ValidTOTPCode reports whether code has a TOTP code's shape: 6 digits.
func ValidTOTPCode(code string) bool {
	if len(code) != totpDigits {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// CheckTOTP returns the step that code matches, within one step either side
// of now (phones drift), and only after last: a code works once, even inside
// its 90-second window. ok is false when no allowed step matches. Each step
// is compared in constant time, and all three are always compared.
func CheckTOTP(secret []byte, code string, now time.Time, last int64) (step int64, ok bool) {
	if !ValidTOTPCode(code) {
		return 0, false
	}
	current := TOTPStep(now)
	for candidate := current - 1; candidate <= current+1; candidate++ {
		match := subtle.ConstantTimeCompare([]byte(TOTPCode(secret, candidate)), []byte(code)) == 1
		if match && candidate > last && !ok {
			step, ok = candidate, true
		}
	}
	return step, ok
}

// EncodeTOTPSecret is the secret as people type it into an authenticator app.
func EncodeTOTPSecret(secret []byte) string {
	return totpEncoding.EncodeToString(secret)
}

// TOTPURI is the otpauth:// URI an authenticator app reads from the QR code,
// labelled "issuer:account".
func TOTPURI(issuer, account string, secret []byte) string {
	query := url.Values{
		"secret":    {EncodeTOTPSecret(secret)},
		"issuer":    {issuer},
		"algorithm": {"SHA1"},
		"digits":    {fmt.Sprint(totpDigits)},
		"period":    {fmt.Sprint(totpPeriod)},
	}
	label := labelEscape(issuer) + ":" + labelEscape(account)
	return "otpauth://totp/" + label + "?" + strings.ReplaceAll(query.Encode(), "+", "%20")
}

// labelEscape percent-encodes a label part. Some apps read "+" as a space,
// so spaces are %20 and a literal "+" (common in emails) is %2B.
func labelEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
