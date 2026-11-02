package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func keysFrom(t *testing.T, fill byte) *Keys {
	t.Helper()
	keys, err := NewKeys(bytes.Repeat([]byte{fill}, AppKeySize))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestAdminAssetsPass(t *testing.T) {
	keys := keysFrom(t, 7)
	keyHash := Digest("the-current-admin-route-key-0123456789")
	now := time.Unix(1_800_000_000, 0)
	pass := keys.AdminAssetsPass(keyHash, now.Add(AdminAssetsPassLifetime))
	if !keys.AdminAssetsPassValid(pass, keyHash, now) {
		t.Fatalf("a fresh pass %q is refused", pass)
	}
	if !keys.AdminAssetsPassValid(pass, keyHash, now.Add(AdminAssetsPassLifetime-time.Second)) {
		t.Fatal("a pass is refused before it ends")
	}
	expiry, mac, _ := strings.Cut(pass, ".")
	other := keysFrom(t, 8)
	refused := map[string]struct {
		pass, keyHash string
		keys          *Keys
		at            time.Time
	}{
		"ended":                {pass, keyHash, keys, now.Add(AdminAssetsPassLifetime)},
		"another key's digest": {pass, Digest("the-previous-admin-route-key-0123456789"), keys, now},
		"another APP_KEY":      {pass, keyHash, other, now},
		"a later expiry":       {strconv.FormatInt(now.Unix()+3600, 10) + "." + mac, keyHash, keys, now},
		"a flipped MAC":        {expiry + "." + flip(mac), keyHash, keys, now},
		"a short MAC":          {expiry + "." + mac[:len(mac)-2], keyHash, keys, now},
		"padded base64":        {pass + "=", keyHash, keys, now},
		"a leading zero":       {"0" + pass, keyHash, keys, now},
		"a plus sign":          {"+" + pass, keyHash, keys, now},
		"no MAC":               {expiry, keyHash, keys, now},
		"two dots":             {pass + ".x", keyHash, keys, now},
		"empty":                {"", keyHash, keys, now},
		"just a dot":           {".", keyHash, keys, now},
		"a long expiry":        {strings.Repeat("9", 13) + "." + mac, keyHash, keys, now},
		"spaces":               {" " + pass, keyHash, keys, now},
		// Signed, but no time a pass is made for.
		"a negative expiry": {keys.AdminAssetsPass(keyHash, time.Unix(-5, 0)), keyHash, keys, time.Unix(-6, 0)},
		"a zero expiry":     {keys.AdminAssetsPass(keyHash, time.Unix(0, 0)), keyHash, keys, time.Unix(-1, 0)},
	}
	for name, c := range refused {
		if c.keys.AdminAssetsPassValid(c.pass, c.keyHash, c.at) {
			t.Errorf("%s: accepted", name)
		}
	}
	// Admin off: no key, no digest that a pass could be made for by accident.
	if keys.AdminAssetsPassValid(pass, "", now) {
		t.Error("a pass is accepted with no key digest")
	}
}

// A pass may end at most its lifetime ahead (plus a minute for another
// server's clock): one made to last longer, even correctly signed, is refused.
func TestAdminAssetsPassCantOutliveItsLifetime(t *testing.T) {
	keys := keysFrom(t, 7)
	keyHash := Digest("the-current-admin-route-key-0123456789")
	now := time.Unix(1_800_000_000, 0)
	for ahead, valid := range map[time.Duration]bool{
		AdminAssetsPassLifetime + 59*time.Second: true,
		AdminAssetsPassLifetime + 2*time.Minute:  false,
		30 * 24 * time.Hour:                      false,
		0:                                        false,
		-time.Second:                             false,
	} {
		pass := keys.AdminAssetsPass(keyHash, now.Add(ahead))
		if got := keys.AdminAssetsPassValid(pass, keyHash, now); got != valid {
			t.Errorf("ending %v ahead: valid %v, want %v", ahead, got, valid)
		}
	}
}

// The pass's key is its own: not APP_KEY itself, nor any other key derived
// from it, and its MAC starts with the "admin-assets" domain.
func TestAdminAssetsKeyIsItsOwn(t *testing.T) {
	appKey := bytes.Repeat([]byte{7}, AppKeySize)
	keys := keysFrom(t, 7)
	for name, key := range map[string][]byte{"APP_KEY": appKey, "recovery": keys.recovery, "email codes": keys.emailCode} {
		if hmac.Equal(key, keys.adminAssets) {
			t.Errorf("the pass key is the %s key", name)
		}
	}
	keyHash := Digest("k")
	mac := hmac.New(sha256.New, keys.adminAssets)
	mac.Write([]byte("admin-assets" + keyHash))
	mac.Write([]byte{0, 0, 0, 0, 0x6b, 0x49, 0xd2, 0x00}) // 1_800_000_000
	want := "1800000000." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if got := keys.AdminAssetsPass(keyHash, time.Unix(1_800_000_000, 0)); got != want {
		t.Fatalf("pass %q, want %q", got, want)
	}
}

func TestAdminAssetsCookie(t *testing.T) {
	for _, production := range []bool{false, true} {
		c := AdminAssetsCookie("pass", production)
		if c.Path != "/admin-assets/" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure != production || c.MaxAge != 8*60*60 {
			t.Errorf("production=%v: %+v", production, c)
		}
		if production != strings.HasPrefix(c.Name, "__Secure-") {
			t.Errorf("production=%v: name %q", production, c.Name)
		}
	}
}

// flip changes the MAC's first character to another base64url one.
func flip(mac string) string {
	if mac[0] == 'A' {
		return "B" + mac[1:]
	}
	return "A" + mac[1:]
}
