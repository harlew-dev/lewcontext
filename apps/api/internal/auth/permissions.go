package auth

import "slices"

// Role is a member's role in a workspace.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// Roles, most access first. Members are listed in this order.
var Roles = []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer}

// Permission is something a member may do in a workspace: <resource>.<action>,
// where the action is the route's CRUD action (plans/features/crud-routing-convention.md).
type Permission string

const (
	WorkspaceUpdate            Permission = "workspace.update"
	WorkspaceDestroy           Permission = "workspace.destroy"
	WorkspaceInvitationIndex   Permission = "workspace.invitation.index"
	WorkspaceInvitationStore   Permission = "workspace.invitation.store"   // send
	WorkspaceInvitationDestroy Permission = "workspace.invitation.destroy" // revoke
	WorkspaceMemberIndex       Permission = "workspace.member.index"       // see who is in the workspace
	WorkspaceMemberUpdate      Permission = "workspace.member.update"      // change a role
	WorkspaceMemberDestroy     Permission = "workspace.member.destroy"     // remove from the workspace; the account stays
	ProjectStore               Permission = "project.store"
	ProjectUpdate              Permission = "project.update"
	ProjectDestroy             Permission = "project.destroy"
)

// AnyMember is granted to every role. A workspace mutation every member may
// do (leaving) names it, so the choice is written out in routes.go.
const AnyMember Permission = "*"

// grants is the whole matrix. Reading a workspace and its projects needs only
// membership. The member and invitation lists hold other people's emails, so
// reading them needs a permission.
var grants = map[Role][]Permission{
	RoleOwner: {
		WorkspaceUpdate, WorkspaceDestroy,
		WorkspaceInvitationIndex, WorkspaceInvitationStore, WorkspaceInvitationDestroy,
		WorkspaceMemberIndex, WorkspaceMemberUpdate, WorkspaceMemberDestroy,
		ProjectStore, ProjectUpdate, ProjectDestroy,
	},
	RoleAdmin: {
		WorkspaceUpdate,
		WorkspaceInvitationIndex, WorkspaceInvitationStore, WorkspaceInvitationDestroy,
		WorkspaceMemberIndex, WorkspaceMemberUpdate, WorkspaceMemberDestroy,
		ProjectStore, ProjectUpdate, ProjectDestroy,
	},
	RoleMember: {ProjectStore, ProjectUpdate},
	RoleViewer: {},
}

// assignable is which roles each role may invite as, change to or from, and
// remove, so an admin can't promote themselves or remove an owner.
var assignable = map[Role][]Role{
	RoleOwner: Roles,
	RoleAdmin: {RoleMember, RoleViewer},
}

// ParseRole accepts only the exact role names.
func ParseRole(s string) (Role, bool) {
	r := Role(s)
	return r, slices.Contains(Roles, r)
}

// Rank orders roles by access: 4 for owner down to 1 for viewer, and 0 for an
// unknown role.
func (r Role) Rank() int {
	i := slices.Index(Roles, r)
	if i < 0 {
		return 0
	}
	return len(Roles) - i
}

// Can reports whether the role grants p. Unknown roles grant nothing.
func (r Role) Can(p Permission) bool {
	if p == AnyMember {
		return r.Rank() > 0
	}
	return slices.Contains(grants[r], p)
}

// Permissions is what the role grants, sorted, for the API response.
func (r Role) Permissions() []Permission {
	ps := slices.Clone(grants[r])
	if ps == nil {
		ps = []Permission{}
	}
	slices.Sort(ps)
	return ps
}

// AssignableRoles are the roles r may give, change to or from, and remove,
// most access first. An action on a member needs a permission as well.
func (r Role) AssignableRoles() []Role {
	roles := slices.Clone(assignable[r])
	if roles == nil {
		roles = []Role{}
	}
	return roles
}
