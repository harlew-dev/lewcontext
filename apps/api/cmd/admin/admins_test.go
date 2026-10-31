package main

import (
	"lewcontext/api/internal/config"
	"testing"
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
	t.Setenv("CLIENT_IP_HEADER", "")
	t.Setenv("TRUSTED_PROXIES", "")
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
