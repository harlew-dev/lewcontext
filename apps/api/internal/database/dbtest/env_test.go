package dbtest

import (
	"bufio"
	"regexp"
	"strings"
	"testing"
)

func TestParseEnv(t *testing.T) {
	env, err := parseEnv("good", bufio.NewScanner(strings.NewReader("# comment\n\nA=1\nB_2=postgresql://u:p@h:5432/d?x=y\nEMPTY=\n")))
	if err != nil {
		t.Fatal(err)
	}
	if env["A"] != "1" || env["B_2"] != "postgresql://u:p@h:5432/d?x=y" || env["EMPTY"] != "" || len(env) != 3 {
		t.Fatalf("parsed %v", env)
	}
	for _, bad := range []string{
		"export A=1",
		"A = 1",
		"A=two words",
		`A="quoted"`,
		"A='quoted'",
		"A=$HOME",
		`A=back\slash`,
		"lower=1",
		"NOEQUALS",
		"A=1\nA=2",
	} {
		if _, err := parseEnv("bad", bufio.NewScanner(strings.NewReader(bad))); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestLocalTestDatabase(t *testing.T) {
	for _, good := range []string{
		"postgresql://lewcontext:password@127.0.0.1:5432/lewcontext_test?sslmode=disable",
		"postgresql://u:p@localhost/lewcontext_test_3101",
		"postgres://u:p@[::1]:5432/app_test",
		"postgresql://u:p@postgres:5432/lewcontext_test",
	} {
		if err := localTestDatabase(good); err != nil {
			t.Errorf("refused %q: %v", good, err)
		}
	}
	for _, bad := range []string{
		"postgresql://u:p@db.example.com:5432/lewcontext_test",
		"postgresql://u:p@127.0.0.1:5432/lewcontext",
		"postgresql://u:p@127.0.0.1:5432/lewcontext_test_abc",
		"postgresql://u:p@127.0.0.1:5432/test_lewcontext",
		"mysql://u:p@127.0.0.1/lewcontext_test",
		"",
	} {
		if err := localTestDatabase(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// The committed .env.testing must parse, point at a local test database, and
// hold no secret: anything named like one stays empty.
func TestEnvTestingHoldsNoSecrets(t *testing.T) {
	env := testEnv(t)
	if err := localTestDatabase(env["DATABASE_URL"]); err != nil {
		t.Fatal(err)
	}
	secret := regexp.MustCompile(`PASSWORD|SECRET|TOKEN|(^|_)KEY$`)
	for key, value := range env {
		if secret.MatchString(key) && value != "" {
			t.Errorf("%s has a value; .env.testing is committed, so it must hold no secret", key)
		}
	}
}
