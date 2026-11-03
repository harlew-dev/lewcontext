# Grouping `internal/httpapi` by area

Status: **implemented 2026-10-31.** Every decision was confirmed ("what's best for Go" for the ones that were open); three more were made while building ([Built](#built-how-it-differs-from-the-plan)). Docs updated: [`docs/naming.md`](../../../docs/naming.md#controllers-internalhttpapi), [`docs/api/README.md`](../../../docs/api/README.md), [`docs/api/testing.md`](../../../docs/api/testing.md), and the names in `endpoints.md`, `auth.md`, `admin.md`, `rate-limits.md`, `pagination.md`, `events.md`, `database.md`, `docs/README.md`, `README.md` and AGENTS.md rule 5.

## Why

`apps/api/internal/httpapi` has 50 files in one folder: 23 handler and middleware files, 3 small helpers, 19 integration test files with their shared helpers, and 6 unit test files. Every new feature adds two or three more. The folder doesn't show what goes together, and it will keep growing.

The goal is a hierarchy like Laravel's controllers (`Http\Controllers\Workspace\ProjectController`): open `httpapi`, see one folder per area, and in each folder one file per resource's controller. This should be a rule that new features follow and a test enforces, not a one-off tidy-up. The web app already works this way (`pages/app/[workspace]/projects/…`). This brings the API in line with it.

## The constraint: a Go folder is a package

In Go, a subfolder is a separate package, and it sees only another package's exported (capitalised) names. Today every handler is a method on `*Server`, or on a `xxxHandlers struct{ *Server }`, and uses unexported helpers: `jsonResponse`, `fail`, `decodeValid`, `current(r)`, `currentWorkspace(r)`, `s.allow`, the rate limit declarations, and more (about 230 calls to the response helpers alone). A few handlers also call into another area:

| Caller | Uses | From |
| --- | --- | --- |
| workspace deletion | `passwordConfirmed` | `password.go` |
| admin sign-in | `secondFactor`, `recordMFACheck`, `beginMFASetup`, `confirmMFASetup`, `accountLimit`, `validCredentials`, `sessionDigest` | `mfa.go`, `auth.go`, `middleware.go` |
| admin workspaces | `projectFilter` | `projects.go` |
| members, admin users | `validSearch` | `projects.go` |
| invitations, admin events | `validEmail`, `validResetToken`, `failMembership` | `auth.go`, `password_reset.go`, `members.go` |

So area folders need two things:

- **A shared core below the areas.** It holds what today's `*Server` and its helpers provide.
- **Exported controller methods** (`Projects.Index`), because the route registry calls them from another package. Laravel's controllers have public methods too, so `projects.Index` reads like `ProjectController@index`.

The Go way to arrange that is the standard library's: **the parent package is the core, and its subpackages import it**, as `net/http/httptest` imports `net/http` and `image/png` imports `image`. A parent can't also import its children (Go refuses the cycle), so the registry, which imports every area, moves to its own small package, `httpapi/router`.

## Design

### The layout

```
internal/httpapi/              the core every controller uses (package httpapi)
  server.go  respond.go  context.go  middleware.go  admin.go  client_ip.go
  limits.go  sessions.go  credentials.go  second_factor.go  (+ their unit tests)

  account/      /api/auth/*: the account and its session
  workspace/    /api/workspaces/*, and /api/invitations/*
  admin/        /api/admin/*
  router/       routes.go: the one registry of routes and pages, like Laravel's routes/api.php

  apitest/      shared test helpers (imported only by tests)
  clientip/     resolving the client's address
  input/        query parameters, paging, public IDs, the search and token rules
  ratelimit/    what a limit is, its messages, the in-memory counter
```

The top level holds only the core. It doesn't grow with features: a new feature adds a file to its area, or a new area folder.

### Areas and controllers

- **An area is the URL's first segment after `/api`, singular:** `workspace` and `admin`. There are two exceptions:
  - `/api/auth/*` is the `account` area, because a package named `auth` would clash with `internal/auth`, which these controllers import. `docs/naming.md` already calls this area "the account and its session".
  - `/api/invitations/check` and `/accept` belong to `workspace`, with the other invitation routes.
- **One controller per file, named after its resource:** `type Projects struct{ *httpapi.Server }` in `workspace/projects.go`, `Membership` in `workspace/membership.go`. A file holds its controller, its input types and its validation, as today.
- **Methods are the CRUD actions, exported:** `Index`, `Show`, `Store`, `Update`, `Destroy`, or a named verb (`Accept`, `Check`). `/api/auth/*` keeps its named actions (`Login`, `Register`), as `docs/naming.md` already allows.
- **A resource gets its own folder only when it has sub-resources of its own.** For example, if projects gain tasks and comments, `workspace/project/` holds `Projects`, `Tasks` and `Comments`. Until then, an area stays one folder.
- **Areas never import each other.** Anything two areas share moves down into the core (or a leaf package). This is what keeps one area from growing into another, and the layout test checks it.

Controllers embed `*httpapi.Server`, today's type, so their dependencies don't change. Only the handlers move out of its package.

Today's handlers map onto controllers like this:

| Area | File | Controller | Methods | Today |
| --- | --- | --- | --- | --- |
| `account` | `registrations.go` | `Registrations` | `Register`, `Check`, `Resend`, `Verify` | `s.register`, `registrationHandlers` |
| | `sessions.go` | `Sessions` | `Login`, `LoginMFA`, `Logout`, `RevokeOthers` | `s.login`, `s.loginMFA`, `s.logout`, `s.revokeOthers` |
| | `me.go` | `Me` | `Show`, `Update`, `Destroy` | `s.me`, `s.updateProfile`, `s.destroyAccount` (`GET`/`PATCH`/`DELETE /api/auth/me`) |
| | `password.go` | `Password` | `Verify`, `Update` | `s.verifyPassword`, `s.changePassword` |
| | `password_resets.go` | `PasswordResets` | `Request`, `Check`, `Reset` | `s.requestPasswordReset`, `s.checkPasswordReset`, `s.resetPassword` |
| | `email_verification.go` | `EmailVerification` | `Store`, `Update` | `emailVerificationHandlers` |
| | `mfa.go` | `MFA` | `Setup`, `Store`, `Destroy` | `mfaHandlers` |
| | `recovery_codes.go` | `RecoveryCodes` | `Store` | `recoveryCodeHandlers` |
| `workspace` | `workspaces.go` | `Workspaces` | `Index`, `Show`, `Store`, `Update`, `Destroy` | `workspaceHandlers` |
| | `projects.go` | `Projects` | `Index`, `Show`, `Store`, `Update`, `Destroy` | `projectHandlers` |
| | `members.go` | `Members` | `Index`, `Update`, `Destroy` | `memberHandlers` |
| | `membership.go` | `Membership` | `Destroy` | `membershipHandlers` |
| | `invitations.go` | `Invitations` | `Index`, `Store`, `Destroy`, `Check`, `Accept` | `invitationHandlers` |
| `admin` | `sessions.go` | `Sessions` | `Login`, `LoginMFASetup`, `LoginMFA`, `Me`, `Logout` | `adminAuthHandlers` |
| | `users.go` | `Users` | `Index`, `Show` | `adminUserHandlers` |
| | `workspaces.go` | `Workspaces` | `Index`, `Show` | `adminWorkspaceHandlers` |
| | `workspace_projects.go` | `WorkspaceProjects` | `Index` | `adminWorkspaceProjectHandlers` |
| | `events.go` | `Events` | `Index`, `Show`, `Catalogue` | `adminEventHandlers` |
| | `mail.go` | `Mail` | `Show` | `adminMailHandlers` |
| | `deliveries.go` | `Deliveries` | `Index` | `adminDeliveryHandlers` |

The `account` method names are today's handler names, capitalised and without the type's name (`requestPasswordReset` → `PasswordResets.Request`). Renaming them to CRUD actions is a separate change (decision 10). The package name replaces today's `admin` prefix in type names (`adminUserHandlers` → `admin.Users`).

### The registry: `httpapi/router`

`routes.go` moves to `router/routes.go`, keeping every route and its middleware chain line for line, with only the names changed. `Server.Routes()` becomes `router.New(s)`. The health check, the one handler with no area, is an invokable function there.

```go
// cmd/server
api, err := httpapi.New(repository, c)
api.Pages = pages
handler := router.New(api)

// router/routes.go
projects := workspace.Projects{s}
ws.HandleFunc("GET", "/projects", projects.Index)
ws.Can("POST", "/projects", auth.ProjectStore, s.Limit(httpapi.ProjectPerUser, projects.Store))
```

`group`, `routeGroup` and `Can` move with it. The local `admin := group(…)` becomes `signedIn`, so it doesn't hide the `admin` package. No import needs an alias.

### The core: package `httpapi`

What stays at the top is today's non-handler code, exported where the areas or the router need it, plus the cross-area pieces from the table above:

| File | Holds |
| --- | --- |
| `server.go` | `Server` (the store, config, sealing keys, dummy hash, hashing slots, limit counters), `New`, `WebPages` |
| `respond.go` | `JSON`, `Fail`, `FailField`, `InternalError`, `Decode`, `DecodeValid` |
| `context.go` | `Session(r)`, `Workspace(r)`, `Admin(r)` and their `Optional…` forms: what the middleware put on the request |
| `middleware.go` | `Security`, `RequireSession`, `RequireSessionUnmetered`, `AllowUnverified`, `RequireWorkspace`, `RequirePermission`, `LimitAuth`, `RequirePageSession` |
| `admin.go` | `RequireAdminKey`, `RequireAdminSession`, `AdminKeyHash`, the admin pages' gate |
| `client_ip.go` | the `ClientAddress` middleware and `ClientIP(r)` |
| `limits.go` | the rate limit declarations (grouped, with `rateLimits` and `TestRateLimitNames`), `Allow`, `Limit`, `Meter`, `AccountLimit` |
| `sessions.go` | `StartSession`, `SessionDigest`, `SessionToken`, `ClearSessionCookie` |
| `credentials.go` | `ValidCredentials`, `ValidEmail`, `StrongPassword`, `PasswordInBounds`, `PasswordConfirmed` |
| `second_factor.go` | `SecondFactor`, `RecordMFACheck`, `BeginMFASetup`, `ConfirmMFASetup`, the MFA challenge cookie |

Stateless helpers are package functions, like `http.Error`: `httpapi.Fail(w, 404, "Project not found.")`, `httpapi.Workspace(r)`. Anything that needs the store or config is a method on `*Server` (`h.Allow(w, r, httpapi.InvitationPerUser, key)`, `h.PasswordConfirmed(…)`), as today's `s.allow`.

`httpapi` imports none of its subpackages except the leaves. The context keys stay unexported, so only its middleware can put a session or workspace on a request. Controllers can read them but never set them.

### Leaf packages

| Package | From | Holds |
| --- | --- | --- |
| `clientip` | the pure part of `client_ip.go`, `client_ip_test.go` | `Resolve(r, header, proxies)`, the `X-Forwarded-For` walk, trusting and normalising addresses |
| `ratelimit` | `memlimit.go`, the `rateLimit` type and its messages from `limits.go`, their unit tests | `Limit` (name, max, window), `Key`, `Refusal`, `RetryAfter`, `WaitFor`, the in-memory `Memory` counter |
| `input` | `pagination.go`, `ids.go`, `validSearch`, `boundedInt`, `projectFilter`, `validResetToken`, their tests | `Query(r, allowed…)`, `Page`, `PublicID`, `Search`, `Token` (an emailed link token's shape), `ProjectFilter` |

`input` names what the package checks: client input. It reads well at the call site (`input.PublicID("prj", id)`), and it doesn't clash with the `params` local variable that 10 handler files use. `failMembership` moves into the core.

### Tests

- **Integration tests sit beside the controllers they cover**, as is usual in Go: `workspace/projects_integration_test.go` in the external package `workspace_test`. An external test package can import `router` and build the real server, even though `router` imports `workspace`. They keep the `_integration` suffix, as `store/deliveries_integration_test.go` does, because they sit beside unit tests.
- **Shared helpers live in `apitest`:**
  - `Database(t)`: a migrated database, with the second-migrate no-op check;
  - the request and setup helpers `Call`, `Send`, `SignedIn` and `Must`;
  - the lookup helpers `IDOf`, `EmailOf`, `OwnedWorkspace` and `WorkspacePath`;
  - the event checks `EventsNamed` and `OnlyEvent`.

  `apitest` imports neither `httpapi` nor `router`, so tests inside the core can use it without an import cycle. Each test package has a short `server(t)` that calls `httpapi.New` and `router.New`. Helpers used by one area stay in it (`inbox`, `adminSignedIn`, `behindProxy`).
- **Tests that need internals stay with the internals:**
  - The router's tests keep `TestWorkspacePermissionMatrix` (it checks the matrix against the real `workspaceRoutes`), `TestWorkspaceMutationsNeedAPermission`, `TestAdminRoutesStayInTheAdminGroup` and `TestOnlyFiveRoutesAllowUnverified`.
  - The core's tests keep the two in-memory budget tests. They now send requests through the middleware directly (`s.RequireSession(ok)`, `s.RequireAdminSession(ok)`) rather than through the router, which the core can't import. The router tests keep covering which routes are metered.
- `s.keys` in the verification tests becomes `auth.NewKeys(s.Config.AppKey)`, and `validPublicID` becomes `input.PublicID`.

### Enforcing the layout: `TestHTTPLayout`

A test in the core package (`layout_test.go`). It parses the source with `go/parser` (no database), in the shape the [convention checks plan](convention-checks.md) uses: a pure function with a table test, run on the real tree, with failure messages that point to `docs/naming.md`. It fails when:

1. A controller (a type embedding `*httpapi.Server`) is outside an area package.
2. A folder isn't an area and isn't one of the listed support packages (`router`, `apitest`, `clientip`, `input`, `ratelimit`), each listed with its reason. A new area is just a new folder.
3. An area imports another area, or the core or a leaf imports an area or `router`.
4. An exported type in an area isn't a controller, or isn't in the file named after it in snake_case (`WorkspaceProjects` in `workspace_projects.go`).
5. A controller's exported method isn't `func(http.ResponseWriter, *http.Request)`, or isn't a CRUD action or a listed named verb (the convention checks plan's `TestHandlerNames`, moved here).
6. Any non-test package imports `apitest`.

## API and data changes

None. No route, response, table, migration, event or config changes. `cmd/server` calls `router.New(api)` where it called `api.Routes()`. Nothing else about startup changes.

## Security risks and how they're avoided

- **A route loses its guard in the move.** `router/routes.go` keeps every route and its middleware chain, line for line, with only the names changed. The permission matrix, `TestWorkspaceMutationsNeedAPermission`, `TestAdminRoutesStayInTheAdminGroup` and `TestOnlyFiveRoutesAllowUnverified` keep checking the real registry, with their source paths updated. Each is broken once on purpose after the move to see it still fails.
- **A controller sets a session or workspace itself.** The context keys stay unexported in the core, so only its middleware can set them.
- **An admin route escapes the key check.** Admin controllers only answer requests. Registration stays in `adminRoutes` behind `RequireAdminKey`, and its source test still reads that function.
- **A test silently dropped.** Record `go test -list '.*' ./internal/httpapi/...` before and after: the same test names must appear, apart from the package path. The same count must pass, and none may be skipped.
- **Test code in the server binary.** The layout test refuses a non-test import of `apitest`, and `go list -deps ./cmd/server` must not list it. Add it to the "`cmd/server` never imports" rule in `docs/api/README.md`.
- **A wider public surface.** Everything sits under `internal/`, so nothing outside `apps/api` can import it. `Server`'s fields stay unexported apart from `Store`, `Config` and `Pages` (as today); the keys and the dummy hash are reached only through its methods.
- **A behaviour change hidden in a move.** Each step is its own commit and changes only names, receivers and imports. Logic edits wait for a separate change.

## Tests

`make test` (integration tests included) and `make check` after each step. Before the first step and after the last, compare the `go test -list` names and run the deliberate guard failures above. `make build` once at the end, because `cmd/server`'s composition changes. No end-to-end run is needed: no behaviour changes.

## Docs to update

- `docs/naming.md`: "HTTP routes and handlers" becomes "Controllers": areas, one controller per file, exported CRUD methods, the registry example in the new form, when a resource gets its own folder, and areas never importing each other.
- `docs/api/README.md`: the packages table (`httpapi` and its folders), the request flow's names (`httpapi.RequireSession`, `workspace.Projects.Index`), and `cmd/server` never importing `apitest`.
- `docs/api/testing.md`: `apitest`, tests beside their controllers, which tests stay with the core or the router and why, and the new paths in its table.
- `docs/api/endpoints.md`, `auth.md`, `admin.md`, `rate-limits.md`, `pagination.md`, `events.md`, `docs/README.md` and `README.md`: the new file, handler and registry names.
- AGENTS.md rules 4 and 5: the registry is `apps/api/internal/httpapi/router/routes.go`, the `httpapi` folders, and the handler example (`projectHandlers` becomes `workspace.Projects`).
- [`convention-checks.md`](convention-checks.md): its `TestHandlerNames` and `TestRouteNames` rows change to the new shape, and point to `TestHTTPLayout`.

The implemented plans keep the old names. They record what was built at the time.

## Order

0. **After the work in progress in `httpapi` is committed.** On 2026-10-31 another session was changing 20 `httpapi` files (admin events and mail), plus `store`, `events` and the web app.
1. The leaf packages: `clientip`, `ratelimit`, `input` (`refactor:`).
2. The core exported, and `router/` split off. Handlers stay at the top for now and use the exported names (`refactor:`).
3. One commit per area, with its tests: `workspace`, then `account`, then `admin` (`refactor:`).
4. `apitest` finished, `TestHTTPLayout`, and the docs (`test:` and `docs:`).

The server builds and every test passes after each commit.

## Built: how it differs from the plan

Built on 2026-10-31 in one commit per step and per area: `refactor: httpapi leaf packages…`, `…export the httpapi core and split the registry…`, `…workspace endpoints…`, `…account endpoints…`, `…admin endpoints…`, then `TestHTTPLayout` and these docs. The same 117 test names ran and passed after each, plus `TestLayoutProblems` (the layout test's table test) at the end; every moved guard was broken once on purpose and failed.

Decided while building:

| # | Question | Decision |
| --- | --- | --- |
| 13 | A controller's `Store` action hides the embedded `Server.Store` field (Go's method and field share a namespace), so `h.Store.CreateProject(…)` stops compiling | **The field is `Server.Repo`**: `h.Repo.CreateProject(…)`. Only a rename. Grouping the store by resource (`h.Projects.Create(…)`) was raised and left for a plan of its own |
| 14 | Where the area folders sit | **Under `httpapi/endpoints/`**: `endpoints/account`, `endpoints/workspace`, `endpoints/admin`. The top level holds only the core and the support packages, and `TestHTTPLayout` treats every folder under `endpoints/` as an area. `routes/` was considered and refused: `router/routes.go` is where routes are declared |
| 15 | `go vet` refuses unkeyed literals of another package's struct | **`workspace.Projects{Server: s}`** in the router, not `{s}` |

Other differences:

- **Exported only where another package needs it.** `meter` and `sessionToken` stay unexported (only the core uses them). The keys and the dummy hash are reached through methods: `Server.DummyHash()`, and `EmailCodeDigest`, `SecondFactor`, `NewRecoveryCodes`, `BeginMFASetup`, `ConfirmMFASetup` for the keys.
- **Helpers one area uses moved with it:** `failMembership` and `thousands` are the workspace area's (the table above listed `failMembership` as cross-area, but only members and invitations use it). The app's MFA challenge cookie stays in the core's `second_factor.go`, as planned.
- **The per-IP key takes the address:** `l.IPKey(httpapi.ClientIP(r))`, and the /64 rule (`subject`, `TestLimitSubject`) is `ratelimit`'s. `TestRetryAfterAndWaitFor` checks the refusal messages with limits of its own instead of the declared ones; the declared messages are covered by the integration tests.
- **`apitest` holds more than listed:** besides `Database`, `Config`, `Call`, `Send`, `SignedIn`, `Must`, the lookups and the event checks, every helper two packages' tests share: `Mail` and `Mailbox` (the reset and invitation listeners; each package's `mailServer` wraps it with its `server(t)`), `WithMFA` and `StartMFALogin`, request bodies, `ErrorField`, `CookieNamed`, `Get` and `Pages`, `Reached`, `RetryWithin`, `CountRateLimits`, `PublicIDOf`, `CountRows`. Helpers that took a `*Server` take the DB.
- **Where the tests went:** tests of more than one area (`TestAuthenticationAndProjectIsolation`, events, limits, client IPs, the page gate) are `httpapi_test` in the core's folder. The account deletion tests stay with the workspace deletion tests in `endpoints/workspace`: they're about owning workspaces and share the members fixture. The admin page tests are the admin area's.
- **The core's files** are as planned: `server.go`, `respond.go`, `context.go`, `middleware.go`, `admin.go`, `client_ip.go`, `limits.go`, `sessions.go`, `credentials.go`, `second_factor.go`.
- **Open plans keep the old names** (`captcha-challenges.md`, the `admin/` plans, `email-blocks.md`): they describe code not written yet, and two of them were being edited elsewhere. Their implementers follow `docs/naming.md`.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | How far to split? | **Controllers in area folders**, Laravel-style, with one route registry. (This replaces the first draft's option B. B kept the handlers in one folder, which doesn't meet "one folder per area".) |
| 2 | The leaf packages (`clientip`, `ratelimit`, `input`) | **All three.** |
| 3 | The request parsing package's name | **`input`**: it names what's checked, and doesn't clash with the `params` local variable. |
| 4 | The shared test helpers' package name | **`apitest`**, after `httptest`. |
| 5 | Where the integration tests go | **Beside their controllers**, with the `_integration` suffix (`workspace/projects_integration_test.go`). This replaces the first draft's single `integration/` folder, chosen before area folders existed. |
| 6 | Tests that need internals | **They stay with the internals** (the core or the router), with no test hooks in production code. |
| 7 | When? | **After the uncommitted work is committed.** |
| 8 | Where the shared code goes | **The parent package `httpapi` is the core, with the registry in `httpapi/router`**: the standard library's layout (`net/http` and `net/http/httptest`). This replaces the earlier `base` subpackage, a name Go's style guides advise against (like `util` and `common`). |
| 9 | The `/api/auth` area's folder | **`account`**, so it doesn't clash with `internal/auth` and nothing needs an import alias. |
| 10 | Rename the `account` area's methods to CRUD actions? | **Not in this change.** Keep today's names capitalised, as `docs/naming.md` allows named actions under `/api/auth`. |
| 11 | Response helpers and request accessors: package functions or methods? | **Package functions** (`httpapi.Fail(w, …)`, like `http.Error`). Only what needs the server's dependencies is a method. |
| 12 | Group `internal/store` the same way? | **No.** See [Other folders](#other-folders). |

## Other folders

The same review, for every other source folder on 2026-10-31:

| Folder | Files | Verdict |
| --- | --- | --- |
| `apps/api/internal/store` | 24 | **Leave flat.** Every query shares the unexported transaction and event-recording helpers (`record`), and the models refer to one another. Splitting it would mean the same export churn as here, for a package that is one layer and grows by one file per table. Its files are already one per table or resource. |
| `apps/api/internal/{auth,config,events,factory,mail,…}` | 3–14 each | **Leave.** Each is one concern, already one file per topic. |
| `apps/web/src/lib` | 41 (24 source, 17 tests) | **The one other candidate.** About half its files are hooks (`use-*.ts`), next to session and workspace contexts, routing, and styling plumbing. TypeScript folders don't create packages, so grouping them costs only import paths. It's a separate change with its own plan, because its docs and conventions (AGENTS rule 4, `docs/ui/README.md`) are separate, and the session working now is also changing files there. |
| `apps/web/src/components/ui` | 41 | **Leave flat.** A primitive kit is flat by design: one file per family (`button.tsx`, `fieldset.tsx`), as AGENTS rule 6 sets out. `components/app/<feature>` already groups feature UI. |
| `apps/web/src/pages` | 38 | **Already grouped** by URL (`auth/`, `app/[workspace]/…`, `admin/`). |

## Not included

- Grouping `internal/store` or the web app's `lib` (see [Other folders](#other-folders)).
- Renaming routes, or changing any route's middleware, limits or responses.
- Renaming the `account` area's methods to CRUD actions (decision 10).
- Logic changes of any kind, including tidying what the move exposes. Those come after, as separate changes.
