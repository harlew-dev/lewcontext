package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
)

// DestinationSealer uses a separate domain from MFA and binds each field to
// its destination identity and kind. It deliberately has no owner-user context.
type DestinationSealer struct{ aead cipher.AEAD }

func NewDestinationSealer(key []byte) (*DestinationSealer, error) {
	if len(key) != AppKeySize {
		return nil, ErrUnsealable
	}
	derived, err := hkdf.Key(sha256.New, key, nil, "lewcontext alert-destinations v1", 32)
	if err != nil {
		return nil, ErrUnsealable
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, ErrUnsealable
	}
	aead, err := cipher.NewGCM(block)
	return &DestinationSealer{aead: aead}, err
}

func (s *DestinationSealer) Seal(value []byte, kind, identity, field string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(append([]byte{1}, nonce...), nonce, value, destinationData(kind, identity, field)), nil
}

func (s *DestinationSealer) Open(value []byte, kind, identity, field string) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(value) < 1+n+s.aead.Overhead() || value[0] != 1 {
		return nil, ErrUnsealable
	}
	plain, err := s.aead.Open(nil, value[1:1+n], value[1+n:], destinationData(kind, identity, field))
	if err != nil {
		return nil, ErrUnsealable
	}
	return plain, nil
}

func destinationData(kind, identity, field string) []byte {
	return []byte("alert-destination-v1\x00" + kind + "\x00" + identity + "\x00" + field)
}
