package input

import (
	"lewcontext/api/internal/store"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ProjectStatuses are the statuses a project can have.
var ProjectStatuses = []string{"Planned", "In progress", "Completed"}

// maxSearchLength bounds the project search term in Unicode characters.
const maxSearchLength = 100

// ProjectFilter validates the listing's "q", "status" and "sort" parameters.
func ProjectFilter(params map[string]string) (store.ProjectFilter, bool) {
	f := store.ProjectFilter{
		Search: strings.TrimSpace(params["q"]),
		Status: params["status"],
		Sort:   params["sort"],
	}
	if !Search(f.Search) {
		return store.ProjectFilter{}, false
	}
	if f.Status != "" && !slices.Contains(ProjectStatuses, f.Status) {
		return store.ProjectFilter{}, false
	}
	if f.Sort == "" {
		f.Sort = store.DefaultProjectSort
	}
	if !store.ProjectSortable(f.Sort) {
		return store.ProjectFilter{}, false
	}
	return f, true
}

// Search rejects invalid UTF-8 and control characters (PostgreSQL text
// cannot hold NUL) and bounds the length.
func Search(term string) bool {
	if !utf8.ValidString(term) || utf8.RuneCountInString(term) > maxSearchLength {
		return false
	}
	return !strings.ContainsFunc(term, unicode.IsControl)
}
