# CRUD naming convention

Status: **implemented 2026-10-31**, in one change right after the workspace-members commit. This plan also absorbs the former `store-method-names` plan.

## Why

Each resource names its routes differently in each layer. `POST /api/workspaces/{workspace}/projects` is `s.createProject` in Go, `api.projects(id).create()` in the client, and `routes.projects(ws)` / `routes.project(ws, id)` as pages. Some store methods are bare nouns (`Project`, `Session`), so a call doesn't say whether it reads or writes. Settling one vocabulary means each new route, method and page is named without thinking about it, and a permission can be read straight off its route (`project.update` guards `projects.update`).

The HTTP convention is Laravel's resource controller:

| Action | Method | Path | Does |
| --- | --- | --- | --- |
| `index` | GET | `/things` | one page of the list |
| `show` | GET | `/things/{thing}` | one record |
| `store` | POST | `/things` | create, 201 with the record |
| `update` | PATCH | `/things/{thing}` | change, 200 with the record |
| `destroy` | DELETE | `/things/{thing}` | delete, 204 |

The web app adds Laravel's two page actions, `create` and `edit`. Neither is ever an API call.

## The convention

### HTTP (`internal/httpapi`)

- **Paths** are plural nouns under their parent, as today (`/api/workspaces/{workspace}/projects/{project}`). Path IDs are public IDs.
- **Handlers** for one resource are methods on a small per-resource type, named after the action:

  ```go
  // httpapi/projects.go
  type projectHandlers struct{ *Server }

  func (h projectHandlers) index(w http.ResponseWriter, r *http.Request)   { … }
  func (h projectHandlers) show(w http.ResponseWriter, r *http.Request)    { … }
  func (h projectHandlers) store(w http.ResponseWriter, r *http.Request)   { … }
  func (h projectHandlers) update(w http.ResponseWriter, r *http.Request)  { … }
  func (h projectHandlers) destroy(w http.ResponseWriter, r *http.Request) { … }

  // routes.go
  projects := projectHandlers{s}
  ws.HandleFunc("GET", "/projects", projects.index)
  ws.Can("POST", "/projects", auth.ProjectStore, projects.store)
  ```

  In `routes.go`, each line then reads as Laravel's route name (`projects.index`). One file per resource stays as it is. Shared helpers (`validProject`, `projectID`) stay plain functions.
- **`update` is PATCH.** PATCH may still require the whole editable representation, as projects do. The method says "update", not "partial".
- **A singleton resource** (one per parent or per session, no ID in the path) uses the same actions: leaving a workspace is `membership.destroy` on `DELETE …/membership`.
- **Actions that aren't one of the five** are named verbs, as `POST` on the record or collection: `POST /api/auth/password/verify`, `POST /api/invitations/accept`. The handler method is the verb (`invitations.accept`). Before adding one, check whether it's really a `store` or `destroy` of some other resource.
- **`/api/auth/*` keeps its paths and handler names** (`register`, `login`, `logout`, `me`, `updateProfile`, `revokeOthers`, `verifyPassword`, `changePassword`, the reset handlers). These are named actions on the session and password. Remodelling them as resources would change every auth test and doc for no gain. Only their two `PUT`s become `PATCH`.

### Store (`internal/store`)

**Store methods start with a verb.** The store talks in data verbs; the Laravel verbs (`show`, `store`, `destroy`) belong to the HTTP layer.

| Verb | For | Examples |
| --- | --- | --- |
| `Index<Resources>` | one page of a list, including search and filters | `IndexProjects`, `IndexMembers`, `IndexWorkspaces` (already in place) |
| `Find<Resource>` | loading one record; `sql.ErrNoRows` when it's missing or out of scope | `FindUser`, `FindProject`, `FindWorkspace` |
| `Create` / `Update` / `Delete<Resource>` | plain writes | `CreateProject`, `UpdateProject`, `DeleteProject` |
| a domain verb | when it says more than create, update or delete | `RenameWorkspace`, `UpdateMemberRole`, `RemoveMember`, `LeaveWorkspace`, `IssuePasswordReset`, `AcceptInvitation` |
| `Check<Thing>` | a yes/no question about a record | `CheckPasswordReset` |

- There are no noun-only methods.
- **The exception:** methods that implement an interface defined elsewhere take that interface's names. That means the event worker's `Claim`, `Delivered`, `Fail` and `Retry` for `events.Queue`.
- One store method may still serve several routes.

### Events

Events stay facts in the past tense, the existing `<area>.<past_tense>` rule:

- `store` records `*_created`;
- `update` records `*_updated`, or a more specific fact (`workspace.renamed`);
- `destroy` records `*_deleted`, or a more specific fact (`workspace.member_removed`, `workspace.member_left`, `workspace.invitation_revoked`).

### Web app

