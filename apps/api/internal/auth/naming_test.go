package auth

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Permissions are <resource>.<action> (docs/naming.md#permissions-internalauthpermissionsgo):
// snake_case segments ending in the CRUD action of the route the permission
// guards, and the Go constant is the same words in PascalCase
// (workspace.member.destroy is WorkspaceMemberDestroy).

// notPermissionNames are the Permission constants that aren't
// <resource>.<action>, each with its reason.
var notPermissionNames = map[string]string{
	"AnyMember": "granted to every role (leaving a workspace): no resource or action",
}

var permissionPattern = regexp.MustCompile(`^[a-z][a-z_]*(\.[a-z][a-z_]*)*\.(index|show|store|update|destroy)$`)

// badPermission says what's wrong with a permission constant, or "".
func badPermission(name, value string) string {
	if notPermissionNames[name] != "" {
		return ""
	}
	if !permissionPattern.MatchString(value) {
		return fmt.Sprintf("permission %s = %q: permissions are <resource>.<action>, snake_case, ending in index, show, store, update or destroy; see docs/naming.md#permissions-internalauthpermissionsgo", name, value)
	}
	if want := pascalCase(value); name != want {
		return fmt.Sprintf("permission %s = %q: its constant is the same words in PascalCase, %s; see docs/naming.md#permissions-internalauthpermissionsgo", name, value, want)
	}
	return ""
}

// pascalCase spells a permission as its constant: workspace.member.destroy
// is WorkspaceMemberDestroy.
func pascalCase(value string) string {
	var b strings.Builder
	for _, word := range strings.FieldsFunc(value, func(r rune) bool { return r == '.' || r == '_' }) {
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	return b.String()
}

func TestPermissionNames(t *testing.T) {
	for _, d := range permissionDeclarations(t) {
		if problem := badPermission(d.name, d.value); problem != "" {
			t.Error(problem)
		}
	}
	for name := range notPermissionNames {
		found := false
		for _, d := range permissionDeclarations(t) {
			found = found || d.name == name
		}
		if !found {
			t.Errorf("%s is listed as an exception but isn't declared", name)
		}
	}
}

// The check itself.
func TestBadPermission(t *testing.T) {
	for name, value := range map[string]string{
		"WorkspaceMemberDestroy": "workspace.member.destroy",
		"ProjectStore":           "project.store",
		"EmailBlockIndex":        "email_block.index",
		"AnyMember":              "*",
	} {
		if problem := badPermission(name, value); problem != "" {
			t.Errorf("refused: %s", problem)
		}
	}
	for name, value := range map[string]string{
		"WorkspaceMemberRemove": "workspace.member.remove",
		"ProjectCreate":         "project.create",
		"MemberDestroy":         "workspace.member.destroy",
		"ProjectStore":          "Project.store",
		"Everyone":              "*",
		"Project":               "project",
	} {
		if problem := badPermission(name, value); !strings.Contains(problem, "docs/naming.md#permissions") {
			t.Errorf("%s = %q accepted", name, value)
		}
	}
}
