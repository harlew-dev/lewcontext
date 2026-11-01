package observe

import (
	"log/slog"
	"net"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What an error's text may keep (docs/api/observability.md#scrubbing). An
// error's message is written by code, but it often quotes what it was
// given: PostgreSQL echoes a value it couldn't read, a scan error quotes
// one, an SMTP refusal names the recipient. Scrub replaces what could be a
// person's data, or a secret, with a placeholder before the text is logged
// or grouped, so two failures that differ only in whom they hit read the
// same and neither is stored.

// Placeholders.
const (
	scrubbedEmail  = "<email>"
	scrubbedIP     = "<ip>"
	scrubbedID     = "<id>"
	scrubbedNumber = "<n>"
	scrubbedHex    = "<hex>"
	scrubbedToken  = "<token>"
	scrubbedURL    = "<url>"
)

// Limits on what Scrub and ScrubStack return, in characters: the
// app_errors columns' CHECKs.
const (
	MaxMessage = 1000
	MaxStack   = 8000
)

var (
	// A URL (its path can hold a link's token or the admin key, its query
	// a search or an email), and a query string on its own.
	urlPattern   = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>` + "`" + `]+`)
	queryPattern = regexp.MustCompile(`[?#][^\s"'<>?#=]+=[^\s"'<>]*`)
	// V8's JSON.parse quotes the text it couldn't read, quotes and all
	// (Unexpected token 'o', "{"name":"Ada"}" is not valid JSON): everything
	// from the first quote to the one before the suffix goes, as nested
	// quotes can't be paired.
	jsonSnippetPattern = regexp.MustCompile(`(?:\.\.\.)?"[^\n]*"(?:\.\.\.)?(\s+is not valid JSON)`)
	// PostgreSQL's details: the values in Key (email)=(…) and Failing row
	// contains (…), up to the last parenthesis (values can hold their own).
	keyValuePattern   = regexp.MustCompile(`\)=\(.*\)`)
	failingRowPattern = regexp.MustCompile(`(?i)(failing row contains )\(.*\)`)
	// An email address, or anything shaped like one: the at sign (also
	// URL-encoded, %40, or full-width, ＠) and what's around it up to a space,
	// quote, bracket or comma.
	// A quoted local part ("ada lovelace"@example.com), once its quotes are
	// scrubbed.
	quotedEmailPattern = regexp.MustCompile(`"…"(?:@|＠|%40)[^\s@＠<>"'()\[\],;:]+`)
	emailPattern       = regexp.MustCompile(`<?[^\s@＠<>"'()\[\],;:]+(?:@|＠|%40)[^\s@＠<>"'()\[\],;:]+>?`)
	// A public ID (usr_, wsp_, prj_, inv_, evt_) or a request's (req_): three
	// letters, an underscore and 14 base62 characters.
	publicIDPattern = regexp.MustCompile(`\b[a-z]{3}_[0-9A-Za-z]{14}\b`)
	uuidPattern     = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	// An IPv4 address, with a port when it has one.
	ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b`)
	// What could be an IPv6 address (hex digits, colons, dots, maybe in
	// brackets with a port); net.ParseIP decides.
	ipv6Pattern = regexp.MustCompile(`\[?[0-9A-Fa-f]*:[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*(?:%[0-9A-Za-z]+)?\]?(?::\d{1,5})?`)
	// A double-quoted string (Go's %q escapes included, or cut off at the
	// end), a backquoted one, and a single-quoted one whose quote opens it
	// (not an apostrophe: "can't"), apostrophes inside it included ('Ada's');
	// curly and guillemet quotes alike.
	doubleQuotedPattern = regexp.MustCompile(`"(?:[^"\\]|\\.)*(?:"|\\?$)`)
	backQuotedPattern   = regexp.MustCompile("`[^`]*`")
	singleQuotedPattern = regexp.MustCompile(`(^|[^\p{L}\p{N}])'(?:[^'\n]|'[\p{L}\p{N}])*'`)
	curlyQuotedPattern  = regexp.MustCompile(`“[^”]*(?:”|$)|‘(?:[^’]|’[\p{L}\p{N}])*(?:’|$)|«[^»]*(?:»|$)|„[^“”]*(?:[“”]|$)`)
	// A run of base64 or base64url characters (scrubToken decides).
	tokenPattern = regexp.MustCompile(`[A-Za-z0-9+/_\-]{20,}={0,2}`)
	// A base32 secret (an authenticator's): 16 or more of A–Z and 2–7.
	base32Pattern = regexp.MustCompile(`\b[A-Z2-7]{16,}\b`)
	// 16 or more hex digits, even inside a word (digest_9f86…).
	hexPattern = regexp.MustCompile(`(?:0x)?[0-9A-Fa-f]{16,}`)
	// A recovery code (Crockford's base32, as auth.GenerateRecoveryCodes
	// makes them): xxxxx-xxxxx always; ten in a row only holding a digit and
	// a letter (scrubCode), so a ten-letter word stays.
	codePattern       = regexp.MustCompile(`(?i)\b[0-9abcdefghjkmnpqrstvwxyz]{5}-[0-9abcdefghjkmnpqrstvwxyz]{5}\b`)
	joinedCodePattern = regexp.MustCompile(`(?i)\b[0-9abcdefghjkmnpqrstvwxyz]{10}\b`)
	// A number: digits, maybe with a fraction or dotted parts (5.1.1), or 0x
	// and hex digits. SQLSTATE codes are kept (scrubNumber).
	numberPattern   = regexp.MustCompile(`(?i)(SQLSTATE [0-9A-Z]{5})|\b(?:0x[0-9a-f]+|\d+(?:[.,]\d+)*)\b`)
	spacesPattern   = regexp.MustCompile(`\s+`)
	goArgsPattern   = regexp.MustCompile(`\([^()]*\)$`)
	goOffsetPattern = regexp.MustCompile(`\s\+0x[0-9a-f]+$`)
	// Go's location line, once its offset is gone: a path ending in
	// file.go:line, nothing else.
	goLocationPattern = regexp.MustCompile(`^\S+\.go:\d+$`)
	// A browser's stack frame: "at name (url:line:col)", "at url:line:col"
	// (Chrome, Safari's newer), or "name@url:line:col" (Firefox, Safari).
	jsFramePattern = regexp.MustCompile(`^\s*(?:at\s+(?:(.*?)\s+\()?|([^@\s]*)@)(.*?):(\d+)(?::(\d+))?\)?\s*$`)
	// In a browser's text only: a host with a path, or a host under a
	// common top-level domain (dots written as [.] or (dot) too), which
	// someone could plant for an admin to visit. Never applied to the
	// server's errors, whose table and constraint names look alike.
	hostPathPattern = regexp.MustCompile(`\b[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+/[^\s"'<>]*`)
	hostPattern     = regexp.MustCompile(`(?i)\b[a-z0-9-]+(?:(?:\.|\[\.\]|\(\.\)|\[dot\]|\(dot\)|\s+dot\s+)[a-z0-9-]+)*(?:\.|\[\.\]|\(\.\)|\[dot\]|\(dot\)|\s+dot\s+)(?:com|net|org|io|dev|app|co|info|biz|xyz|me|ly|gg|ai|sh|site|online|top|link|click|page|ru|cn|de|uk|fr|us|eu|nl|tk|ml|ga|cf|gq|zip|mov)\b(?:/[^\s"'<>]*)?`)
)

// identifierKeywords are the words after which PostgreSQL quotes the name of
// something in the schema (constraint "users_email_key", relation
// "projects"): what broke, so it's kept. A quoted string anywhere else is a
// value, and goes.
var identifierKeywords = []string{
	"constraint", "relation", "table", "column", "index", "schema", "sequence",
	"function", "trigger", "type", "database", "role", "view", "extension", "operator",
}

// ScrubError is err's message as Scrub leaves it, "" for nil.
func ScrubError(err error) string {
	if err == nil {
		return ""
	}
	return Scrub(err.Error())
}

// ErrorAttr is how request and background code logs an error: its message
// scrubbed (Scrub), under "error".
func ErrorAttr(err error) slog.Attr {
	return slog.String("error", ScrubError(err))
}

// Scrub is message with anything that could be a person's data or a secret
// replaced by a placeholder (docs/api/observability.md#scrubbing): URLs
// (<url>) and query strings (?<query>), email addresses (<email>), IP
// addresses (<ip>), public and request IDs and UUIDs (<id>), quoted strings
// ("…", '…', “…”), PostgreSQL's row and key details, token-like runs
// (<token>), long hex (<hex>), base32 secrets and recovery codes (<token>),
// and numbers (<n>). Names PostgreSQL quotes after a word such as
// constraint or relation stay, as do SQLSTATE codes: they say what broke.
// The result is one line of valid UTF-8 without control or format
// characters, at most MaxMessage characters.
func Scrub(message string) string {
	s := oneLine(message)
	s = jsonSnippetPattern.ReplaceAllString(s, `"…"$1`)
	s = keyValuePattern.ReplaceAllString(s, ")=(…)")
	s = failingRowPattern.ReplaceAllString(s, "${1}(…)")
	s = urlPattern.ReplaceAllString(s, scrubbedURL)
	s = queryPattern.ReplaceAllStringFunc(s, func(q string) string { return q[:1] + "<query>" })
	s = emailPattern.ReplaceAllString(s, scrubbedEmail)
	s = publicIDPattern.ReplaceAllString(s, scrubbedID)
	s = uuidPattern.ReplaceAllString(s, scrubbedID)
	s = ipv4Pattern.ReplaceAllString(s, scrubbedIP)
	s = ipv6Pattern.ReplaceAllStringFunc(s, scrubIPv6)
	s = scrubDoubleQuoted(s)
	s = quotedEmailPattern.ReplaceAllString(s, scrubbedEmail)
	s = curlyQuotedPattern.ReplaceAllStringFunc(s, scrubCurly)
	s = backQuotedPattern.ReplaceAllString(s, "`…`")
	s = singleQuotedPattern.ReplaceAllString(s, "$1'…'")
	s = tokenPattern.ReplaceAllStringFunc(s, scrubToken)
	s = base32Pattern.ReplaceAllString(s, scrubbedToken)
	s = hexPattern.ReplaceAllString(s, scrubbedHex)
	s = codePattern.ReplaceAllString(s, scrubbedToken)
	s = joinedCodePattern.ReplaceAllStringFunc(s, scrubCode)
	s = numberPattern.ReplaceAllStringFunc(s, scrubNumber)
	s = scrubOtherDigits(s)
	s = strings.TrimSpace(spacesPattern.ReplaceAllString(s, " "))
	return truncateRunes(s, MaxMessage)
}

// ScrubClient is Scrub for text a browser sent (SourceClient): it also
// replaces bare host names and paths (evil.example.com/login, evil[.]com)
// with <url>, so nobody can leave an admin a link to follow. Only for a
// browser's text: the server's errors name tables and constraints that look
// alike.
func ScrubClient(message string) string {
	s := Scrub(message)
	s = hostPathPattern.ReplaceAllString(s, scrubbedURL)
	s = hostPattern.ReplaceAllString(s, scrubbedURL)
	return truncateRunes(s, MaxMessage)
}

// oneLine is s as valid UTF-8 with every control character (NUL, a newline)
// a space and every format character (bidi controls, zero-width spaces, a
// soft hyphen) gone: one could split a token so half of it looked like a
// word.
func oneLine(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || r == ' ' || r == ' ':
			return ' '
		}
		return r
	}, s)
}