- **API client** (`lib/api/<resource>.ts`) uses the API's names: `index`, `show`, `store`, `update`, `destroy`, plus named verbs (`accept`). The same names are used for both the call (`api.projects(ws).store(input)`) and the typed path (`paths.projects(ws).store()`).
- **Page routes** (`lib/routes.ts`) are grouped by resource and use the page actions:

  | Page action | Is | Example |
  | --- | --- | --- |
  | `index` | a list page | `routes.projects.index(ws)` → `/app/<wsp>/projects` |
  | `show` | one record's page | `routes.projects.show(ws, prj)` → `/app/<wsp>/projects/<prj>` |
  | `create` | a page for a new record, only when it isn't a dialog | none today (projects are created in a dialog) |
  | `edit` | a page for editing a record | `routes.workspaces.edit(ws)` → `/app/<wsp>/settings` |

  **Delete is never a page.** It is a confirmation dialog that calls `destroy`.
- **Pages that aren't a resource keep plain names:** `home`, `app`, `onboarding`, `settings` (account), `login`, `register`, `forgotPassword`, `dashboard(ws)`.
- **URLs don't change.** `/app/<wsp>/settings` stays `/settings`, not `/edit`, because "Settings" is the name people see. Only the constant's name follows the convention.
- **Page files stay shaped like the URL** (`projects.tsx`, `projects/[project].tsx`), since `src/routes.ts` maps each URL to its file explicitly. **Default exports** of resource pages are named `<Resource><Action>`, which shows up in React DevTools and stack traces.

## Changes to existing code

As of 2026-10-31, after the members work. Before starting, recheck `routes.go`, `lib/routes.ts`, `lib/api/*.ts` and `pages/` for anything added since, and rename it the same way.

### Go handlers (`httpapi`, `routes.go`)

| Route | Now | After |
| --- | --- | --- |
| `GET /api/workspaces` | `s.workspaces` | `workspaces.index` |
| `POST /api/workspaces` | `s.createWorkspace` | `workspaces.store` |
| `GET /api/workspaces/{workspace}` | `s.workspace` | `workspaces.show` |
| `PATCH /api/workspaces/{workspace}` | `s.updateWorkspace` | `workspaces.update` |
| `GET …/projects` | `s.projects` | `projects.index` |
| `POST …/projects` | `s.createProject` | `projects.store` |
| `GET …/projects/{project}` | `s.project` | `projects.show` |
| `PATCH …/projects/{project}` | `s.updateProject` | `projects.update` |
| `GET …/members` | `s.members` | `members.index` |
| `PATCH …/members/{member}` | `s.updateMember` | `members.update` |
| `DELETE …/members/{member}` | `s.removeMember` | `members.destroy` |
| `DELETE …/membership` | `s.leaveWorkspace` | `membership.destroy` |
| `PUT /api/auth/password` | PUT | **PATCH** (decision 3) |
| `PUT /api/auth/password-reset` | PUT | **PATCH** (decision 3) |

The handler types are `workspaceHandlers`, `projectHandlers`, `memberHandlers` and `membershipHandlers`.

### Store methods

| Now | After | Defined in | Called from (as of 2026-10-31) |
| --- | --- | --- | --- |
| `Project` | `FindProject` | `store/projects.go` | `httpapi/projects.go` |
| `Session` | `FindSession` | `store/sessions.go` | `httpapi/middleware.go` |
| `MemberWorkspace` | `FindWorkspace` | `store/workspaces.go` | `httpapi/middleware.go` (`requireWorkspace`) |
| `PasswordHash` | `FindPasswordHash` | `store/users.go` | `httpapi/password.go` |
| `PasswordResetValid` | `CheckPasswordReset` | `store/password_resets.go` | `httpapi/password_reset.go` (2 calls) |

`FindWorkspace` still loads the workspace **through the user's membership** (`userID, publicID`), and still returns `sql.ErrNoRows` for both a missing workspace and a non-member. Its doc comment must keep saying so, because the new name no longer mentions membership.

Unchanged: `Claim`, `Delivered`, `Fail`, `Retry` (the `events.Queue` interface); `Record`, `Cleanup`, `Prune`, `CountPrunable` (already verbs); and `AllowAuth`, which the [rate-limits plan](rate-limits.md) renames to `Allow`.

### Web app

| Where | Now | After |
| --- | --- | --- |
| `lib/api/workspaces.ts` (call and path) | `create` | `store` |
| `lib/api/projects.ts` (call and path) | `create` | `store`; `destroy` stays commented until the deletion plan adds the endpoint |
| `lib/api/members.ts` | `index`, `update`, `destroy` | unchanged (already follows the convention) |
| `lib/routes.ts` | `projects(ws)`, `project(ws, id)` | `projects.index(ws)`, `projects.show(ws, id)` |
| `lib/routes.ts` | `workspaceSettings(ws)` | `workspaces.edit(ws)` |
| `lib/routes.ts` | `members(ws)` | `members.index(ws)` |
| `pages/app/[workspace]/projects.tsx` | `Projects` | `ProjectIndex` |
| `pages/app/[workspace]/projects/[project].tsx` | `ProjectPage` | `ProjectShow` |
| `pages/app/[workspace]/settings.tsx` | `WorkspaceSettings` | `WorkspaceEdit` |
| `pages/app/[workspace]/settings/members.tsx` | `WorkspaceMembers` | `MemberIndex` |
| every caller of the above | | follows (the type checker finds them) |

