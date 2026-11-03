package auth

import (
	"bytes"
	"testing"
)

func TestDestinationSealingContexts(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealer, err := NewDestinationSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := sealer.Seal([]byte("receiver secret"), "webhook", "dst_12345678901234", "url")
	if err != nil {
		t.Fatal(err)
	}
	for _, context := range [][3]string{{"webhook", "dst_12345678901234", "url"}, {"heartbeat", "dst_12345678901234", "url"}, {"webhook", "dst_00000000000000", "url"}, {"webhook", "dst_12345678901234", "secret"}} {
		plain, err := sealer.Open(ciphertext, context[0], context[1], context[2])
		valid := context == [3]string{"webhook", "dst_12345678901234", "url"}
		if valid && (err != nil || string(plain) != "receiver secret") {
			t.Fatal("round trip failed")
		}
		if !valid && err == nil {
			t.Fatal("substitution accepted")
		}
	}
	mfa, _ := NewKeys(key)
	if _, err := mfa.Open(ciphertext, 1); err == nil {
		t.Fatal("destination ciphertext accepted by MFA context")
	}
	other, _ := NewDestinationSealer(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(ciphertext, "webhook", "dst_12345678901234", "url"); err == nil {
		t.Fatal("other key accepted")
	}
}
