package observe

import (
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"runtime/debug"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// Values an error's text can carry, none of which may survive Scrub.
const (
	email      = "ada.lovelace+work@example.com"
	digest     = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	token      = "Zx9_aB3-qL0vT7mK2pR5sW8yE1nH4jD6fG9cV0bX3zA" // a session token's shape: base64url of 32 bytes
	requestID  = "req_7Hq2mZ1xT4bLwN"
	userID     = "usr_2yEHYLwiwPVhLW"
	workspace  = "wsp_8kQ2mZ1xT4bLwN"
	uuid       = "3f2b8c1e-9d4a-4e6b-8f0c-1a2b3c4d5e6f"
	recovery   = "4kq2z-8m3tw"
	ipv4       = "203.0.113.42"
	ipv6       = "2001:db8:85a3::8a2e:370:7334"
	typedValue = "Ada's secret plan"
)

var secrets = []string{
	email, "ada.lovelace", "example.com", digest, token, requestID, userID, workspace, uuid, recovery,
	ipv4, ipv6, "8a2e", typedValue, "secret plan", "5432", "1234567",
}

// Every value that could be personal data or a secret becomes a
// placeholder, and what says what broke (constraint, table and column
// names, SQLSTATE codes, the words of the message) stays.
func TestScrub(t *testing.T) {
	for _, c := range []struct {
		name, in, want string
	}{
		{"email", "mail to " + email + " refused", "mail to <email> refused"},
		{"email in brackets", "rcpt <" + email + ">: no", "rcpt <email>: no"},
		{"SMTP 550", "gomail: could not send email 1: 550 5.1.1 <" + email + ">: Recipient address rejected: User unknown in virtual mailbox table",
			"gomail: could not send email <n>: <n> <n> <email>: Recipient address rejected: User unknown in virtual mailbox table"},
		{"IPv4 with a port", "dial tcp " + ipv4 + ":5432: connect: connection refused", "dial tcp <ip>: connect: connection refused"},
		{"IPv6", "from " + ipv6 + " and [" + ipv6 + "]:443 and ::1", "from <ip> and <ip> and <ip>"},
		{"public IDs", "user " + userID + " in " + workspace + " not found", "user <id> in <id> not found"},
		{"request ID", "while serving " + requestID, "while serving <id>"},
		{"UUID", "no row " + uuid, "no row <id>"},
		{"64-hex digest", "session " + digest + " gone", "session <hex> gone"},
		{"base64url token", "token " + token + " expired", "token <token> expired"},
		{"recovery code", "code " + recovery + " used", "code <token> used"},
		{"double quotes", `parsing "` + typedValue + `": invalid`, `parsing "…": invalid`},
		{"Go's %q", fmt.Sprintf("bad value %q here", `a "quoted" `+typedValue), `bad value "…" here`},
		{"single quotes", "can't parse '" + typedValue + "' at all", "can't parse '…' at all"},
		{"backquotes", "column `" + typedValue + "`", "column `…`"},
		{"cut-off quote", `name "` + typedValue, `name "…"`},
		{"PostgreSQL invalid input", `ERROR: invalid input syntax for type uuid: "` + typedValue + `" (SQLSTATE 22P02)`,
			`ERROR: invalid input syntax for type uuid: "…" (SQLSTATE 22P02)`},
		{"PostgreSQL integer", `ERROR: invalid input syntax for type integer: "1234567" (SQLSTATE 22P02)`,
			`ERROR: invalid input syntax for type integer: "…" (SQLSTATE 22P02)`},
		{"unique constraint", `ERROR: duplicate key value violates unique constraint "users_email_key" (SQLSTATE 23505)`,
			`ERROR: duplicate key value violates unique constraint "users_email_key" (SQLSTATE 23505)`},
		{"not-null column", `ERROR: null value in column "name" of relation "projects" violates not-null constraint (SQLSTATE 23502)`,
			`ERROR: null value in column "name" of relation "projects" violates not-null constraint (SQLSTATE 23502)`},
		{"key detail", `Key (email)=(` + email + `) already exists; Key (name)=(` + typedValue + `)`,
			`Key (email)=(…)`}, // to the last parenthesis: a value can hold its own
		{"a quoted value that looks like a name", `relation "` + typedValue + `"`, `relation "…"`},
		{"scan error", `sql: Scan error on column index 0, name "email": converting driver.Value type string ("` + email + `") to a int64: invalid syntax`,
			`sql: Scan error on column index <n>, name "…": converting driver.Value type string ("…") to a int64: invalid syntax`},
		{"numbers", "value too long for type character varying(80), got 1234567 and 3.25", "value too long for type character varying(<n>), got <n> and <n>"},
		{"words with digits stay", "utf8 and int64 and sha256", "utf8 and int64 and sha256"},
		{"identifiers stay", "projects_workspace_id_fkey and TestNothingSecretInTheLogs", "projects_workspace_id_fkey and TestNothingSecretInTheLogs"},
		{"one line", "first\nsecond\r\n\tthird\x00", "first second third"},
		{"context canceled", "context canceled", "context canceled"},
		{"URL", "Failed to load https://app.example.com/auth/forgot-password/" + token + "?next=%2Fapp for user", "Failed to load <url> for user"},
		{"postgres URL", "parse postgresql://nobody:hunter2@db:5432/x?sslmode=disable: bad", "parse <url> bad"},
		{"V8's JSON.parse", `SyntaxError: Unexpected token 'o', "{"name":"Ada Lovelace","email":"` + email + `"}" is not valid JSON`,
			`SyntaxError: Unexpected token '…', "…" is not valid JSON`},
		{"V8's JSON.parse, cut", `SyntaxError: Unexpected token 'A', ..."{"project":"` + typedValue + `"}"... is not valid JSON`,
			`SyntaxError: Unexpected token '…', "…" is not valid JSON`},
		{"nested quotes", `bad body "{"secret":"` + typedValue + `"}" here`, `bad body "…" here`},
		{"curly quotes", "project “" + typedValue + "” and ‘" + typedValue + "’ and «" + typedValue + "»", "project “…” and ‘…’ and «…»"},
		{"quoted local part", `mail "ada lovelace"@example.com refused`, "mail <email> refused"},
		{"URL-encoded email", "see /users/ada.lovelace%40example.com/profile", "see <email>"},
		{"full-width at", "ada.lovelace＠example.com refused", "<email> refused"},
		{"IPv6 then a colon", "from " + ipv6 + ": connection refused", "from <ip>: connection refused"},
		{"IPv6 then a full stop", "from " + ipv6 + ".", "from <ip>."},
		{"zero-width split", "token " + token[:20] + "\u200b" + token[20:], "token <token>"},
		{"bidi controls", "bidi \u202eplan\u202c done", "bidi plan done"},
		{"other digits", "digits １２３４５６７ and ٣٤٥٦", "digits <n> and <n>"},
		{"failing row", "ERROR: new row violates check constraint \"users_email_check\" DETAIL: Failing row contains (1234567, " + email + ", Ada (the countess) Lovelace, null).",
			"ERROR: new row violates check constraint \"users_email_check\" DETAIL: Failing row contains (…)."},
		{"key with nested parentheses", "Key (lower(name))=(Ada (the countess) Lovelace) already exists.", "Key (lower(name))=(…) already exists."},
		{"hex after a word", "digest_" + digest + " and tok" + digest[:40], "<token> and <token>"},
		{"base32 secret", "totp secret JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP invalid, short JBSWY3DPEHPK3PXP", "totp secret <token> invalid, short <token>"},
		{"recovery codes", "codes abcde-fghjk, ABCDE-12345 and abcde12345", "codes <token>, <token> and <token>"},
		{"ten-letter words stay", "background strengthen everywhere", "background strengthen everywhere"},
		{"fragment", "#access_token=" + token, "#<query>"},
		{"query string", "GET /api/workspaces?q=" + typedValue[:3] + "&page=2 failed", "GET /api/workspaces?<query> failed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Scrub(c.in)
			if got != c.want {
				t.Errorf("Scrub(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
			if Scrub(got) != got {
				t.Errorf("not stable: Scrub(%q) = %q", got, Scrub(got))
			}
			for _, secret := range secrets {
				if strings.Contains(got, secret) {
					t.Errorf("%q kept %q", got, secret)
				}
			}
		})
	}
}

// The result is one line of valid UTF-8, at most MaxMessage characters.
func TestScrubBounds(t *testing.T) {
	long := Scrub(strings.Repeat("é", 3000))
	if n := utf8.RuneCountInString(long); n != MaxMessage || !strings.HasSuffix(long, "…") {
		t.Errorf("a long message: %d characters", n)
	}
	if got := Scrub("bad \xff\xfe byte"); !utf8.ValidString(got) {
		t.Errorf("invalid UTF-8 kept: %q", got)
	}
	if ScrubError(nil) != "" {
		t.Error("ScrubError(nil) isn't empty")
	}
	if ErrorAttr(errors.New("to "+email)).Value.String() != "to <email>" {
		t.Error("ErrorAttr doesn't scrub")
	}
}

// Failures that differ only in what Scrub replaces share a fingerprint;
// another route, source or message doesn't.
func TestFingerprints(t *testing.T) {
	route := "GET /api/workspaces/{workspace}/projects"
	pairs := [][2]string{
		{"user " + userID + " has no workspace " + workspace, "user usr_0000000000000A has no workspace wsp_ZZZZZZZZZZZZZZ"},
		{"mail to " + email + " refused", "mail to bob@example.org refused"},
		{"from " + ipv4, "from 198.51.100.7"},
		{"from " + ipv6, "from ::1"},
		{"session " + digest, "session " + strings.Repeat("0", 64)},
		{"token " + token, "token aB3dE5fG7hJ9kL1mN3pQ5rS7tU9vW1xY3zA5bC7dE9f"},
		{"request " + requestID, "request req_0123456789abcd"},
		{`parsing "` + typedValue + `"`, `parsing "x"`},
		{`ERROR: invalid input syntax for type uuid: "abc" (SQLSTATE 22P02)`, `ERROR: invalid input syntax for type uuid: "` + uuid + `x" (SQLSTATE 22P02)`},
		{"550 5.1.1 <" + email + ">: Recipient address rejected", "550 5.1.1 <bob@example.org>: Recipient address rejected"},
		{"row " + uuid, "row 00000000-0000-4000-8000-000000000000"},
		{"took 1234 ms", "took 7 ms"},
	}
	for _, p := range pairs {
		a, b := Fingerprint(SourceRequest, route, Scrub(p[0])), Fingerprint(SourceRequest, route, Scrub(p[1]))
		if a != b {
			t.Errorf("different fingerprints:\n %q → %q\n %q → %q", p[0], Scrub(p[0]), p[1], Scrub(p[1]))
		}
	}
	base := Fingerprint(SourceRequest, route, "context deadline exceeded")
	for _, other := range []string{
		Fingerprint(SourcePanic, route, "context deadline exceeded"),
		Fingerprint(SourceRequest, "GET /api/workspaces", "context deadline exceeded"),
		Fingerprint(SourceRequest, route, "connection refused"),
	} {
		if other == base {
			t.Error("a different source, route or message shares a fingerprint")
		}
	}
	if len(base) != 64 {
		t.Errorf("fingerprint %q isn't hex SHA-256", base)
	}
}

// A Go stack keeps function names and file:line, never argument values,
// directories or the goroutine header.
func TestScrubGoStack(t *testing.T) {
	stack := string(debug.Stack())
	got := ScrubStack(stack)
	if !strings.Contains(got, "runtime/debug.Stack(…)") || !strings.Contains(got, "\tstack.go:") || !strings.Contains(got, "TestScrubGoStack") {
		t.Errorf("function names or lines missing:\n%s", got)
	}
	for _, gone := range []string{"goroutine ", "+0x", "0xc0", "/src/runtime/"} {
		if strings.Contains(got, gone) {
			t.Errorf("kept %q:\n%s", gone, got)
		}
	}
	crafted := "goroutine 9 [running]:\nmain.handle({0xc000123456, 0x2a}, {0x" + digest + "})\n\t/home/ada/app/main.go:42 +0x1d\ncreated by net/http.(*Server).Serve in goroutine 1\n\t/usr/local/go/src/net/http/server.go:3360 +0x485"
	want := "main.handle(…)\n\tmain.go:42\ncreated by net/http.(*Server).Serve\n\tserver.go:3360"
	if got := ScrubStack(crafted); got != want {
		t.Errorf("ScrubStack:\n got %q\nwant %q", got, want)
	}
}

// A browser's stack keeps each frame's function and script file with its
// line and column; never the script's address, a query, or the page's own
// path (an inline script's frame), which can hold a link's token.
func TestScrubBrowserStack(t *testing.T) {
	chrome := "TypeError: Cannot read properties of undefined (reading '" + typedValue + "')\n" +
		"    at ProjectRow (https://app.example.com/assets/projects-B4xQ9.js:12:345)\n" +
		"    at https://app.example.com/assets/root-C1d2E.js?v=" + token + ":1:99\n" +
		"    at inline (https://app.example.com/auth/forgot-password/" + token + ":3:7)\n" +
		"    at async Promise.all (index 0)"
	want := "TypeError: Cannot read properties of undefined (reading '…')\n" +
		"at ProjectRow (projects-B4xQ9.js:12:345)\n" +
		"at root-C1d2E.js:1:99\n" +
		"at inline (<page>:3:7)\n" +
		"at async Promise.all (index <n>)"
	if got := ScrubStack(chrome); got != want {
		t.Errorf("Chrome:\n got %q\nwant %q", got, want)
	}
	firefox := "ProjectRow@https://app.example.com/assets/projects-B4xQ9.js:12:345\n@https://app.example.com/auth/invitations/" + token + ":1:2\n"
	want = "at ProjectRow (projects-B4xQ9.js:12:345)\nat <page>:1:2"
	if got := ScrubStack(firefox); got != want {
		t.Errorf("Firefox:\n got %q\nwant %q", got, want)
	}
	long := ScrubStack(strings.Repeat("    at f (https://h/a.js:1:2)\n", 2000))
	if n := utf8.RuneCountInString(long); n > MaxStack {
		t.Errorf("a long stack: %d characters", n)
	}
	for _, secret := range append(secrets, "app.example.com", "forgot-password", "invitations") {
		if strings.Contains(ScrubStack(chrome+"\n"+firefox), secret) {
			t.Errorf("a browser stack kept %q", secret)
		}
	}
}

// A random token that holds an ID-shaped piece (vbv_ and 14 base62 characters)
// was cut up by the ID rule before the token rules saw it, leaving 25 of its
// 43 characters in the output.
func TestScrubTokenWithIDShapedPiece(t *testing.T) {
	const secret = "Uc3gCkIXBG4kWB-vbv_R7RP4mCmrLclu3-VcGmW_zaA"
	if got, want := Scrub("reset "+secret+" expired"), "reset <token> expired"; got != want {
		t.Fatalf("Scrub = %q, want %q", got, want)
	}
	// A real ID next to it is still an ID.
	if got, want := Scrub("usr_2yEHYLwiwPVhLW "+secret), "<id> <token>"; got != want {
		t.Fatalf("Scrub = %q, want %q", got, want)
	}
}

// longestSharedRun is the length of the longest run of consecutive
// characters of secret that out also holds.
func longestSharedRun(secret, out string) int {
	best := 0
	for i := 0; i < len(secret); i++ {
		for n := best + 1; i+n <= len(secret) && strings.Contains(out, secret[i:i+n]); n++ {
			best = n
		}
	}
	return best
}

// Secrets in the shapes the app makes, plain and with pieces shaped like
// the other rules' targets (an ID, hex, a number, an email, a UUID) planted
// inside, none of which may leave 8 or more consecutive characters. Runs
// with several fixed seeds, and a failure names its seed.
func TestScrubbedSecretShapes(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3, 20261101, 0xC0FFEE} {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := mrand.New(mrand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
			random := func(n int) []byte {
				b := make([]byte, n)
				for i := range b {
					b[i] = byte(rng.UintN(256))
				}
				return b
			}
			const alnum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
			pick := func(set string, n int) string {
				b := make([]byte, n)
				for i := range b {
					b[i] = set[rng.IntN(len(set))]
				}
				return string(b)
			}
			// Planted pieces, each as the other rules would match it.
			plants := []func() string{
				func() string { return pick("abcdefghijklmnopqrstuvwxyz", 3) + "_" + pick(alnum, 14) },
				func() string {
					return []string{"usr", "wsp", "prj", "inv", "req", "evt", "err", "srv", "ipb", "alt"}[rng.IntN(10)] + "_" + pick(alnum, 14)
				},
				func() string { return pick("0123456789abcdef", 16+rng.IntN(20)) },
				func() string { return pick("0123456789", 1+rng.IntN(12)) },
				func() string { return pick("abcdefgh", 5) + "@" + pick("abcdefgh", 5) + ".com" },
				func() string {
					return pick("0123456789abcdef", 8) + "-" + pick("0123456789abcdef", 4) + "-" + pick("0123456789abcdef", 4) + "-" + pick("0123456789abcdef", 4) + "-" + pick("0123456789abcdef", 12)
				},
			}
			// planted is a base64url token of n characters with a plant
			// swapped in at a random place.
			planted := func(n int) string {
				tok := base64.RawURLEncoding.EncodeToString(random(n))[:n]
				plant := plants[rng.IntN(len(plants))]()
				at := rng.IntN(n)
				return tok[:at] + plant + tok[at:]
			}
			check := func(kind string, n int, make func() string) {
				t.Helper()
				for range n {
					secret := make()
					out := Scrub("reset " + secret + " expired")
					if run := longestSharedRun(secret, out); run >= 8 {
						t.Fatalf("seed %d: a %s left %d consecutive characters: %q → %q", seed, kind, run, secret, out)
					}
				}
			}
			check("base64url token", 500, func() string { return base64.RawURLEncoding.EncodeToString(random(32)) })
			check("base64 key", 500, func() string { return base64.StdEncoding.EncodeToString(random(32)) })
			check("base32 secret", 500, func() string { return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random(20)) })
			check("base64url token with a planted piece", 500, func() string { return planted(43) })
			check("long base64url token with a planted piece", 300, func() string { return planted(64) })
			check("short token (20-31) with a planted piece", 300, func() string { return planted(20 + rng.IntN(8)) })
			alphabet := "0123456789abcdefghjkmnpqrstvwxyz"
			code := func(dash bool) func() string {
				return func() string {
					var b strings.Builder
					for i, x := range random(10) {
						if i == 5 && dash {
							b.WriteByte('-')
						}
						b.WriteByte(alphabet[x%32])
					}
					return b.String()
				}
			}
			check("recovery code", 500, code(true))
			// Typed without its dash, a code is caught when it holds a digit
			// and a letter: all but about 2.4% (all letters) and 1 in 10^5
			// (all digits, then a number anyway). Ten letters in a row are a
			// word as often as a code, so those stay.
			check("recovery code without its dash", 500, func() string {
				for {
					c := code(false)()
					if strings.ContainsAny(c, "0123456789") && strings.ContainsAny(c, "abcdefghjkmnpqrstvwxyz") {
						return c
					}
				}
			})
		})
	}
}