// scrubIPv6 replaces a candidate that is an IPv6 address (with or without
// brackets and a port), trimming punctuation the sentence put after it
// (2001:db8::1: refused).
func scrubIPv6(candidate string) string {
	for trimmed := candidate; trimmed != ""; trimmed = trimmed[:len(trimmed)-1] {
		if isIP(trimmed) {
			return scrubbedIP + candidate[len(trimmed):]
		}
		if last := trimmed[len(trimmed)-1]; last != ':' && last != '.' {
			break
		}
	}
	return candidate
}

func isIP(candidate string) bool {
	host := candidate
	if h, _, err := net.SplitHostPort(candidate); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	return net.ParseIP(host) != nil
}

// scrubDoubleQuoted replaces each double-quoted string with "…", unless
// it's a schema name PostgreSQL quotes after one of identifierKeywords. A
// closing quote followed straight by more text ("{"name":…) means the
// quotes are nested and can't be paired: everything up to the line's last
// quote goes.
func scrubDoubleQuoted(s string) string {
	matches := doubleQuotedPattern.FindAllStringIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		if m[0] < last {
			continue // inside a widened replacement
		}
		b.WriteString(s[last:m[0]])
		end := m[1]
		if nested(s, end) {
			end = strings.LastIndexByte(s, '"') + 1
		}
		quoted := s[m[0]:end]
		if end == m[1] && keepIdentifier(s[:m[0]], quoted) {
			b.WriteString(quoted)
		} else {
			b.WriteString(`"…"`)
		}
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// nested reports whether the quote that closed at end is followed straight
// by a word, a bracket or another quote, as in JSON quoted in a message;
// there is a later quote to widen to.
func nested(s string, end int) bool {
	if end >= len(s) || strings.LastIndexByte(s, '"') < end {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[end:])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(`{["_@`, r)
}

// scrubCurly replaces a curly or guillemet-quoted string, keeping its
// quotes' style.
func scrubCurly(quoted string) string {
	switch {
	case strings.HasPrefix(quoted, "“"):
		return "“…”"
	case strings.HasPrefix(quoted, "‘"):
		return "‘…’"
	case strings.HasPrefix(quoted, "«"):
		return "«…»"
	}
	return "„…“"
}

// keepIdentifier reports whether quoted, which follows before, is a schema
// name: a word of letters, digits and underscores after one of
// identifierKeywords.
func keepIdentifier(before, quoted string) bool {
	name := strings.Trim(quoted, `"`)
	if len(name) == 0 || len(name) > 63 || len(quoted) != len(name)+2 {
		return false
	}
	for _, c := range name {
		if !(c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
			return false
		}
	}
	words := strings.Fields(before)
	if len(words) == 0 {
		return false
	}
	last := strings.ToLower(words[len(words)-1])
	for _, keyword := range identifierKeywords {
		if last == keyword {
			return true
		}
	}
	return false
}

// scrubToken decides on a run of base64 or base64url characters (20 or
// more). Kept: a path of lower-case segments (a Go package), a lower- or
// upper-case identifier (projects_workspace_id_fkey), and under 32
// characters a CamelCase name without digits (TestNothingSecretInTheLogs).
// All hex is <hex>. Anything else 32 or longer is a token (a session,
// reset or invitation token is 43 base64url characters, whatever classes
// they happen to hold), and so is a shorter run mixing two of upper case,
// lower case and digits.
func scrubToken(run string) string {
	body := strings.TrimRight(run, "=")
	var upper, lower, digit, slash, other int
	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case 'A' <= c && c <= 'Z':
			upper++
		case 'a' <= c && c <= 'z':
			lower++
		case '0' <= c && c <= '9':
			digit++
		case c == '/':
			slash++
		default:
			other++
		}
	}
	switch {
	case len(body) >= 16 && allHex(body):
		return scrubbedHex
	case slash > 0 && upper == 0:
		return run // a path
	case digit == 0 && slash == 0 && (upper == 0 || lower == 0):
		return run // snake_case, kebab-case or UPPER_CASE
	case len(body) >= 32:
		return scrubbedToken
	case digit == 0 && slash == 0 && other == 0 && camelCase(body):
		return run
	}
	classes := 0
	for _, n := range []int{upper, lower, digit} {
		if n > 0 {
			classes++
		}
	}
	if classes >= 2 {
		return scrubbedToken
	}
	return run
}

