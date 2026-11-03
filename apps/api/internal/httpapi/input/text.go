package input

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// HiddenCharacter reports whether r may not appear in text people type for
// others to read (names, descriptions, reasons): a character that is
// invisible and can hide or rearrange what the text appears to say, or one
// PostgreSQL can't store (NUL). That is every control character, the bidi
// embeddings, overrides, isolates and marks (U+202A–202E, U+2066–2069,
// U+061C, U+200E, U+200F), zero-width spaces and invisible operators
// (U+200B, U+2060–2064, U+FEFF), and the Unicode line and paragraph
// separators (U+2028, U+2029).
//
// It allows the invisible characters ordinary text needs: the zero-width
// joiner and non-joiner (emoji such as 👩\u200D💻 and 🏳\uFE0F\u200D🌈, Persian and Indic
// scripts), the soft hyphen, variation selectors and tag characters
// (subdivision flags). ValidText decides about line breaks and tabs.
func HiddenCharacter(r rune) bool {
	switch {
	case unicode.IsControl(r):
		return true
	case r >= '\u202A' && r <= '\u202E', r >= '\u2066' && r <= '\u2069', r == '\u061C', r == '\u200E', r == '\u200F':
		return true
	case r == '\u200B', r >= '\u2060' && r <= '\u2064', r == '\uFEFF':
		return true
	case r == '\u2028', r == '\u2029':
		return true
	}
	return false
}

// ValidText reports whether s is valid UTF-8 with no HiddenCharacter, apart
// from line breaks and tabs (\n, \r, \t) when allowLineBreaks (a
// description, a reason; never a name).
func ValidText(s string, allowLineBreaks bool) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool {
		if allowLineBreaks && (r == '\n' || r == '\r' || r == '\t') {
			return false
		}
		return HiddenCharacter(r)
	})
}
