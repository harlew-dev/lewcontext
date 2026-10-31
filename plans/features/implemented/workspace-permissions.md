# Workspace permissions

Status: **implemented 2026-10-31**. Decisions 1 and 2 confirmed by the user; the others were built as recommended. Builds on [CRUD routing convention](crud-routing-convention.md) for its names. Needed by [workspace members](workspace-members.md) and [workspace invitations](workspace-invitations.md).

## Why

Authorization today is a rank check: `requireRole(store.RoleAdmin, …)` on one route (rename), and nothing on projects, so every member can create and edit them. The coming features need finer rules (who may invite, revoke, remove, or change a role) and a read-only `viewer` role. A list of named permissions, each granted to roles in one table, makes every route say exactly what it needs. It also gives the web app the same answer instead of a copy of the rules (`canManage(role)`).

## Design

### `internal/auth/permissions.go`

Roles and permissions are authorization primitives with no database access, so they live in `internal/auth` (which imports nothing internal). `store` and `httpapi` import them. The role constants move here from `store/workspaces.go`.

```go
package auth

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
// where the action is the route's CRUD action (plans/features/implemented/crud-routing-convention.md).
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

// grants is the whole matrix. Reading a workspace and its projects needs only
// membership. The member and invitation lists hold other people's emails, so
// reading them needs a permission.
var grants = map[Role][]Permission{ … } // the table below

func ParseRole(s string) (Role, bool)
func (r Role) Rank() int                    // 4 owner … 1 viewer; 0 for an unknown role
func (r Role) Can(p Permission) bool        // false for an unknown role
func (r Role) Permissions() []Permission    // sorted, for the API response
func (r Role) AssignableRoles() []Role      // the roles r may give, change or remove (below)
```

### The matrix

| Permission | owner | admin | member | viewer | Guards |
| --- | --- | --- | --- | --- | --- |
| *(membership)* | ✓ | ✓ | ✓ | ✓ | `GET` the workspace and its projects |
| `workspace.update` | ✓ | ✓ | | | `PATCH /workspaces/{ws}` (exists) |
| `workspace.destroy` | ✓ | | | | no endpoint yet |
| `workspace.invitation.index` | ✓ | ✓ | | | `GET …/invitations` (invitations plan) |
| `workspace.invitation.store` | ✓ | ✓ | | | `POST …/invitations` |
| `workspace.invitation.destroy` | ✓ | ✓ | | | `DELETE …/invitations/{inv}` |
| `workspace.member.index` | ✓ | ✓ | | | `GET …/members` (members plan) |
| `workspace.member.update` | ✓ | ✓ | | | `PATCH …/members/{usr}` |
| `workspace.member.destroy` | ✓ | ✓ | | | `DELETE …/members/{usr}` |
| `project.store` | ✓ | ✓ | ✓ | | `POST …/projects` (exists) |
| `project.update` | ✓ | ✓ | ✓ | | `PATCH …/projects/{prj}` (exists) |
| `project.destroy` | ✓ | ✓ | | | no endpoint yet |

**Viewer** is read-only: every `GET` except the member and invitation lists, and no mutations. **Members and viewers can't see who else is in the workspace.** Only owners and admins can list members or invitations. Leaving a workspace isn't a permission. Anyone may leave, apart from the last owner (members plan).

### Which roles a role may manage

A permission says what kind of action is allowed. For actions on another person's role, a second rule says which roles may be targeted, so an admin can't promote themselves or remove an owner:

| Actor | May invite as, change to or from, and remove |
| --- | --- |
| owner | owner, admin, member, viewer (any, including other owners) |
| admin | member, viewer |
| member, viewer | none |

`AssignableRoles()` returns that list. An action on a member needs both the permission and the target's current role (and, for a role change, the new role) to be in the actor's `AssignableRoles()`. The members and invitations plans apply it. The last-owner rule is a store check, because it depends on data, not roles.

### HTTP: `can` replaces `requireRole`

```go
// routes.go: workspace mutations name the permission they need
ws.Can("PATCH", "", auth.WorkspaceUpdate, workspaces.update)
ws.HandleFunc("GET", "/projects", projects.index)
ws.Can("POST", "/projects", auth.ProjectStore, projects.store)
ws.Can("PATCH", "/projects/{project}", auth.ProjectUpdate, projects.update)
```

- `routeGroup.Can(method, path, permission, handler)` wraps the handler in `requirePermission(p)`, which runs after `requireWorkspace` and returns 403 with the existing message when `currentWorkspace(r).Role.Can(p)` is false.
- **Missing permissions are caught by construction.** The `ws` group is created with `requirePermissions: true`, and its `Handle`/`HandleFunc` panic at startup for any method other than GET or HEAD. A new workspace mutation without a permission therefore can't start the server, and every test fails. The rare mutation every member may do (leaving) uses `ws.Can(…, auth.AnyMember, …)`, an explicit sentinel, so the choice is visible in `routes.go`.
- `requireRole` and `store.RoleAtLeast` are deleted.

### API: the member's permissions

The workspace response (`GET /api/workspaces`, `GET /api/workspaces/{ws}`, and the responses to store, update and accept) gains two fields, computed in Go from the role at no query cost:

```json
{ "id": "wsp_…", "name": "Acme", "role": "admin",
  "permissions": ["project.destroy", "project.store", "project.update", "workspace.invitation.destroy", "…"],
  "assignableRoles": ["member", "viewer"],
  "createdAt": "…", "updatedAt": "…" }
```

The list includes them too, because the web app shows a workspace from the list at once while `show` refreshes it.

### Data

