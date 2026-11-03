package auth

import (
	"strings"
	"testing"
)

func TestApiKeyCredentials(t *testing.T) {
	for _, kind := range []ApiKeyKind{ApiKeyWorkspace, ApiKeyPersonal} {
		value, digest, err := NewApiKey(kind, "apk_0123456789AbCd")
		if err != nil {
			t.Fatal(err)
		}
		parsed, id, ok := ParseApiKey(value)
		if !ok || parsed != kind || id != "apk_0123456789AbCd" || !CheckApiKey(value, digest) {
			t.Fatal("credential did not round trip")
		}
		other, _, err := NewApiKey(kind, id)
		if err != nil || other == value || CheckApiKey(other, digest) || CheckApiKey(value, "") {
			t.Fatal("credential independence or comparison failed")
		}
		for _, bad := range []string{"", value + "x", strings.Replace(value, "apk_", "usr_", 1), "Bearer " + value, strings.TrimSuffix(value, value[len(value)-1:]) + "!"} {
			if _, _, ok := ParseApiKey(bad); ok {
				t.Fatal("accepted malformed credential")
			}
		}
	}
}

func TestApiKeyScopesRespectLiveRoles(t *testing.T) {
	for _, role := range append([]Role{"unknown"}, Roles...) {
		for _, scope := range append([]string{"project.future", "*"}, ApiKeyScopes...) {
			want := scope != "*" && role.Rank() > 0 && (scope == "project.index" || scope == "project.show" || role.Can(Permission(scope)))
			if ApiKeyRoleAllows(role, scope) != want {
				t.Fatalf("role %s scope %s", role, scope)
			}
		}
	}
}