// camelCase reports whether letters read as CamelCase words: lower-case
// runs of 2.5 letters or more on average, upper-case ones of at most 1.5
// (random letters average 2 each).
func camelCase(letters string) bool {
	var upperRuns, lowerRuns, upperLen, lowerLen int
	for i := 0; i < len(letters); i++ {
		isUpper := 'A' <= letters[i] && letters[i] <= 'Z'
		starts := i == 0 || isUpper != ('A' <= letters[i-1] && letters[i-1] <= 'Z')
		if isUpper {
			upperLen++
			if starts {
				upperRuns++
			}
		} else {
			lowerLen++
			if starts {
				lowerRuns++
			}
		}
	}
	if lowerRuns == 0 || upperRuns == 0 {
		return true
	}
	return float64(lowerLen)/float64(lowerRuns) >= 2.5 && float64(upperLen)/float64(upperRuns) <= 1.5
}

func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// scrubCode replaces ten recovery-code characters in a row holding both a
// digit and a letter: a recovery code typed without its dash.
func scrubCode(match string) string {
	if strings.ContainsAny(match, "0123456789") && strings.IndexFunc(match, unicode.IsLetter) >= 0 {
		return scrubbedToken
	}
	return match
}

// scrubOtherDigits replaces each run of digits outside ASCII (full-width
// １２３, Arabic-Indic ٣٤٥), which numberPattern's \d doesn't see.
func scrubOtherDigits(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r > unicode.MaxASCII && unicode.IsDigit(r) }) {
		return s
	}
	var b strings.Builder
	inRun := false
	for _, r := range s {
		if r > unicode.MaxASCII && unicode.IsDigit(r) {
			if !inRun {
				b.WriteString(scrubbedNumber)
			}
			inRun = true
			continue
		}
		inRun = false
		b.WriteRune(r)
	}
	return b.String()
}

