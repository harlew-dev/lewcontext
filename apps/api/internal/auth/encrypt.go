package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

// AppKeySize is the length of APP_KEY, decoded: 32 bytes.
const AppKeySize = 32

// GenerateAppKey makes a new APP_KEY: 32 random bytes in base64.
func GenerateAppKey() (string, error) {
	key := make([]byte, AppKeySize)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// sealVersion is the first byte of every sealed value, so the key can be
// rotated later without guessing which key sealed what.
const sealVersion = 1

var ErrUnsealable = errors.New("sealed value can't be opened")

// Keys are the keys derived from APP_KEY: one for AES-256-GCM, which
// seals secrets the server must read back (TOTP secrets), two for HMAC
// digests of recovery codes and emailed codes, so they can't be guessed
// offline from a database dump, and one for the admin build's assets pass
// (admin_assets.go). Deriving each (HKDF, its own label) keeps any from being
// another.
type Keys struct {
	aead        cipher.AEAD
	recovery    []byte
	emailCode   []byte
	adminAssets []byte
}

// NewKeys derives the keys from the 32-byte APP_KEY.
func NewKeys(key []byte) (*Keys, error) {
	if len(key) != AppKeySize {
		return nil, errors.New("APP_KEY must be 32 bytes")
	}
	sealKey, err := hkdf.Key(sha256.New, key, nil, "lewcontext seal v1", 32)
	if err != nil {
		return nil, err
	}
	recovery, err := hkdf.Key(sha256.New, key, nil, "lewcontext recovery-codes v1", 32)
	if err != nil {
		return nil, err
	}
	emailCode, err := hkdf.Key(sha256.New, key, nil, "lewcontext email-codes v1", 32)
	if err != nil {
		return nil, err
	}
	adminAssets, err := hkdf.Key(sha256.New, key, nil, "lewcontext admin-assets-pass v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(sealKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Keys{aead: aead, recovery: recovery, emailCode: emailCode, adminAssets: adminAssets}, nil
}

// Seal encrypts plaintext for the row of owner (a user's internal ID): it
// opens only with the same owner, so a value copied onto another user's row
// is useless. The result is version ‖ nonce ‖ ciphertext and tag.
func (k *Keys) Seal(plaintext []byte, owner int64) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{sealVersion}, nonce...)
	return k.aead.Seal(out, nonce, plaintext, ownerData(owner)), nil
}

// Open decrypts a value Seal made for owner.
func (k *Keys) Open(sealed []byte, owner int64) ([]byte, error) {
	size := k.aead.NonceSize()
	if len(sealed) < 1+size+k.aead.Overhead() || sealed[0] != sealVersion {
		return nil, ErrUnsealable
	}
	plaintext, err := k.aead.Open(nil, sealed[1:1+size], sealed[1+size:], ownerData(owner))
	if err != nil {
		return nil, ErrUnsealable
	}
	return plaintext, nil
}

// RecoveryCodeDigest is what's stored for a recovery code: an HMAC-SHA256,
// keyed so it can't be checked without APP_KEY. Pass the normalised
// code (NormalizeRecoveryCode).
func (k *Keys) RecoveryCodeDigest(code string) string {
	mac := hmac.New(sha256.New, k.recovery)
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

// EmailCodeDigest is what's stored for an emailed verification code: an
// HMAC-SHA256 like RecoveryCodeDigest's, under its own key. A million codes
// would make a plain hash trivial to reverse. Pass the normalised code
// (NormalizeEmailCode).
func (k *Keys) EmailCodeDigest(code string) string {
	mac := hmac.New(sha256.New, k.emailCode)
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

// ownerData is the GCM additional data binding a sealed value to its row.
func ownerData(owner int64) []byte {
	return binary.BigEndian.AppendUint64(nil, uint64(owner))
}