- `004_create_workspaces` (squashed, edited in place): `role IN ('owner','admin','member','viewer')`. Rebuild with `make migrate-fresh CONFIRM=1 && make seed`.
- `store.Workspace.Role` and `membership.Role` become `auth.Role` (a string type, so Bun scans it unchanged).
- Factory: `workspaceRoles` becomes `auth.Roles`, so random memberships include viewers.

## Web app

- `types/workspaces.ts`: `roles` gains `viewer`, a `Permission` union mirrors the Go constants, and `Workspace` gains `permissions` and `assignableRoles`. `can(workspace, "project.store")` replaces `canManage(role)`. Because the list comes from the API, there is no second matrix in TypeScript.
- **Viewer UI:**
  - Projects hides "New project".
  - The project page shows its fields read-only with an `InputLock` ("Viewers can't edit projects"), as workspace settings does for rename.
  - The settings role badge gets a viewer entry.
- The API still enforces everything. The UI only hides what would be refused.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A new mutation ships without a check | `ws` group panics on an unguarded non-GET route; `AnyMember` must be written out. The role × route integration matrix (Tests) fails for a new route until it's listed. |
| An unknown or corrupted role grants access | `Can` and `Rank` return false or 0 for unknown roles; the column `CHECK` limits the values. |
| UI and API disagree | The UI reads `permissions` from the API instead of keeping its own rules. |
| Privilege escalation (admin promotes self to owner, removes an owner) | `AssignableRoles` (admins manage only member and viewer), applied by the members and invitations plans. |
| Role read from the request | Never. The role comes from `workspace_user` via `requireWorkspace`. |

## Tests

- **Go unit (`auth`):**
  - every permission is granted to owner;
  - viewer has none;
  - each role's permissions are a subset of the role above's;
  - `ParseRole` rejects unknown and wrong-case values;
  - the `AssignableRoles` table;
  - every `Permission` constant appears in `grants` at least once.
- **Go unit (`httpapi`):** a non-GET `Handle` on the `ws` group panics.
- **Go integration, a role × route matrix:** for every workspace route and every role, the expected status: 2xx for allowed roles, 403 for refused ones. Viewer gets the workspace and project GETs and is refused `POST`/`PATCH` on projects. Members and viewers are refused the member and invitation lists (403). Non-members still get 404. One table, extended by each later plan.
- **Go integration:** the workspace response's `permissions` and `assignableRoles` for each role.
- **Vitest:** a viewer sees no "New project" and a read-only project form; an admin sees both.

## Docs

`docs/api/auth.md` (the roles table becomes this matrix and the assignable-roles table, `can` replaces `requireRole`, and "Protecting a new endpoint"), `docs/api/endpoints.md` (the Requires column names permissions; workspace fields), `docs/api/database.md` (role values), `docs/ui/pages.md` (`can` replaces `canManage`), and AGENTS.md (security invariants: `requireRole` → `can`).

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Permission names | `<resource>.<action>` with the route's CRUD action: `workspace.update` (you wrote `workspace.edit`), `workspace.invitation.store` (send), `workspace.invitation.destroy` (revoke), `workspace.member.destroy` (your `workspace.user.remove`: it removes the membership, not the user). Each permission then matches its route. |
| 2 | Added permissions | `workspace.member.index` (see the member list), `workspace.member.update` (change a role) and `workspace.invitation.index` (see pending invitations), which the members and invitations plans need. |
| 3 | Where it lives | `internal/auth/permissions.go`, with the role constants moved from `store`. |
| 4 | Viewer | Read-only everywhere, including projects. |
| 5 | `project.destroy` for members | No: owners and admins only, as with removing people. |
| 6 | Admins and admin roles | Admins manage only member and viewer, so only owners can create admins. |
| 7 | Enforcing a permission on every mutation | Panic at startup on an unguarded `ws` mutation, with an explicit `auth.AnyMember` for leaving. |
| 8 | The web app's copy of the rules | None: it reads `permissions` and `assignableRoles` from the workspace response. |
| 9 | Who can see members | Owners and admins only (`workspace.member.index`). Members and viewers never see the list or other members' emails. |

## Not included

- Delete endpoints for projects and workspaces. Their permissions are defined so the routes can be added with `ws.Can(…)` later; deleting a workspace needs its own plan (cascade, confirmation, the events it leaves).
- Custom roles or per-member permission overrides.
- Account-level (non-workspace) permissions, such as a site admin.

## As built

Where the build differs from the design above:

- **Project page:** it has no form (details plus an Edit dialog), so a member without `project.update` sees the details without the "Edit project" button, rather than locked fields. The projects list's empty state likewise drops "Create a project" and says projects will show once someone creates one.
- **Routes:** the workspace routes live in `workspaceRoutes(ws)` in `routes.go`, which sets `requirePermissions`, so the matrix test can register them on a recording group and fail when a route has no row in `workspaceRoutesMatrix`.
- **Names:** the CRUD routing convention isn't implemented yet, so handlers keep their names (`s.createProject`, `s.updateWorkspace`); only the permissions use the convention.
- `auth.AnyMember` is `"*"`, granted to every known role and never listed in `permissions`. Nothing uses it yet.
- The response fields come from `store.Workspace.MarshalJSON`, so every workspace response (list, show, store, update) carries them.
- Web tests build workspaces with `testWorkspace(role)` (`src/test/workspace.ts`), the one place outside Go that spells out each role's permissions. `types/workspaces.test.ts` checks the TypeScript names against the Go source.

Docs updated: [auth.md](../../../docs/api/auth.md#workspaces-and-roles), [endpoints.md](../../../docs/api/endpoints.md), [api/README.md](../../../docs/api/README.md), [database.md](../../../docs/api/database.md), [seeding.md](../../../docs/api/seeding.md), [api/testing.md](../../../docs/api/testing.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/testing.md](../../../docs/ui/testing.md), [docs/README.md](../../../docs/README.md) and AGENTS.md.
