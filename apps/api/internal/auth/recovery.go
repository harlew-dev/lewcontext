package auth

import (
	"crypto/rand"
	"strings"
)

// Recovery codes sign in when the authenticator is lost: 10 per user, each
// working once. A code is 10 characters from Crockford's base32 alphabet (no
// I, L, O or U, so it can't be misread), 50 bits, shown as "k7p2m-x9qfr".
const (
	RecoveryCodeCount  = 10
	recoveryCodeLength = 10
	recoveryAlphabet   = "0123456789abcdefghjkmnpqrstvwxyz"
)

// GenerateRecoveryCodes returns a new set of codes, formatted for display.
func GenerateRecoveryCodes() ([]string, error) {
	codes := make([]string, RecoveryCodeCount)
	raw := make([]byte, recoveryCodeLength)
	for i := range codes {
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		var code strings.Builder
		for j, b := range raw {
			if j == recoveryCodeLength/2 {
				code.WriteByte('-')
			}
			// 256 is a multiple of 32, so every character is equally likely.
			code.WriteByte(recoveryAlphabet[b%32])
		}
		codes[i] = code.String()
	}
	return codes, nil
}

// NormalizeRecoveryCode turns a code as typed into its stored form: lower
// case, without hyphens or spaces. ok is false for anything that can't be a
// recovery code.
func NormalizeRecoveryCode(input string) (code string, ok bool) {
	input = strings.ToLower(input)
	var b strings.Builder
	for _, c := range input {
		switch {
		case c == '-' || c == ' ':
			continue
		case strings.ContainsRune(recoveryAlphabet, c):
			b.WriteRune(c)
		default:
			return "", false
		}
	}
	if b.Len() != recoveryCodeLength {
		return "", false
	}
	return b.String(), true
}