// scrubNumber replaces a number, keeping a SQLSTATE code.
func scrubNumber(match string) string {
	if strings.HasPrefix(strings.ToUpper(match), "SQLSTATE") {
		return match
	}
	return scrubbedNumber
}

// truncateRunes cuts s to at most n characters, ending it with "…" when
// it was longer.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// ScrubStack is a stack as it may be logged and stored: function names and
// where they are (file and line), never the arguments' values or a file's
// directory, at most MaxStack characters. It reads Go's (debug.Stack) and
// browsers' (Error.stack) formats; any other line is scrubbed like a
// message (Scrub). Lines are joined with "\n", and a Go location starts
// with "\t": no other control or format character is ever in the result.
func ScrubStack(stack string) string {
	return scrubStack(stack, false)
}

// ScrubClientStack is ScrubStack for a browser's stack (SourceClient): a
// line in Go's format, which no browser writes, is dropped, and any line
// that isn't a browser's frame is scrubbed as ScrubClient does.
func ScrubClientStack(stack string) string {
	return scrubStack(stack, true)
}

func scrubStack(stack string, client bool) string {
	stack = strings.ToValidUTF8(stack, "�")
	lines := strings.Split(strings.ReplaceAll(stack, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	size := 0
	for _, line := range lines {
		frame := stackLine(line, client)
		if frame == "" {
			continue
		}
		if size+utf8.RuneCountInString(frame)+1 > MaxStack {
			break
		}
		out = append(out, frame)
		size += utf8.RuneCountInString(frame) + 1
	}
	return strings.Join(out, "\n")
}

// stackLine is one line of a stack as ScrubStack keeps it, or "" to drop
// it. The line is cleaned (oneLine) before it's read, so nothing it keeps
// as it is can hold a control character.
func stackLine(raw string, client bool) string {
	tabbed := strings.HasPrefix(raw, "\t")
	line := oneLine(raw)
	trimmed := strings.TrimSpace(line)
	scrub := Scrub
	if client {
		scrub = ScrubClient
	}
	if trimmed == "" {
		return ""
	}
	if m := jsFramePattern.FindStringSubmatch(line); m != nil && (strings.HasPrefix(trimmed, "at ") || strings.Contains(trimmed, "@")) {
		return jsFrame(m)
	}
	if goLine(trimmed, tabbed) {
		if client {
			return "" // no browser writes Go's format
		}
		return goFrame(trimmed, tabbed)
	}
	return scrub(trimmed)
}

// goLine reports whether a cleaned stack line is in Go's format: the
// goroutine header, a location (tab, path.go:line), "created by", or a
// function's name and arguments.
func goLine(trimmed string, tabbed bool) bool {
	switch {
	case strings.HasPrefix(trimmed, "goroutine ") && strings.HasSuffix(trimmed, ":"):
		return true
	case tabbed && strings.Contains(trimmed, ".go:"):
		return true
	case strings.HasPrefix(trimmed, "created by "):
		return true
	case strings.HasSuffix(trimmed, ")"):
		name := goArgsPattern.ReplaceAllString(trimmed, "")
		return name != "" && !strings.ContainsAny(name, " \"'@")
	}
	return false
}

// goFrame is a Go-format line as ScrubStack keeps it: no goroutine header;
// a location as file.go:line only if it is exactly a path and a line (else
// scrubbed as a message); a function without its arguments' values.
func goFrame(trimmed string, tabbed bool) string {
	switch {
	case strings.HasPrefix(trimmed, "goroutine "):
		return ""
	case tabbed && strings.Contains(trimmed, ".go:"):
		location := goOffsetPattern.ReplaceAllString(trimmed, "")
		if goLocationPattern.MatchString(location) {
			return "\t" + baseName(location)
		}
		return Scrub(trimmed)
	case strings.HasPrefix(trimmed, "created by "):
		return "created by " + goFunction(strings.TrimPrefix(trimmed, "created by "))
	}
	return goFunction(trimmed)
}

// goFunction is a Go stack's function line without its arguments' values.
func goFunction(line string) string {
	if i := strings.Index(line, " in goroutine "); i >= 0 {
		line = line[:i]
	}
	return Scrub(goArgsPattern.ReplaceAllString(line, "(…)"))
}

// jsFrame is a browser stack frame as "at name (file.js:line:col)": the
// function's name and the file's name, without its address or query.
func jsFrame(m []string) string {
	name := m[1]
	if name == "" {
		name = m[2]
	}
	file := m[3]
	if i := strings.IndexAny(file, "?#"); i >= 0 {
		file = file[:i]
	}
	file = baseName(file)
	if !scriptFile(file) {
		// The page itself (an inline script): its path could hold a link's
		// token, so it goes.
		file = "<page>"
	} else {
		file = Scrub(file)
	}
	location := file + ":" + m[4]
	if m[5] != "" {
		location += ":" + m[5]
	}
	name = ScrubClient(name)
	if name == "" {
		return "at " + location
	}
	return "at " + name + " (" + location + ")"
}

// scriptFile reports whether name is a script's file name.
func scriptFile(name string) bool {
	for _, ext := range []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx"} {
		if strings.HasSuffix(name, ext) && len(name) > len(ext) {
			return true
		}
	}
	return false
}

// baseName is what follows the last slash.
func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
