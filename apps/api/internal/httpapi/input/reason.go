package input

import (
	"strings"
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
// character that could hide or rearrange what an audit record says:
// ValidText with line breaks and tabs allowed, so no other control
// character (and no NUL, which PostgreSQL's jsonb can't hold), no bidi
// override such as U+202E, no zero-width space or U+FEFF, and no Unicode
// line or paragraph separator.
func Reason(reason *string) bool {
	*reason = strings.TrimSpace(*reason)
	n := utf8.RuneCountInString(*reason)
	return n >= MinReason && n <= MaxReason && ValidText(*reason, true)
}
