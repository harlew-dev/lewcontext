package dbtest

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// envFile is the committed test settings file at the repository root.
const envFile = ".env.testing"

var (
	envKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	// A test database: lewcontext_test for the Go tests, or an e2e run's
	// lewcontext_test_<port>.
	testDatabaseName = regexp.MustCompile(`_test(_[0-9]+)?$`)
	localHosts       = map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true, "postgres": true}
)

// testEnv reads .env.testing from the repository root, found by walking up
// from the working directory. Tests use it instead of the process
// environment, so a development DATABASE_URL is never used by mistake.
func testEnv(t *testing.T) map[string]string {
	t.Helper()
	path, err := findEnvFile()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	env, err := parseEnv(f.Name(), bufio.NewScanner(f))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func findEnvFile() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		path := filepath.Join(dir, envFile)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s in this directory or any above it", envFile)
		}
		dir = parent
	}
}

// parseEnv reads plain KEY=value lines, # comments and blank lines: the form
// Go, Node's util.parseEnv, bash's source and make all read the same way.
// Anything else is refused rather than guessed at.
func parseEnv(name string, lines *bufio.Scanner) (map[string]string, error) {
	env := map[string]string{}
	for n := 1; lines.Scan(); n++ {
		line := lines.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !envKey.MatchString(key) {
			return nil, fmt.Errorf("%s:%d: want KEY=value with an upper-case KEY", name, n)
		}
		if strings.ContainsAny(value, " \t'\"`$\\") {
			return nil, fmt.Errorf("%s:%d: %s: no spaces, quotes, $ or backslashes in values", name, n, key)
		}
		if _, seen := env[key]; seen {
			return nil, fmt.Errorf("%s:%d: %s is set twice", name, n, key)
		}
		env[key] = value
	}
	return env, lines.Err()
}

// localTestDatabase refuses a URL whose host isn't local or whose database
// isn't a test database (*_test or *_test_<port>), so an edited .env.testing
// can't aim the tests' schema drops at a real database.
func localTestDatabase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return errors.New("DATABASE_URL in .env.testing must be a postgresql:// URL")
	}
	if !localHosts[u.Hostname()] {
		return fmt.Errorf("DATABASE_URL in .env.testing must be on this machine (127.0.0.1, ::1, localhost or postgres), not %q", u.Hostname())
	}
	if name := strings.TrimPrefix(u.Path, "/"); !testDatabaseName.MatchString(name) {
		return fmt.Errorf("DATABASE_URL in .env.testing must name a test database ending in _test, not %q", name)
	}
	return nil
}
