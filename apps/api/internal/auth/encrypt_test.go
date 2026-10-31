package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"
)

func testKeys(t *testing.T) *Keys {
	t.Helper()
	key := make([]byte, AppKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	keys, err := NewKeys(key)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestSealAndOpen(t *testing.T) {
	keys := testKeys(t)
	secret := []byte("a totp secret, 20 b.")
	sealed, err := keys.Seal(secret, 42)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("the sealed value holds the plaintext")
	}
	again, _ := keys.Seal(secret, 42)
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals of one value are identical: the nonce isn't random")
	}
	opened, err := keys.Open(sealed, 42)
	if err != nil || !bytes.Equal(opened, secret) {
		t.Fatalf("round trip: %q %v", opened, err)
	}

	tampered := slices.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	unknownVersion := slices.Clone(sealed)
	unknownVersion[0] = 2
	cases := map[string]func() ([]byte, error){
		"another owner":   func() ([]byte, error) { return keys.Open(sealed, 43) },
		"another key":     func() ([]byte, error) { return testKeys(t).Open(sealed, 42) },
		"tampered":        func() ([]byte, error) { return keys.Open(tampered, 42) },
		"unknown version": func() ([]byte, error) { return keys.Open(unknownVersion, 42) },
		"truncated":       func() ([]byte, error) { return keys.Open(sealed[:10], 42) },
		"empty":           func() ([]byte, error) { return keys.Open(nil, 42) },
	}
	for name, open := range cases {
		if _, err := open(); !errors.Is(err, ErrUnsealable) {
			t.Errorf("%s: %v, want ErrUnsealable", name, err)
		}
	}
}

func TestNewKeysNeeds32Bytes(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33, 64} {
		if _, err := NewKeys(make([]byte, size)); err == nil {
			t.Errorf("a %d-byte key was accepted", size)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("%d codes", len(codes))
	}
	seen := map[string]bool{}
	for _, code := range codes {
		if len(code) != 11 || code[5] != '-' || strings.ContainsAny(code, "ilou") || seen[code] {
			t.Fatalf("code %q: want xxxxx-xxxxx from Crockford's alphabet, unique", code)
		}
		seen[code] = true
		if normal, ok := NormalizeRecoveryCode(code); !ok || normal != strings.ReplaceAll(code, "-", "") {
			t.Fatalf("%q doesn't normalise: %q %v", code, normal, ok)
		}
	}

	for input, want := range map[string]string{
		"K7P2M-X9QFR":    "k7p2mx9qfr",
		" k7p2m x9qfr ":  "k7p2mx9qfr",
		"k7p2mx9qfr":     "k7p2mx9qfr",
		"k-7-p-2-mx9qfr": "k7p2mx9qfr",
	} {
		if got, ok := NormalizeRecoveryCode(input); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", input, got, ok, want)
		}
	}
	for _, bad := range []string{"", "123456", "k7p2m-x9qf", "k7p2m-x9qfrr", "k7p2m-x9qfi", "k7p2m_x9qfr"} {
		if _, ok := NormalizeRecoveryCode(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}

	keys, other := testKeys(t), testKeys(t)
	digest := keys.RecoveryCodeDigest("k7p2mx9qfr")
	if digest != keys.RecoveryCodeDigest("k7p2mx9qfr") || len(digest) != 64 {
		t.Fatalf("digest %q isn't stable hex", digest)
	}
	if digest == other.RecoveryCodeDigest("k7p2mx9qfr") {
		t.Fatal("the digest doesn't depend on the key")
	}
}

func TestGenerateAppKey(t *testing.T) {
	a, err := GenerateAppKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateAppKey()
	key, err := base64.StdEncoding.DecodeString(a)
	if err != nil || len(key) != AppKeySize || a == b {
		t.Fatalf("keys %q %q: %v", a, b, err)
	}
	if _, err := NewKeys(key); err != nil {
		t.Fatal(err)
	}
}
