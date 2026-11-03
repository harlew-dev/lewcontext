package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
)

// ApiKeyKind distinguishes workspace services from a person's automation.
type ApiKeyKind string

const (
	ApiKeyWorkspace ApiKeyKind = "workspace"
	ApiKeyPersonal  ApiKeyKind = "personal"
)

// ApiKeyScopes is a closed catalogue; new API resources are never implicit grants.
var ApiKeyScopes = []string{"project.index", "project.show", "project.store", "project.update", "project.destroy"}

var apiKeyPattern = regexp.MustCompile(`^lc_(wk|pk)_(apk_[A-Za-z0-9]{14})_([A-Za-z0-9_-]{43})$`)

// ParseApiKey accepts the exact canonical wire format, including base64 pad bits.
func ParseApiKey(value string) (ApiKeyKind, string, bool) {
	if len(value) != 68 {
		return "", "", false
	}
	m := apiKeyPattern.FindStringSubmatch(value)
	if m == nil {
		return "", "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(m[3])
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != m[3] {
		return "", "", false
	}
	kind := ApiKeyWorkspace
	if m[1] == "pk" {
		kind = ApiKeyPersonal
	}
	return kind, m[2], true
}

// NewApiKey returns a creation-only credential and the digest stored in PostgreSQL.
func NewApiKey(kind ApiKeyKind, id string) (string, string, error) {
	prefix := "lc_wk_"
	if kind == ApiKeyPersonal {
		prefix = "lc_pk_"
	} else if kind != ApiKeyWorkspace {
		return "", "", errors.New("invalid credential kind")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	value := prefix + id + "_" + base64.RawURLEncoding.EncodeToString(b)
	if _, _, ok := ParseApiKey(value); !ok {
		return "", "", errors.New("invalid credential identifier")
	}
	return value, ApiKeyDigest(value), nil
}

// ApiKeyDigest hashes the complete canonical credential, never just its secret.
func ApiKeyDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// CheckApiKey performs a fixed-length comparison even for an unknown identifier.
func CheckApiKey(value, stored string) bool {
	want, err := hex.DecodeString(stored)
	valid := err == nil && len(want) == sha256.Size
	if !valid {
		want = make([]byte, sha256.Size)
	}
	got := sha256.Sum256([]byte(value))
	return subtle.ConstantTimeCompare(got[:], want) == 1 && valid
}

// ApiKeyRoleAllows intersects a saved scope with the current member's authority.
func ApiKeyRoleAllows(role Role, scope string) bool {
	if role.Rank() == 0 || !slices.Contains(ApiKeyScopes, scope) {
		return false
	}
	if scope == "project.index" || scope == "project.show" {
		return true
	}
	return role.Can(Permission(scope))
}
