package input

import (
	"lewcontext/api/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryParams(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  bool
	}{
		{"", true},
		{"page=2&q=launch", true},
		{"page=1&page=2", false},
		{"perPage=5&unknown=1", false},
		{"q=%zz", false},
		{"q=a;b", false},
	} {
		r := httptest.NewRequest("GET", "/api/projects?"+tc.query, nil)
		if _, ok := Query(r, "page", "perPage", "q"); ok != tc.want {
			t.Errorf("%q: want %v got %v", tc.query, tc.want, ok)
		}
	}
}
func TestPageRequest(t *testing.T) {
	for _, tc := range []struct {
		page, perPage string
		want          store.PageRequest
		ok            bool
	}{
		{"", "", store.PageRequest{Page: 1, PerPage: store.DefaultPerPage}, true},
		{"3", "5", store.PageRequest{Page: 3, PerPage: 5}, true},
		{"10000", "100", store.PageRequest{Page: 10000, PerPage: 100}, true},
		{"0", "", store.PageRequest{}, false},
		{"-1", "", store.PageRequest{}, false},
		{"+2", "", store.PageRequest{}, false},
		{" 2", "", store.PageRequest{}, false},
		{"1e3", "", store.PageRequest{}, false},
		{"10001", "", store.PageRequest{}, false},
		{"99999999999999999999", "", store.PageRequest{}, false},
		{"", "0", store.PageRequest{}, false},
		{"", "101", store.PageRequest{}, false},
	} {
		got, ok := Page(map[string]string{"page": tc.page, "perPage": tc.perPage})
		if ok != tc.ok || got != tc.want {
			t.Errorf("page=%q perPage=%q: want %+v %v got %+v %v", tc.page, tc.perPage, tc.want, tc.ok, got, ok)
		}
	}
}
func TestProjectFilter(t *testing.T) {
	got, ok := ProjectFilter(map[string]string{"q": "  launch 50%  ", "status": "Planned"})
	if !ok || got != (store.ProjectFilter{Search: "launch 50%", Status: "Planned", Sort: store.DefaultProjectSort}) {
		t.Fatalf("valid filter: got %+v %v", got, ok)
	}
	for _, params := range []map[string]string{
		{"status": "Archived"},
		{"status": "planned"},
		{"sort": "user_id"},
		{"sort": "name; DROP TABLE projects"},
		{"q": "nul\x00byte"},
		{"q": "line\nbreak"},
		{"q": "\xff"},
		{"q": strings.Repeat("é", maxSearchLength+1)},
	} {
		if _, ok := ProjectFilter(params); ok {
			t.Errorf("invalid filter accepted: %q", params)
		}
	}
	if _, ok := ProjectFilter(map[string]string{"q": strings.Repeat("é", maxSearchLength)}); !ok {
		t.Error("search at the length limit rejected")
	}
}
