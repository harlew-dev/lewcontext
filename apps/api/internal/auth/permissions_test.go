package auth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

// declaredPermissions reads every Permission constant from permissions.go, so
// a new one can't be left out of the matrix.
func declaredPermissions(t *testing.T) []Permission {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "permissions.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []Permission
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if typ, ok := spec.Type.(*ast.Ident); !ok || typ.Name != "Permission" {
			return true
		}
		for _, v := range spec.Values {
			value, err := strconv.Unquote(v.(*ast.BasicLit).Value)
			if err != nil {
				t.Fatal(err)
			}
			if Permission(value) != AnyMember {
				found = append(found, Permission(value))
			}
		}
		return true
	})
	if len(found) < 11 {
		t.Fatalf("found only %d permissions in permissions.go", len(found))
	}
	return found
}
func TestEveryPermissionIsGrantedToOwner(t *testing.T) {
	for _, p := range declaredPermissions(t) {
		if !RoleOwner.Can(p) {
			t.Errorf("owner lacks %s", p)
		}
	}
	if len(RoleOwner.Permissions()) != len(declaredPermissions(t)) {
		t.Fatalf("owner grants %v", RoleOwner.Permissions())
	}
}
func TestViewerIsReadOnly(t *testing.T) {
	if got := RoleViewer.Permissions(); got == nil || len(got) != 0 {
		t.Fatalf("viewer grants %#v, want an empty list", got)
	}
	if !RoleViewer.Can(AnyMember) {
		t.Fatal("viewer is refused AnyMember")
	}
}
func TestEachRoleGrantsASubsetOfTheRoleAbove(t *testing.T) {
	for i := 1; i < len(Roles); i++ {
		above, role := Roles[i-1], Roles[i]
		for _, p := range role.Permissions() {
			if !above.Can(p) {
				t.Errorf("%s has %s but %s doesn't", role, p, above)
			}
		}
		if above.Rank() <= role.Rank() {
			t.Errorf("%s ranks %d, %s %d", above, above.Rank(), role, role.Rank())
		}
	}
	if got := RoleMember.Permissions(); !slices.Equal(got, []Permission{ProjectStore, ProjectUpdate}) {
		t.Fatalf("member grants %v", got)
	}
}
func TestPermissionsAreSorted(t *testing.T) {
	for _, role := range Roles {
		if !slices.IsSorted(role.Permissions()) {
			t.Errorf("%s permissions unsorted: %v", role, role.Permissions())
		}
	}
	// The response is a copy; changing it doesn't change the matrix.
	RoleOwner.Permissions()[0] = "workspace.anything"
	if RoleOwner.Can("workspace.anything") {
		t.Fatal("Permissions returned the matrix itself")
	}
}
func TestUnknownRolesGrantNothing(t *testing.T) {
	for _, role := range []Role{"", "root", "Owner", "owner "} {
		if role.Rank() != 0 || role.Can(ProjectStore) || role.Can(AnyMember) || len(role.Permissions()) != 0 || len(role.AssignableRoles()) != 0 {
			t.Errorf("unknown role %q grants access", role)
		}
	}
}
func TestParseRole(t *testing.T) {
	for _, s := range []string{"owner", "admin", "member", "viewer"} {
		if role, ok := ParseRole(s); !ok || string(role) != s {
			t.Errorf("ParseRole(%q) = %q, %v", s, role, ok)
		}
	}
	for _, s := range []string{"", "Owner", "ADMIN", " member", "guest", "*"} {
		if _, ok := ParseRole(s); ok {
			t.Errorf("ParseRole(%q) accepted", s)
		}
	}
}
func TestAssignableRoles(t *testing.T) {
	want := map[Role][]Role{
		RoleOwner:  {RoleOwner, RoleAdmin, RoleMember, RoleViewer},
		RoleAdmin:  {RoleMember, RoleViewer},
		RoleMember: {},
		RoleViewer: {},
	}
	for role, roles := range want {
		got := role.AssignableRoles()
		if got == nil || !slices.Equal(got, roles) {
			t.Errorf("%s may assign %#v, want %v", role, got, roles)
		}
	}
	for _, actor := range Roles {
		for _, target := range append(slices.Clone(Roles), "", "root") {
			if actor.Manages(target) != slices.Contains(want[actor], target) {
				t.Errorf("%s manages %q: %v", actor, target, actor.Manages(target))
			}
		}
	}
	RoleOwner.AssignableRoles()[0] = RoleViewer
	if Roles[0] != RoleOwner {
		t.Fatal("AssignableRoles returned Roles itself")
	}
}
