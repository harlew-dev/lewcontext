package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The admin build's files (/admin-assets/…, docs/api/admin.md) open only with
// an assets pass: a cookie the admin page gate sets on every 200 admin page.
// It is stateless, so any server with the same APP_KEY accepts it:
//
//	<expiry, unix seconds>.<base64url HMAC-SHA256(k, "admin-assets" ‖ admin key digest ‖ expiry)>
//
// k is derived from APP_KEY for this alone (Keys.adminAssets). The digest is
// always the CURRENT ADMIN_ROUTE_KEY's, whichever key the page was served
// under (a previous key, once ADMIN_ROUTE_KEY_PREVIOUS exists): a pass signed
// with any other digest is invalid, so rotating the key ends every pass.
const (
	// AdminAssetsPassLifetime matches an admin session's maximum: a pass
	// rarely ends mid-visit, and when it does the page reloads through the
	// gate, which renews it.
	AdminAssetsPassLifetime = AdminSessionLifetime
	// adminAssetsClockSkew is how far past the lifetime an expiry may be, for
	// a pass made by another server whose clock runs a little ahead.
	adminAssetsClockSkew = time.Minute
	// AdminAssetsPath is the cookie's path: only requests for the admin
	// build's files carry it.
	AdminAssetsPath = "/admin-assets/"
)

// adminAssetsDomain starts every MAC's input, so it can't be another HMAC's.
const adminAssetsDomain = "admin-assets"

// AdminAssetsPass makes a pass for the admin key whose digest is keyHash
// (AdminKeyHash: the current key's), ending at expires.
func (k *Keys) AdminAssetsPass(keyHash string, expires time.Time) string {
	expiry := expires.Unix()
	mac := k.adminAssetsMAC(keyHash, expiry)
	return strconv.FormatInt(expiry, 10) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

// AdminAssetsPassValid reports whether pass is one AdminAssetsPass made for
// keyHash that hasn't ended at now, and doesn't end further ahead than a
// pass made now could. The MAC is compared in constant time.
func (k *Keys) AdminAssetsPassValid(pass, keyHash string, now time.Time) bool {
	expiryText, macText, ok := strings.Cut(pass, ".")
	if !ok || len(expiryText) > 12 {
		return false
	}
	// Only the form AdminAssetsPass writes: a positive number of seconds,
	// digits only, no sign or leading zero.
	expiry, err := strconv.ParseInt(expiryText, 10, 64)
	if err != nil || expiry <= 0 || strconv.FormatInt(expiry, 10) != expiryText {
		return false
	}
	given, err := base64.RawURLEncoding.Strict().DecodeString(macText)
	if err != nil || len(given) != sha256.Size {
		return false
	}
	signed := hmac.Equal(given, k.adminAssetsMAC(keyHash, expiry))
	ends := time.Unix(expiry, 0)
	return signed && ends.After(now) && !ends.After(now.Add(AdminAssetsPassLifetime+adminAssetsClockSkew))
}

// adminAssetsMAC is HMAC-SHA256 over the domain, the key's digest and the
// expiry as 8 big-endian bytes. The digest is a fixed 64 hex characters, so
// the parts can't run into each other.
func (k *Keys) adminAssetsMAC(keyHash string, expiry int64) []byte {
	mac := hmac.New(sha256.New, k.adminAssets)
	mac.Write([]byte(adminAssetsDomain))
	mac.Write([]byte(keyHash))
	mac.Write(binary.BigEndian.AppendUint64(nil, uint64(expiry)))
	return mac.Sum(nil)
}

// AdminAssetsCookieName is the pass's cookie. Its path isn't /, so it can't
// be __Host-; __Secure- in production still needs Secure.
func AdminAssetsCookieName(production bool) string {
	if production {
		return "__Secure-lewcontext_admin_assets"
	}
	return "lewcontext_admin_assets"
}

// AdminAssetsCookie carries a pass made now: HttpOnly, SameSite Strict, and
// sent only with requests under /admin-assets/.
func AdminAssetsCookie(pass string, production bool) *http.Cookie {
	return &http.Cookie{Name: AdminAssetsCookieName(production), Value: pass, Path: AdminAssetsPath, HttpOnly: true, Secure: production, SameSite: http.SameSiteStrictMode, MaxAge: int(AdminAssetsPassLifetime.Seconds()), Expires: time.Now().Add(AdminAssetsPassLifetime)}
}
