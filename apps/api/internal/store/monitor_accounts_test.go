package store

import "testing"

// Links and markup in a name, as the Accounts page flags them: a scheme,
// www., a domain-looking word or an angle bracket, in any case.
func TestLinkInName(t *testing.T) {
	for _, name := range []string{"Visit http://spam.example", "HTTPS://x", "www.cheap", "Win at cheap-pills.example", "Ada <b>", "a>b", "Mr.Smith"} {
		if !LinkInName(name) {
			t.Errorf("%q: not flagged", name)
		}
	}
	for _, name := range []string{"Ada Lovelace", "Acme Inc.", "J. R. R. Tolkien", "Team 2.0", "O'Brien & Co"} {
		if LinkInName(name) {
			t.Errorf("%q: flagged", name)
		}
	}
}
