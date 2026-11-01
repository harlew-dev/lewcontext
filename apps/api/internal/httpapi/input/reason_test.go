package input

import (
	"strings"
	"testing"
)

// A reason is trimmed, then 10 to 500 characters (not bytes), with line
// breaks and tabs but nothing that could hide or rearrange what the audit
// trail says.
func TestReason(t *testing.T) {
	for _, tc := range []struct {
		reason, want string
		ok           bool
	}{
		{"  Spam from this account  ", "Spam from this account", true},
		{"Ticket 4521:\n\tinvitation spam", "Ticket 4521:\n\tinvitation spam", true},
		{strings.Repeat("é", 500), strings.Repeat("é", 500), true},
		{"too short", "", false},
		{"   short   ", "", false},
		{strings.Repeat("é", 501), "", false},
		{"Spam \u202eklat\u202c from them", "", false},
		{"Spam\u200b from this account", "", false},
		{"Spam from\u2028this account", "", false},
		{"Spam from\x00this account", "", false},
		{"\ufeffSpam from this account", "", false},
		{"Spam \u2066from\u2069 this account", "", false},
		{"Spam from this account \xff", "", false},
		// What ordinary text needs stays (input.ValidText): an emoji's joiner.
		{"Spam from 👩\u200d💻 accounts", "Spam from 👩\u200d💻 accounts", true},
	} {
		got := tc.reason
		if ok := Reason(&got); ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%q: %v %q", tc.reason, ok, got)
		}
	}
}
