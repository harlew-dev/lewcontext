package store

import "testing"

func TestPagesFor(t *testing.T) {
	for _, tc := range []struct {
		req   PageRequest
		items int
		want  Pages
	}{
		{PageRequest{1, 20}, 0, Pages{Current: 1, Total: 1, PerPage: 20, Items: 0}},
		{PageRequest{1, 20}, 20, Pages{Current: 1, Total: 1, PerPage: 20, Items: 20}},
		{PageRequest{2, 20}, 21, Pages{Current: 2, Total: 2, PerPage: 20, Items: 21}},
		{PageRequest{9, 5}, 12, Pages{Current: 9, Total: 3, PerPage: 5, Items: 12}},
	} {
		if got := pagesFor(tc.req, tc.items); got != tc.want {
			t.Errorf("pagesFor(%+v, %d): want %+v got %+v", tc.req, tc.items, tc.want, got)
		}
	}
}
func TestContainsPattern(t *testing.T) {
	for term, want := range map[string]string{
		"launch":  "%launch%",
		"50%":     `%50\%%`,
		"a_b":     `%a\_b%`,
		`C:\path`: `%C:\\path%`,
	} {
		if got := containsPattern(term); got != want {
			t.Errorf("containsPattern(%q): want %q got %q", term, want, got)
		}
	}
}
