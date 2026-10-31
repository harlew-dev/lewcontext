# Naming

How things are named across the API and the web app. **New code follows this page**, and existing code matches it ([`plans/features/implemented/crud-routing-convention.md`](../plans/features/implemented/crud-routing-convention.md) renamed it).

The vocabulary is Laravel's resource controller, used at the HTTP boundary and in the web app. The store uses data verbs.

## HTTP routes and handlers (`internal/httpapi`)

| Action | Method | Path | Does | Handler |
| --- | --- | --- | --- | --- |
| `index` | GET | `/things` | one page of the list | `things.index` |
| `show` | GET | `/things/{thing}` | one record | `things.show` |
| `store` | POST | `/things` | create, 201 with the record | `things.store` |
| `update` | PATCH | `/things/{thing}` | change, 200 with the record | `things.update` |
| `destroy` | DELETE | `/things/{thing}` | delete, 204 | `things.destroy` |

- **Handlers for one resource are methods on a per-resource type**, in that resource's file:

  ```go
  type projectHandlers struct{ *Server }
  func (h projectHandlers) index(w http.ResponseWriter, r *http.Request) { … }

  // routes.go
  projects := projectHandlers{s}
  ws.HandleFunc("GET", "/projects", projects.index)
  ws.Can("POST", "/projects", auth.ProjectStore, projects.store)
  ```

- **Paths** are plural nouns under their parent (`/api/workspaces/{workspace}/projects/{project}`), and path IDs are public IDs.
- **`update` is always PATCH**, even when the body is the whole editable representation. PUT isn't used.
- **A singleton resource** (one per parent, no ID in the path) uses the same actions: leaving a workspace is `membership.destroy` on `DELETE …/membership`.
- **Anything else is a named verb**, as POST on the record or collection, with the verb as the handler name: `POST /api/invitations/accept` → `invitations.accept`. First check whether it's really a `store` or `destroy` of some other resource.
- **`/api/auth/*` is the exception:** named actions on the session and password (`register`, `login`, `logout`, `password/verify`, `password-reset/check`), not resources.

## Store methods (`internal/store`)

Every method starts with a verb:

| Verb | For | Examples |
| --- | --- | --- |
| `Index<Resources>` | one page of a list, search and filters included (a filter is a field on the query, not a new method) | `IndexProjects`, `IndexMembers`, `IndexWorkspaces` |
| `Find<Resource>` | one record; `sql.ErrNoRows` when it's missing or out of scope | `FindUser`, `FindProject`, `FindWorkspace` |
| `Create` / `Update` / `Delete<Resource>` | plain writes | `CreateProject`, `UpdateProject`, `DeleteProject` |
| a domain verb | when it says more than create, update or delete | `RenameWorkspace`, `UpdateMemberRole`, `RemoveMember`, `LeaveWorkspace`, `IssuePasswordReset`, `AcceptInvitation`, `RevokeInvitation`, `Allow` |
| `Check<Thing>` | a yes/no question about a record | `CheckPasswordReset` |

- There are no noun-only methods (`Project`, `Session`).
- **The exception:** methods implementing an interface defined elsewhere use its names. That means the event worker's `Claim`, `Delivered`, `Fail` and `Retry` for `events.Queue`.

## Permissions (`internal/auth/permissions.go`)

`<resource>.<action>`, where the action is the CRUD action of the route the permission guards: `workspace.update`, `project.store`, `workspace.member.destroy`, `workspace.invitation.store`. The Go constant is the same words in PascalCase (`auth.WorkspaceMemberDestroy`).

## Events (`internal/events`)

`<resource>.<fact>`: `workspace.invitation.created`, `workspace.member.removed`, `auth.password_reset.requested`, `project.updated`, `auth.logged_in`. Segments are snake_case.

- **The resource** is what the event is about, spelled as in its permission: a top-level resource is one segment (`project`, `workspace`), and a resource inside another adds one (`workspace.member`, `workspace.invitation`, `auth.password_reset`). The permission `workspace.member.update` guards the route that records `workspace.member.updated`.
- **The fact** is the last segment: past tense, a fact rather than a command (`created`, `updated`, `removed`, `left`, `sent`). Everything before it is what the fact is about, so filtering by resource is a prefix (`workspace.member.%`).
- **The account and its session** are the `auth` area. Facts about them as a whole stay one segment under `auth` (`auth.registered`, `auth.logged_in`, `auth.login_attempted`, `auth.profile_updated`, `auth.password_changed`); only stored sub-resources (reset links, members, invitations) get a segment.
- **At most three segments**, enforced by the `events.name` CHECK and `TestCatalogueNames`. A deeper resource needs this rule revisited, not a fourth dot.
- **CRUD actions:** `store` records `<resource>.created`; `update` records `<resource>.updated`, or a more specific fact (`workspace.renamed`); `destroy` records `<resource>.deleted`, or a more specific fact (`workspace.member.removed`, `workspace.member.left`, `workspace.invitation.revoked`).
- **One event type per fact**, with variable details in the payload, never in the name (`rate_limit.reached` with a `limit` field, not one event per limit).
- **Listener names** (`mail.password_reset`) name listeners, not events; they are stored with each delivery and don't follow event renames.

## Rate limits (`httpapi/limits.go`)

`<feature>:<counted by>`: `auth:per-ip`, `auth:per-account`, `project:per-user`, `password-reset:global`. Counted by is `per-ip`, `per-account`, `per-email`, `per-user` or `global` (a limit shared by everyone); `TestRateLimitNames` checks it. In `rate_limit.reached`, the level reached follows the name: `register:global:block`. Every limit has only `block` for now; the CAPTCHA plan adds `:challenge`.

## Web app

| What | Names | Examples |
| --- | --- | --- |
| API client (`lib/api/<resource>.ts`), calls and typed paths | the API's actions, plus named verbs | `api.projects(ws).index()`, `.show(id)`, `.store(input)`, `.update(id, input)`, `.destroy(id)`; `paths.projects(ws).store()`; `api.invitations().accept(token)` |
| Page route constants (`lib/routes.ts`) | grouped by resource, with the page actions `index`, `show`, `create`, `edit` | `routes.projects.index(ws)`, `routes.projects.show(ws, prj)`, `routes.workspaces.edit(ws)`, `routes.members.index(ws)` |
| Pages that aren't a resource | plain names | `routes.home`, `routes.login`, `routes.settings`, `routes.dashboard(ws)` |
| Resource pages' default exports | `<Resource><Action>` | `ProjectIndex`, `ProjectShow`, `WorkspaceEdit`, `MemberIndex` |
| Other pages' default exports | what the page is | `Login`, `Onboarding`, `AccountSettings`, `Dashboard` |

- **Delete is never a page.** It is a confirmation dialog that calls `destroy`.
- **URLs read naturally and don't follow the action names:** the workspace `edit` page is `/app/<wsp>/settings`.
- **Page files are shaped like the URL** (`pages/app/[workspace]/projects/[project].tsx`).
- **Component files** are kebab-case, grouping a family of named exports (`button.tsx`, `confirmation-dialog.tsx`); see AGENTS.md rule 6.
- **Variants:** components with alternatives take a `variant` prop (`<Notice variant="error">`, `<ConfirmationDialog variant="danger">`), never `tone`, and never one component per alternative.
