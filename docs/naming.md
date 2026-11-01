# Naming

How things are named across the API and the web app. **New code follows this page**, and existing code matches it ([`plans/features/implemented/crud-routing-convention.md`](../plans/features/implemented/crud-routing-convention.md) renamed it).

Every rule here is checked by a test or a script; see [checks.md](checks.md) for which, how to read a failure, and what to do when a name is new.

The vocabulary is Laravel's resource controller, used at the HTTP boundary and in the web app. The store uses data verbs.

## Controllers (`internal/httpapi`)

| Action | Method | Path | Does | Controller method |
| --- | --- | --- | --- | --- |
| `index` | GET | `/things` | one page of the list | `Things.Index` |
| `show` | GET | `/things/{thing}` | one record | `Things.Show` |
| `store` | POST | `/things` | create, 201 with the record | `Things.Store` |
| `update` | PATCH | `/things/{thing}` | change, 200 with the record | `Things.Update` |
| `destroy` | DELETE | `/things/{thing}` | delete, 204 | `Things.Destroy` |

- **One package per area, under `httpapi/endpoints/`.** An area is the URL's first segment after `/api`, singular: `workspace` (`/api/workspaces/*`, and `/api/invitations/*` with the other invitation routes) and `admin` (`/api/admin/*`). `/api/auth/*` is the `account` area, so it doesn't clash with `internal/auth`; `/api/email-blocks/*` (`account.EmailBlocks`) lives there too, with the other signed-out links, rather than in an area of its own for three routes, and so does `/api/client-errors` (`account.ClientErrors`, the signed-in browser's own errors), one route.
- **One controller per file, named after its resource:** `type Projects struct{ *httpapi.Server }` in `endpoints/workspace/projects.go`, `WorkspaceProjects` in `workspace_projects.go`. The file holds the controller, its input types and its validation. An area exports only its controllers.
- **Its methods are the actions, exported**, each a `func(http.ResponseWriter, *http.Request)`. Helpers stay unexported:

  ```go
  // endpoints/workspace/projects.go
  type Projects struct{ *httpapi.Server }
  func (h Projects) Index(w http.ResponseWriter, r *http.Request) { … }

  // router/routes.go
  projects := workspace.Projects{Server: s}
  ws.HandleFunc("GET", "/projects", projects.Index)
  ws.Can("POST", "/projects", auth.ProjectStore, s.Limit(httpapi.ProjectPerUser, projects.Store))
  ```

- **Every route is registered in `router/routes.go`**, the one registry, like Laravel's `routes/api.php`.
- **A resource gets its own folder only when it has sub-resources:** if projects gain tasks and comments, `endpoints/workspace/project/` holds `Projects`, `Tasks` and `Comments`. Until then, an area is one folder.
- **Areas never import each other.** What two areas share moves into the core (`httpapi`) or a leaf package (`input`, `ratelimit`, `clientip`). Only the router imports areas.
- `TestHTTPLayout` (`httpapi/layout_test.go`) and `TestRouteNames` ([checks.md](checks.md)) check all of this, with the named actions and the support folders listed in it with their reasons.
- **Paths** are plural nouns under their parent (`/api/workspaces/{workspace}/projects/{project}`), and path IDs are public IDs.
- **`update` is always PATCH**, even when the body is the whole editable representation. PUT isn't used.
- **A singleton resource** (one per parent, no ID in the path) uses the same actions: leaving a workspace is `Membership.Destroy` on `DELETE …/membership`.
- **Anything else is a named verb**, as POST on the record or collection, with the verb as the method name: `POST /api/invitations/accept` → `Invitations.Accept`. First check whether it's really a `store` or `destroy` of some other resource.
- **`/api/auth/*` is the exception:** named actions on the session and password (`register`, `register/verify`, `login`, `logout`, `password/verify`, `password-reset/check`), not resources (`account.Sessions.Login`, `account.Registrations.Verify`, `account.PasswordResets.Check`); the account's own singletons there use the CRUD actions (`email-verification`: `EmailVerification.Store` sends a code, `.Update` verifies). The admin door's `/api/admin/login`, `/login/mfa` and `/logout` follow it (`admin.Sessions`); its resources are ordinary (`admin.Users.Index`, `.Show`; `IndexAdminUsers`, `FindAdminWorkspace`; `admin.EmailBlocks.Index`, `.Destroy`), and an admin session's step-up window is its singleton (`POST /api/admin/step-up`: `admin.StepUp.Store`, `StepUpAdminSession`). The admin actions act on a target's sub-resource with its CRUD action (`DELETE /api/admin/users/{user}/sessions`: `admin.UserSessions.Destroy`; an account's suspension is a singleton, `POST`/`DELETE …/suspension`: `admin.UserSuspension.Store`, `.Destroy`), and queuing failed deliveries again is the named action `Retry` (`admin.Deliveries.Retry`, `admin.FailedDeliveries.Retry`; the store's verb is `Requeue`, since the worker's `Retry` is `events.Queue`'s).

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
- `TestStoreMethodNames` checks the verbs (the domain verbs are listed in it, each with its reason) and that `Index…` returns a list ([checks.md](checks.md)).
- **The exception:** methods implementing an interface defined elsewhere use its names. That means the event worker's `Claim`, `Delivered`, `Fail` and `Retry` for `events.Queue`.

## Permissions (`internal/auth/permissions.go`)

`<resource>.<action>`, where the action is the CRUD action of the route the permission guards: `workspace.update`, `project.store`, `workspace.member.destroy`, `workspace.invitation.store`. The Go constant is the same words in PascalCase (`auth.WorkspaceMemberDestroy`). `TestPermissionNames` checks both ([checks.md](checks.md)).

The admin dashboard has roles instead (`auth.AdminRole`: `viewer`, `operator`, least access first); an admin route that changes anything names the one it needs with `Role` in `router/routes.go`, and an account-and-access action is registered with `Action` instead, which adds the operator role, the step-up and `admin-action:per-user` (`Role` with `operator` is refused for anything but the step-up). A housekeeping action (no account, address or delivery touched) is registered with `Housekeeping`, the operator role and the limit without a code, and only for the routes listed in `housekeepingRoutes`.

## Events (`internal/events`)

`<resource>.<fact>`: `workspace.invitation.created`, `workspace.member.removed`, `auth.password_reset.requested`, `project.updated`, `auth.logged_in`. Segments are snake_case.

- **The resource** is what the event is about, spelled as in its permission: a top-level resource is one segment (`project`, `workspace`), and a resource inside another adds one (`workspace.member`, `workspace.invitation`, `auth.password_reset`). The permission `workspace.member.update` guards the route that records `workspace.member.updated`.
- **The fact** is the last segment: past tense, a fact rather than a command (`created`, `updated`, `removed`, `left`, `sent`). Everything before it is what the fact is about, so filtering by resource is a prefix (`workspace.member.%`).
- **The account and its session** are the `auth` area. Facts about them as a whole stay one segment under `auth` (`auth.registered`, `auth.logged_in`, `auth.login_attempted`, `auth.profile_updated`, `auth.password_changed`, `auth.email_verified`); only stored sub-resources (reset links, pending sign-ups, email codes, members, invitations) get a segment (`auth.registration.requested`, `auth.email_verification.sent`). A sign-up waiting for its code is a *registration*, after the `register` route; the table is `pending_registrations`.
- **Email addresses on their own**, apart from any account, are the `email` area: `email.blocked`, `email.unblocked` (the block list).
- **Server processes** are the `server` area: `server.started` and `server.stopped`, one per start and graceful stop of a server ([observability.md](api/observability.md#servers-and-versions)).
- **The admin dashboard** is the `admin` area: facts about admins and their sessions as a whole are one segment (`admin.granted`, `admin.logged_in`, `admin.sessions_revoked`, `admin.access_refused`); a fact about one stored admin session names it, as the `auth` area's sub-resources do (`admin.session.moved`); what an admin read gets the resource read (`admin.user.viewed`, `admin.workspace.listed`, `admin.admin.listed`, `admin.email_block.listed`); and an admin's action names what it acted on and the fact (`admin.email.unblocked`, `admin.user.suspended`, `admin.ip.blocked`, `admin.delivery.retried`, `admin.admin.signed_out`), beside the store's own event for the change (`email.unblocked`, `auth.suspended`, `workspace.invitation.revoked`). What it acted on is listed in `TestEventResources`' stored resources.
- **At most three segments**, enforced by the `events.name` CHECK and `TestCatalogueNames`; `TestEventResources` checks a three-segment event's resource against the permissions and its list of stored resources ([checks.md](checks.md)). A deeper resource needs this rule revisited, not a fourth dot.
- **CRUD actions:** `store` records `<resource>.created`; `update` records `<resource>.updated`, or a more specific fact (`workspace.renamed`); `destroy` records `<resource>.deleted`, or a more specific fact (`workspace.member.removed`, `workspace.member.left`, `workspace.invitation.revoked`).
- **One event type per fact**, with variable details in the payload, never in the name (`rate_limit.reached` with a `limit` field, not one event per limit).
- **Listener names** (`mail.password_reset`) name listeners, not events; they are stored with each delivery and don't follow event renames.

## Rate limits (`httpapi/limits.go`)

`<feature>:<counted by>`: `auth:per-ip`, `auth:per-account`, `project:per-user`, `password-reset:global`. Counted by is `per-ip`, `per-account`, `per-email`, `per-user` or `global` (a limit shared by everyone); `TestRateLimitNames` checks it. In `rate_limit.reached`, the level reached follows the name: `register:global:block`. Every limit has only `block` for now; the CAPTCHA plan adds `:challenge`. The declarations are grouped by feature ([Lists of declarations](#lists-of-declarations)).

## Web app

| What | Names | Examples |
| --- | --- | --- |
| API client (`lib/api/<resource>.ts`), calls and typed paths | the API's actions, plus named verbs | `api.projects(ws).index()`, `.show(id)`, `.store(input)`, `.update(id, input)`, `.destroy(id)`; `paths.projects(ws).store()`; `api.invitations().accept(token)` |
| Page route constants (`lib/routes.ts`) | grouped by resource, with the page actions `index`, `show`, `create`, `edit` | `routes.projects.index(ws)`, `routes.projects.show(ws, prj)`, `routes.workspaces.edit(ws)`, `routes.members.index(ws)` |
| Pages that aren't a resource | plain names | `routes.home`, `routes.login`, `routes.settings`, `routes.dashboard(ws)` |
| Resource pages' default exports | `<Resource><Action>` | `ProjectIndex`, `ProjectShow`, `WorkspaceEdit`, `MemberIndex`; the admin dashboard's add `Admin` (`AdminUserIndex`, `AdminWorkspaceShow`), its route constants are `adminRoutes` in `lib/admin-routes.ts` and take the key (`adminRoutes.users.show(key, usr)`), and its API paths are `adminPaths` in `lib/api/admin.ts` |
| Other pages' default exports | what the page is | `Login`, `Onboarding`, `AccountSettings`, `Dashboard` |

- **Delete is never a page.** It is a confirmation dialog that calls `destroy`.
- **URLs read naturally and don't follow the action names:** the workspace `edit` page is `/app/<wsp>/settings`.
- **Page files are shaped like the URL** (`pages/app/[workspace]/projects/[project].tsx`).
- **Component files** are kebab-case, grouping a family of named exports (`button.tsx`, `confirmation-dialog.tsx`); see AGENTS.md rule 6.
- **Checked** ([checks.md](checks.md)): the API client's names, page route constants, resource pages' export names and kebab-case files.
- **Variants:** components with alternatives take a `variant` prop (`<Notice variant="error">`, `<ConfirmationDialog variant="danger">`), never `tone`, and never one component per alternative.

## Lists of declarations

A run of declarations of the same kind (the rate limits in `limits.go`, a `var (…)` or `const (…)` block, a table of routes) is grouped by what it's for, with **one blank line between groups** and none inside one. Each declaration's comment starts with its name and what it is, then any context on the lines after, as `//` comments:

```go
var (
	// AuthPerIP: everything that signs in or checks a password, per client IP.
	AuthPerIP = ratelimit.Limit{…}
	// AuthPerAccount: password guesses against one account, from any IP.
	AuthPerAccount = ratelimit.Limit{…}

	// RegisterPerEmail: sign-ups for one email, from any IP, with verification on.
	// Each one emails the address, a code or "you already have an account".
	RegisterPerEmail = ratelimit.Limit{…}
	// RegisterGlobal: accounts created by everyone.
	// Caps sign-ups during a distributed flood.
	RegisterGlobal = ratelimit.Limit{…}
)
```

- **Comments are `//`**, never `/* */`, like every other comment in the code.
- **A list that repeats the declarations** (`RateLimits`, beside them) follows the same groups and order, one line per group.
- **A new declaration joins its group;** a new kind of thing starts a group of its own, after a blank line.

