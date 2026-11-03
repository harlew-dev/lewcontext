package auth

import (
	"bytes"
	"testing"
)

func TestEmailCodes(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		code, err := GenerateEmailCode()
		if err != nil {
			t.Fatal(err)
		}
		if normal, ok := NormalizeEmailCode(code); !ok || normal != code {
			t.Fatalf("generated %q doesn't normalise to itself", code)
		}
		seen[code] = true
	}
	if len(seen) < 190 {
		t.Fatalf("only %d distinct codes in 200", len(seen))
	}
	for typed, want := range map[string]string{"482913": "482913", "482 913": "482913", " 482-913 ": "482913", "000001": "000001"} {
		if got, ok := NormalizeEmailCode(typed); !ok || got != want {
			t.Errorf("%q: got %q %v", typed, got, ok)
		}
	}
	for _, bad := range []string{"", "12345", "1234567", "48291a", "４８２９１３", "k7p2m-x9qfr"} {
		if _, ok := NormalizeEmailCode(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if FormatEmailCode("482913") != "482 913" {
		t.Fatal("format")
	}
}

// The code digest has its own key: the same input under another derived key
// (or APP_KEY) differs.
func TestEmailCodeDigest(t *testing.T) {
	keys, err := NewKeys(bytes.Repeat([]byte{7}, AppKeySize))
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewKeys(bytes.Repeat([]byte{8}, AppKeySize))
	if err != nil {
		t.Fatal(err)
	}
	digest := keys.EmailCodeDigest("482913")
	if digest != keys.EmailCodeDigest("482913") || digest == keys.EmailCodeDigest("482914") ||
		digest == keys.RecoveryCodeDigest("482913") || digest == other.EmailCodeDigest("482913") {
		t.Fatal("email code digests aren't keyed apart")
	}
}