The other pages (`Home`, `Login`, `Register`, `ForgotPassword`, `ResetPassword`, `Onboarding`, `WorkspaceHome`, `AccountSettings`, `Dashboard`, `AppLayout`, `NotFoundPage`) aren't resource pages and keep their names.

This is a rename only. There are no behaviour, response, SQL or URL changes, apart from the two `PUT` → `PATCH` changes.

## Steps

1. Go handlers and `routes.go`, then the store methods. Use `gopls rename` or the editor's rename, so tests and doc comments follow.
2. Password routes `PUT` → `PATCH`, including the client calls in `lib/api/auth.ts` and the tests.
3. Web client methods, page route constants, page default exports.
4. `grep -rn` for every old name across `apps/`, `docs/` and the open (not yet implemented) plans, and update any mention. Implemented plans are a historical record and keep the old names.
5. Docs (below).
6. Run `gofmt`, `go vet`, `make test`, `make test-integration`, `make check`, `make build`, and `make test-e2e` against `make serve`.

## Security

No boundary changes. While renaming, check that each route keeps its middleware: `requireSession`, `requireWorkspace`, `ws.Can` with the same permission, and `limitAuth` on the password routes. The existing integration tests (401, 403, 404, and the role × route matrix) are the guard. They must pass unchanged, apart from the method of the two password routes. `FindWorkspace` must keep the same query: scoped by membership, with the same 404 for non-members.

## Tests

- No new behaviour, so no new tests. All the commands in step 6 must pass.
- The two password routes' tests change method, and a `PUT` to either now returns 405.

## Docs

- **`AGENTS.md`:** rule 4 (`api.projects(workspace.id).create()` → `.store()`, and the route constants), plus a line linking this convention.
- **`docs/api/endpoints.md`:** the HTTP convention, and "Adding an endpoint" step 1 names handlers by action. The password routes become `PATCH`.
- **`docs/api/README.md`:** the request flow names `projectHandlers`.
- **`docs/api/database.md`:** the store naming table, under queries, so new code follows it.
- **`docs/ui/api.md` and `docs/ui/pages.md`:** client method names, page route groups, default export names, delete as a dialog.
- **`docs/naming.md`** already documents the whole convention (added 2026-10-31, before the rename, so new code follows it). After the rename, remove its note that some existing code still has older names.

## Decisions (confirmed 2026-10-31)

| # | Question | Decision |
| --- | --- | --- |
| 1 | Go handler shape | A per-resource type with `index/show/store/update/destroy` methods (`projects.index`). |
| 2 | Frontend vocabulary | Page routes use `index/show/create/edit`. The API client uses the API's `index/show/store/update/destroy`. Delete is a confirm dialog, not a page. |
| 3 | `PUT /api/auth/password` and `PUT /api/auth/password-reset` | Both become `PATCH`, so one method means update everywhere. |
| 4 | Store names | Verb first: `Index`, `Find`, `Create`/`Update`/`Delete`, domain verbs, `Check`. The five noun-only methods are renamed, and interface methods are the exception. |
| 5 | URLs | Unchanged (`/settings`, not `/edit`). Only the constants are renamed. |
| 6 | `/api/auth/*` | Stays as named actions, not resources. |
| 7 | When | One commit, right after the members work and before any other feature. |

## Not included

- New endpoints. Project, workspace and account deletion are in [deletion](deletion.md).
- Renaming the `events.Queue` methods.
- Generated route names, or a route-name → URL helper in Go (Laravel's `route('projects.show')`). Go never builds API URLs.
- Renaming page files to `index.tsx` / `show.tsx`.

## As built

Built as planned. Small differences:

- `pages/app/[workspace]/settings/members.tsx` already had a local `MemberIndex` (the list under the tabs). It became `MemberList`, so the default export could be `MemberIndex`.
- `membershipHandlers` lives in `httpapi/members.go` next to `memberHandlers`, rather than in its own file: it's one handler, and it shares `failMembership`.
- The client's `Method` type dropped `PUT`, since nothing sends it any more. `TestChangePassword` checks that `PUT` on both password routes is 405.
- `docs/api/database.md` gained the store naming table; `docs/ui/api.md` lists `members(workspace)` and `workspaces().leave(id)`; `docs/naming.md` no longer says some code has older names.

Docs updated: [naming.md](../../docs/naming.md), [endpoints.md](../../docs/api/endpoints.md), [auth.md](../../docs/api/auth.md), [api/README.md](../../docs/api/README.md), [database.md](../../docs/api/database.md), [ui/api.md](../../docs/ui/api.md), [ui/pages.md](../../docs/ui/pages.md), [ui/README.md](../../docs/ui/README.md), [ui/testing.md](../../docs/ui/testing.md) and AGENTS.md rule 4.
