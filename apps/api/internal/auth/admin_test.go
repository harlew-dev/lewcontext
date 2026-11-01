package auth

import "testing"

func TestMatchAdminKey(t *testing.T) {
	key := "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90"
	previous := "ZPycpaTc6D_ZYGNirUyhk42QUAMY-uT4M3ji3CEnzWx"
	if MatchAdminKey(key, key, "") != AdminKeyCurrent || MatchAdminKey(key, key, previous) != AdminKeyCurrent {
		t.Fatal("the key itself")
	}
	if MatchAdminKey(previous, key, previous) != AdminKeyPrevious {
		t.Fatal("the previous key")
	}
	if MatchAdminKey(previous, key, "") != AdminKeyNone {
		t.Fatal("a previous key matched once it was removed")
	}
	for _, given := range []string{"", key[:35], key + "0", "8A1F0C52-6D3E-4B7A-9C2D-5E8F1A3B7C90", previous[:42]} {
		if got := MatchAdminKey(given, key, previous); got != AdminKeyNone {
			t.Errorf("%q matched: %d", given, got)
		}
	}
	if MatchAdminKey("", "", "") != AdminKeyNone || MatchAdminKey("", key, "") != AdminKeyNone {
		t.Fatal("an empty key matched an unset one")
	}
	if MatchAdminKey(previous, "", previous) != AdminKeyNone {
		t.Fatal("admin off, yet the previous key matched")
	}
}

func TestMatchAdminKeyHash(t *testing.T) {
	current, previous := Digest("current-key"), Digest("previous-key")
	cases := map[string]AdminKeyMatch{current: AdminKeyCurrent, previous: AdminKeyPrevious, Digest("other"): AdminKeyNone, "": AdminKeyNone}
	for hash, want := range cases {
		if got := MatchAdminKeyHash(hash, current, previous); got != want {
			t.Errorf("%q: %d, want %d", hash, got, want)
		}
	}
	if MatchAdminKeyHash(previous, current, "") != AdminKeyNone || MatchAdminKeyHash("", current, "") != AdminKeyNone {
		t.Fatal("a session under an unset previous key matched")
	}
}

func TestAdminRoles(t *testing.T) {
	for _, c := range []struct {
		have, need AdminRole
		can        bool
	}{
		{AdminViewer, AdminViewer, true}, {AdminOperator, AdminViewer, true}, {AdminOperator, AdminOperator, true},
		{AdminViewer, AdminOperator, false}, {"", AdminViewer, false}, {"owner", AdminViewer, false}, {AdminOperator, "root", false},
	} {
		if got := c.have.Can(c.need); got != c.can {
			t.Errorf("%q can %q: %v", c.have, c.need, got)
		}
	}
	if r, ok := ParseAdminRole("operator"); !ok || r != AdminOperator {
		t.Fatal("operator")
	}
	if _, ok := ParseAdminRole("Operator"); ok {
		t.Fatal("a role in another case")
	}
}
