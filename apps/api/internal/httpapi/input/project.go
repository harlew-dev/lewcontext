package input

import (
	"lewcontext/api/internal/store"
	"slices"
	"strings"
	"unicode/utf8"
)

// ValidProject is the common browser/v1 write contract. PATCH supplies the full
// editable representation; descriptions may contain ordinary line breaks/tabs.
func ValidProject(in *store.ProjectInput) (field, message string, ok bool) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	n := utf8.RuneCountInString(in.Name)
	switch {
	case n < 1 || n > 100 || !ValidText(in.Name, false):
		return "name", "Use a project name of 1–100 characters, without invisible formatting characters.", false
	case utf8.RuneCountInString(in.Description) > 500 || !ValidText(in.Description, true):
		return "description", "Use a description of up to 500 characters, without invisible formatting characters.", false
	case !slices.Contains(ProjectStatuses, in.Status) || !slices.Contains([]string{"violet", "blue", "amber", "emerald", "rose"}, in.Color):
		return "", "Check the project name, description, status, and color.", false
	}
	return "", "", true
}
