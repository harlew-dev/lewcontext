package input

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bounds of an admin action's reason, in characters, after trimming.
const (
	MinReason = 10
	MaxReason = 500
)

// Reason trims an admin action's reason, from the dashboard or the command
// line (cmd/admin's -reason), and checks it is 10 to 500 characters
// (counted as Unicode characters, as sign-up counts a password's), with no
// character that could hide or rearrange what an audit record says: line
// breaks and tabs are allowed, but no other control character (and no NUL,
// which PostgreSQL's jsonb can't hold), no format character (bidi
// overrides such as U+202E, zero-width characters, U+FEFF) and no Unicode
// line or paragraph separator.
func Reason(reason *string) bool {
	*reason = strings.TrimSpace(*reason)
	n := utf8.RuneCountInString(*reason)
	return n >= MinReason && n <= MaxReason && !strings.ContainsFunc(*reason, hiddenInReason)
}

// hiddenInReason reports whether c may not appear in a reason.
func hiddenInReason(c rune) bool {
	switch {
	case c == '\n' || c == '\r' || c == '\t':
		return false
	case c == '\u2028' || c == '\u2029':
		return true
	}
	return unicode.IsControl(c) || unicode.Is(unicode.Cf, c)
}
