package main

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"time"
)

func TestNewRouteKeyPassesConfig(t *testing.T) {
	key, err := newRouteKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgresql://u:p@db/app?sslmode=verify-full")
	t.Setenv("APP_ORIGIN", "https://app.example.com")
	t.Setenv("APP_ENV", "production")
	t.Setenv("MAIL_DRIVER", "off")
	t.Setenv("EMAIL_VERIFICATION", "off")
	t.Setenv("EMAIL_VERIFICATION_EXISTING", "")
	t.Setenv("APP_KEY", "c3VwZXItc2VjcmV0LXRlc3Qta2V5LTMyLWJ5dGVzISE=")
	t.Setenv("RATE_LIMITS", "")
	t.Setenv("CAPTCHA_PROVIDER", "off")
	t.Setenv("PER_IP_LIMITS", "")
	t.Setenv("CLIENT_IP_HEADER", "")
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("ADMIN_ROUTE_KEY_PREVIOUS", "")
	t.Setenv("ADMIN_ALLOWED_IPS", "")
	t.Setenv("ADMIN_ROUTE_KEY", key)
	c, err := config.Load()
	if err != nil || c.AdminRouteKey != key || len(key) != 43 {
		t.Fatalf("%d-character key: %v", len(key), err)
	}
	if other, _ := newRouteKey(); other == key {
		t.Fatal("two keys alike")
	}
}

func TestGrantable(t *testing.T) {
	if grantable("seeded@example.test", true) == nil {
		t.Fatal("a fake account granted in production")
	}
	for _, c := range []struct {
		address    string
		production bool
	}{{"seeded@example.test", false}, {"ada@example.com", true}, {"ada@example.test.com", true}} {
		if err := grantable(c.address, c.production); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestGrantFlags(t *testing.T) {
	address, role, confirm, err := grantFlags([]string{"-email", " Ada@Example.com ", "-role", "operator", "-confirm"})
	if err != nil || address != "ada@example.com" || role != auth.AdminOperator || !confirm {
		t.Fatalf("%q %q %v %v", address, role, confirm, err)
	}
	// No -role: "" (a new admin is a viewer, an existing one keeps theirs).
	if _, role, confirm, err := grantFlags([]string{"-email", "ada@example.com"}); err != nil || role != "" || confirm {
		t.Fatalf("no role: %q %v %v", role, confirm, err)
	}
	for _, args := range [][]string{{"-email", "ada@example.com", "-role", "admin"}, {"-email", "ada@example.com", "-role", "Operator"}, {"-role", "viewer"}, {"-email", "a@b.c", "extra"}} {
		if _, _, _, err := grantFlags(args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if grantedRole("", "") != auth.AdminViewer || grantedRole("", auth.AdminOperator) != auth.AdminOperator || grantedRole(auth.AdminViewer, auth.AdminOperator) != auth.AdminViewer {
		t.Fatal("grantedRole")
	}
}

// A role change opens no setup window, and says so.
func TestRoleChangeMFA(t *testing.T) {
	now := time.Now()
	later, earlier := now.Add(time.Hour), now.Add(-time.Hour)
	cases := map[string]struct {
		admin store.Admin
		want  string
	}{
		"MFA on":         {store.Admin{User: store.User{MFAEnabledAt: &now}}, "is on"},
		"a window open":  {store.Admin{MFASetupUntil: &later}, "a role change opens none"},
		"window closed":  {store.Admin{MFASetupUntil: &earlier}, "run grant again without -role"},
		"never a window": {store.Admin{}, "can't sign in"},
	}
	for name, c := range cases {
		if got := roleChangeMFA(c.admin); !strings.Contains(got, c.want) || (c.admin.User.MFAEnabledAt == nil && strings.Contains(got, "24 hours")) {
			t.Errorf("%s: %q", name, got)
		}
	}
	if !strings.Contains(grantMFA(store.User{}), "opens a 24-hour window") || grantMFA(store.User{MFAEnabledAt: &now}) != "Two-factor authentication is on." {
		t.Fatal("grantMFA")
	}
}
