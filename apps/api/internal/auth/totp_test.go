package auth

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The SHA-1 test vectors from RFC 6238, appendix B, cut to 6 digits (the
// RFC's are 8; the last 6 are the same code).
func TestTOTPCodeMatchesRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	vectors := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
	for unix, want := range vectors {
		if got := TOTPCode(secret, TOTPStep(time.Unix(unix, 0))); got != want {
			t.Errorf("at %d: %s, want %s", unix, got, want)
		}
	}
}

func TestCheckTOTP(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	step := TOTPStep(now)
	code := func(offset int64) string { return TOTPCode(secret, step+offset) }

	for _, offset := range []int64{-1, 0, 1} {
		got, ok := CheckTOTP(secret, code(offset), now, 0)
		if !ok || got != step+offset {
			t.Errorf("offset %d refused: %d %v", offset, got, ok)
		}
	}
	for _, offset := range []int64{-2, 2} {
		if _, ok := CheckTOTP(secret, code(offset), now, 0); ok {
			t.Errorf("offset %d accepted: only one step of drift is allowed", offset)
		}
	}
	// A code works once: its step must come after the last one used.
	if _, ok := CheckTOTP(secret, code(0), now, step); ok {
		t.Error("a code was accepted twice")
	}
	if _, ok := CheckTOTP(secret, code(-1), now, step); ok {
		t.Error("an older code was accepted after a newer one")
	}
	if got, ok := CheckTOTP(secret, code(1), now, step); !ok || got != step+1 {
		t.Error("the next step's code was refused after this one")
	}
	for _, bad := range []string{"", "12345", "1234567", "12345a", " 123456", "１２３４５６"} {
		if _, ok := CheckTOTP(secret, bad, now, 0); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, ok := CheckTOTP([]byte("another secret, 20 b"), code(0), now, 0); ok {
		t.Error("another secret's code accepted")
	}
}

func TestGenerateTOTPSecret(t *testing.T) {
	a, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateTOTPSecret()
	if len(a) != TOTPSecretSize || bytes.Equal(a, b) {
		t.Fatalf("secrets: %x %x", a, b)
	}
	if encoded := EncodeTOTPSecret(a); len(encoded) != 32 || strings.Contains(encoded, "=") {
		t.Fatalf("encoded secret %q: want 32 base32 characters without padding", encoded)
	}
}

func TestTOTPURI(t *testing.T) {
	secret := []byte("12345678901234567890")
	raw := TOTPURI("Acme App", "ada+test@example.com", secret)
	if !strings.HasPrefix(raw, "otpauth://totp/Acme%20App:ada%2Btest%40example.com?") {
		t.Fatalf("label not escaped for authenticator apps: %s", raw)
	}
	if strings.Contains(raw, "+") {
		t.Fatalf("a literal + reads as a space in some apps: %s", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" || q.Get("issuer") != "Acme App" ||
		q.Get("algorithm") != "SHA1" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Fatalf("query: %v", q)
	}
}
