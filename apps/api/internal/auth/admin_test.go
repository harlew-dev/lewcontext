package auth

import "testing"

func TestAdminKeyMatches(t *testing.T) {
	key := "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90"
	if !AdminKeyMatches(key, key) {
		t.Fatal("the key itself")
	}
	for _, given := range []string{"", key[:35], key + "0", "8A1F0C52-6D3E-4B7A-9C2D-5E8F1A3B7C90"} {
		if AdminKeyMatches(given, key) {
			t.Errorf("%q matched", given)
		}
	}
	if AdminKeyMatches("", "") {
		t.Fatal("admin off matched an empty key")
	}
}