// A browser's text also loses bare hosts and paths someone could leave for
// an admin to follow; the server's keeps its table and constraint names.
func TestScrubClient(t *testing.T) {
	for in, want := range map[string]string{
		"Admin: please visit evil[.]com or evil.example.com/login to fix this": "Admin: please visit <url> or <url> to fix this",
		"email support at evil dot com":                                        "email support at <url>",
		"bidi ‮evil.com‬ please":                                               "bidi <url> please",
		"TypeError: project.name is undefined; Promise.all failed":             "TypeError: project.name is undefined; Promise.all failed",
	} {
		if got := ScrubClient(in); got != want {
			t.Errorf("ScrubClient(%q) = %q, want %q", in, got, want)
		}
	}
	server := `duplicate key value violates unique constraint "users_email_key" on users.email`
	if Scrub(server) != server {
		t.Errorf("the server's text lost a name: %q", Scrub(server))
	}
}

// A stack's lines never carry a control character (a NUL once failed every
// flush), and a Go location is kept as it is only when it is exactly a path
// and a line; anything else in that place is scrubbed. A browser's stack
// drops Go-format lines altogether.
func TestScrubStackLinesAreSafe(t *testing.T) {
	planted := "\tAdmin: visit evil.example.com, mail " + email + " " + token + "\x00 x.go:1\n" +
		"\t/srv/app/x\x00.go:7 +0x1\n\t/srv/app/main.go:42 +0x1d\nmain.handler(0xc000123456)\n" +
		"goroutine 1 [running]:\ncreated by main.secret\n‮    at ProjectRow (https://h/assets/projects-B4xQ9.js:12:345)"
	server := ScrubStack(planted)
	for _, line := range strings.Split(server, "\n") {
		if strings.ContainsFunc(strings.TrimPrefix(line, "\t"), func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
			t.Errorf("a control character in %q", line)
		}
	}
	if !strings.Contains(server, "\tmain.go:42\n") || strings.Contains(server, "\tx") || strings.Contains(server, email) || strings.Contains(server, token) {
		t.Errorf("server stack:\n%s", server)
	}
	if client := ScrubClientStack(planted); client != "at ProjectRow (projects-B4xQ9.js:12:345)" {
		t.Errorf("client stack: %q", client)
	}
}

// Text an attacker controls (a message sent from a browser, an error that
// quotes a request) must not make the scrubber slow: every call on a large,
// hostile input finishes well under 100 ms (a second with the race detector).
func TestScrubIsFastOnAdversarialInput(t *testing.T) {
	const size = 100 << 10
	limit := 100 * time.Millisecond
	if raceEnabled {
		limit *= 10 // the race detector slows this code about tenfold
	}
	repeat := func(unit string) string { return strings.Repeat(unit, size/len(unit)+1)[:size] }
	inputs := map[string]string{
		"token runs":         repeat("Zx9_aB3-qL0vT7mK2pR5sW8yE1nH4jD6 "),
		"one long token":     repeat("aB3dE5gH7jK9mN1pQ3sT5vW7yZ"),
		"at signs":           repeat("@"),
		"emails":             repeat("a@b.c "),
		"local parts":        repeat("a.b+c@") + "x",
		"underscores":        repeat("_"),
		"id-like":            repeat("usr_2yEHYLwiwPVhLW_"),
		"digits":             repeat("1234567890"),
		"dotted digits":      repeat("1.2.3.4.5."),
		"colons":             repeat(":"),
		"hex and colons":     repeat("ab:cd:"),
		"double quotes":      repeat(`"`),
		"unpaired quotes":    repeat(`"a`),
		"single quotes":      repeat(`'a`),
		"curly quotes":       repeat("“a‘b«c„"),
		"backquotes":         repeat("`"),
		"open parentheses":   repeat("("),
		"nested parentheses": repeat("(a)") + strings.Repeat(")", 10),
		"key values":         repeat(")=(a"),
		"urls":               repeat("http://a/b?c=d#e=f "),
		"query strings":      repeat("?a=b"),
		"brackets":           repeat("[::"),
		"hosts":              repeat("a.b.c.d."),
		"json snippets":      repeat(`"{"a":1}" `),
		"digits in words":    repeat("a1"),
		"spaces":             repeat(" "),
		"many lines":         repeat("\tx.go:1\nf(a)\n"),
	}
	calls := map[string]func(string) string{
		"Scrub":            Scrub,
		"ScrubClient":      ScrubClient,
		"ScrubError":       func(s string) string { return ScrubError(errors.New(s)) },
		"ScrubStack":       ScrubStack,
		"ScrubClientStack": ScrubClientStack,
	}
	for name, input := range inputs {
		for call, fn := range calls {
			start := time.Now()
			fn(input)
			if took := time.Since(start); took > limit {
				t.Errorf("%s on %s (%d bytes) took %v", call, name, len(input), took)
			}
		}
	}
}
