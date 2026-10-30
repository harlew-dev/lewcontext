# Workspaces and public IDs

Status: **draft, 2026-10-30. Open questions are at the end. Don't write code until they're answered.**

## Goal

1. Every resource the API exposes gets a `public_id` such as `prj_j292s2S2j7Qx4B`. Requests, responses and page URLs use only that ID. The bigint `id` stays internal for primary keys, foreign keys and sort tie-breaks.
2. Projects belong to a **workspace**, not a user. Users reach workspaces through `workspace_user` membership. Workspace URLs look like `/app/wsp_2yEHYLwiwPVhLW/projects`.
3. The sidebar's workspace switcher, a placeholder today (`WorkspaceDropdown` in `application-layout.tsx`), lists the user's real workspaces by name.

## Schema

```
workspaces                         workspace_user
---                                ---
id          bigint PK              id            bigint PK
public_id   text UNIQUE NOT NULL   user_id       bigint → users ON DELETE CASCADE
name        text NOT NULL (1–80)   workspace_id  bigint → workspaces ON DELETE CASCADE
created_at  timestamptz            created_at    timestamptz
updated_at  timestamptz            UNIQUE (workspace_id, user_id)
                                   INDEX (user_id)

projects
---
+ public_id     text UNIQUE NOT NULL
+ workspace_id  bigint NOT NULL → workspaces ON DELETE CASCADE
  user_id       → see Q4 (drop, or keep as created_by)
  INDEX (workspace_id, updated_at DESC) replaces (user_id, updated_at DESC)
```

The `UNIQUE (workspace_id, user_id)` constraint isn't in your sketch. Without it, a user could be added to the same workspace twice.

### Public ID format

`<prefix>_<14 base62 chars>`: `wsp_2yEHYLwiwPVhLW`, `prj_j292s2S2j7Qx4B`. That's about 83 random bits, so IDs can't be guessed or enumerated. Access still depends on membership checks, not on the ID being secret.

| Table | Prefix |
| --- | --- |
| workspaces | `wsp_` |
| projects | `prj_` |
| users | `usr_` (Q2) |

- **Where they're generated (Q1):** recommended is a PostgreSQL column default, `DEFAULT public_id('prj')`, from a small SQL function built on `gen_random_bytes`/`gen_random_uuid` (core since PG 13). Then inserts from the API, factories, seeders and the migration backfill all get IDs from one generator, and Bun's `Returning("*")` reads them back. The alternative is a Go generator (`internal/publicid`, `crypto/rand`) called from a Bun `BeforeAppendModel` hook. That's easier to unit-test, but the backfill would still need SQL.
- A `CHECK (public_id ~ '^prj_[0-9A-Za-z]{14}$')` on each table, plus the same regex in Go (`httpapi`) and TS (`lib/ids.ts`). Malformed IDs get a 404 without a query.
- JSON: the `id` field becomes the public ID string (`"id": "prj_…"`), and the internal bigint is `json:"-"`. The web `Project.id` becomes `string`.

## Migrations (new files; applied ones stay untouched)

| File | What it does |
| --- | --- |
| `005_create_public_id_function` | `public_id(prefix text)` SQL function |
| `006_create_workspaces` | `workspaces` and `workspace_user` |
| `007_move_projects_to_workspaces` | Adds `projects.public_id` (default, backfilled); creates one workspace per existing user (Q3 name) with a membership; sets `projects.workspace_id` from the owner's workspace, then `NOT NULL`; swaps the index; handles `user_id` per Q4 |

The data steps run inside 007's transaction, so existing local data (including the demo account) moves over without a reset. The down migrations reverse it: they copy `workspace_id` back to `user_id` through the membership. Running them is destructive, so they're previewed only.

## API

### Routes

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/workspaces` | The user's workspaces (for the switcher). Not paginated? (Q6) |
| POST | `/api/workspaces` | Create one; the creator becomes a member (Q5) |
| GET | `/api/workspaces/{workspace}` | One workspace |
| PATCH | `/api/workspaces/{workspace}` | Rename (Q5) |
| GET | `/api/workspaces/{workspace}/projects` | Was `/api/projects`; same pagination, search and sort |
| POST | `/api/workspaces/{workspace}/projects` | |
| GET | `/api/workspaces/{workspace}/projects/{project}` | `{project}` is `prj_…` |
| PATCH | `/api/workspaces/{workspace}/projects/{project}` | |

`/api/auth/*` and `/api/auth/sessions` belong to the account, so they stay where they are. The old `/api/projects*` routes are removed, not aliased: the only client is this web app, and it changes in the same commit.

### Grouping

`net/http`'s `ServeMux` has no `Group()`. Mounting a sub-mux doesn't help either, because `http.StripPrefix` can't strip a prefix that contains `{workspace}`. The plan is a ~15-line helper in `routes.go` that prefixes patterns and wraps handlers in middleware. That keeps `routes.go` as the single registry, and every workspace route gets session and membership checks without anyone having to remember them:

```go
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// …health, auth as today…
	mux.Handle("GET /api/workspaces", s.requireSession(http.HandlerFunc(s.workspaces)))
	mux.Handle("POST /api/workspaces", s.requireSession(http.HandlerFunc(s.createWorkspace)))

	ws := group(mux, "/api/workspaces/{workspace}", s.requireSession, s.requireWorkspace)
	ws.HandleFunc("GET", "", s.workspace)
	ws.HandleFunc("PATCH", "", s.updateWorkspace)
	ws.HandleFunc("GET", "/projects", s.projects)
	ws.HandleFunc("POST", "/projects", s.createProject)
	ws.HandleFunc("GET", "/projects/{project}", s.project)
	ws.HandleFunc("PATCH", "/projects/{project}", s.updateProject)
	return s.security(mux)
}
```

### Authorization

- `requireWorkspace` (in `middleware.go`) runs after `requireSession`. It validates the `{workspace}` format, then looks the workspace up **joined to `workspace_user` on the session's user ID**. If the user isn't a member, or the workspace doesn't exist, the response is the same `404 Workspace not found.`, so the API doesn't reveal which workspaces exist. On success it puts the workspace in the request context (`currentWorkspace(r)`).
- Store project queries take the internal `workspaceID` from the context and the project's `publicID` from the path: `WHERE p.public_id = ? AND p.workspace_id = ?`. A `prj_` ID from workspace A requested under workspace B's URL is a 404.
- Request bodies never carry workspace or owner IDs.

### Store and Go changes

- `store`: `Workspace` model; `Workspaces(userID)`, `MemberWorkspace(userID, publicID)`, `CreateWorkspace(userID, name)` (a transaction that inserts the workspace and its membership), `RenameWorkspace`; project methods switch from `userID, id int64` to `workspaceID int64, publicID string`.
- `factory`: `Workspaces`, `WorkspaceUsers`, a `WorkspaceFor(userID)` helper, and `ProjectsIn(workspaceID)`. `TestEveryTableHasAFactory` checks the new tables are covered.
- `seed`: the demo account gets a workspace (Q3 name) holding the demo projects. `volume` adds projects to that account's first workspace. `users` creates a workspace for each fake user.
- Registration creates the user's first workspace and membership in the same transaction as the user (Q3). It still starts with no projects.

## Web

### Pages

```
src/pages/app/
  index.tsx                          → redirects to the default workspace (Q7)
  settings.tsx                       stays: account & security is per user (Q8)
  [workspace]/dashboard.tsx          was app/dashboard.tsx
  [workspace]/projects.tsx           was app/projects.tsx
  [workspace]/projects/[project].tsx was app/projects/[id].tsx
```

Next.js serves static segments (`/app/settings`) before dynamic ones (`/app/[workspace]`), and workspace IDs always start with `wsp_`, so the two can't collide.

- `appProps` reads `context.params.workspace`, loads the session and the workspace list, and finds the current workspace. If the user isn't a member it returns `notFound`. The page props gain `workspace` and `workspaces`, so the switcher renders on the server with no flash.
- `_app` adds a `WorkspaceProvider` beside `SessionProvider`, and pages and components read it with `useWorkspace()`.
- `/` and `/app` currently redirect to `/app/dashboard` in `next.config.mjs`. They have to go to the user's workspace now, which needs data, so they become an `app/index.tsx` `getServerSideProps` redirect. The old bookmarks (`/app/dashboard`, `/app/projects`, `/projects`, `/settings`) go there too. Old numeric project URLs (`/app/projects/42`) can't be mapped, so they 404 (Q9).

### Routes, paths and client

```ts
// lib/routes.ts
routes.dashboard(workspace)          // "/app/wsp_…/dashboard"
routes.projects(workspace)
routes.project(workspace, project)   // "/app/wsp_…/projects/prj_…"
routes.settings                      // unchanged
navigation(workspace)                // sidebar items for the current workspace

// lib/api: scoped by workspace (Q10)
paths.projects(workspace).index({ status: "Planned" })
paths.projects(workspace).show(project)
api.projects(workspace).create(input)
paths.workspaces.index()
api.workspaces().create({ name })
```

- `lib/ids.ts`: `parseId` becomes `parsePublicId("prj", value)`, using the same regex as Go.
- `returnPath` patterns become `/^\/app\/wsp_[0-9A-Za-z]{14}\/projects\/prj_[0-9A-Za-z]{14}$/` and so on. The existing test that every `/app` route has a return pattern still applies.
- SWR keys already include the user ID. Workspace paths include the workspace ID, so caches never mix workspaces. `paths.projects(ws).matches(key)` scopes the editor's cache invalidation to one workspace.
- The sidebar's current-item check compares against `router.pathname`, which would now be `/app/[workspace]/projects`. It switches to `router.asPath` without the query.

### Sidebar

`WorkspaceDropdown` shows the current workspace's name and initial. The menu lists every workspace, each a link to its dashboard with a check on the current one. "New workspace…" opens a create dialog (if Q5 includes creating). Settings stays in the account menu. Switching workspaces is a normal navigation, so `ApplicationLayout` stays mounted.

## Tests

- **Go integration:**
  - A non-member gets 404 on every `/api/workspaces/{ws}/…` route (GET, POST and PATCH).
  - A project reached through another of the user's own workspaces is a 404.
  - Malformed `wsp_`/`prj_` IDs are a 404 or 400 (Q11) without a query.
  - Registration creates exactly one workspace with no projects.
  - The 007 migration backfills an existing user's projects into their new workspace (up, then down in a temp schema).
  - `public_id` defaults are unique and match the format.
- **Vitest:** `parsePublicId`; `returnPath` accepts the new routes and rejects numeric IDs; the route builders; `paths.projects(ws).matches` doesn't match another workspace's keys.
- **Playwright:** register → land on `/app/wsp_…/dashboard`; create a project and open it at `/app/wsp_…/projects/prj_…`; the switcher shows the workspace name; another user's workspace URL is a 404.

## Docs to update

`docs/api/endpoints.md`, `api/database.md` (public IDs, workspace scoping replaces "scoped by `user_id`"), `api/seeding.md`, `api/testing.md`, `ui/pages.md`, `ui/api.md`, `docs/README.md` ("Adding a resource end to end": scope by workspace, add a public ID prefix), and AGENTS.md (rule 4 paths, rule 9 "new registrations start with one workspace and no projects", the auth invariant "scope SQL by the authenticated user's workspace membership").

## Suggested order

1. Public ID function + `projects.public_id`; switch project routes and the web to `prj_` IDs (still user-scoped). This works on its own and can ship separately.
2. Workspaces schema, the migration that moves projects, factories, seeds, `requireWorkspace`, the grouped routes, and the store changes.
3. Web pages under `[workspace]`, `WorkspaceProvider`, routes, paths and redirects.
4. Sidebar switcher (and create/rename if in scope).
5. Docs and AGENTS.md.

## Open questions

| # | Question | Suggestion |
| --- | --- | --- |
| 1 | Generate public IDs in PostgreSQL (column default) or in Go? | PostgreSQL: one generator covers the API, factories, seeds and the backfill |
| 2 | Do users get a `usr_` public ID too? `/api/auth/me` returns the numeric user ID today | Yes, for consistency. SWR keys would use it |
| 3 | Each new account's first workspace: created automatically at registration? What's it called? | Yes; "{first name}'s workspace" (so "Alex's workspace" for the demo user) |
| 4 | `projects.user_id` after the move: drop it, or keep it as `created_by` (shown as "Created by …")? | Keep it as nullable `created_by` (`ON DELETE SET NULL`); scoping uses only `workspace_id` |
| 5 | Workspace endpoints in this feature: list only, or also create and rename? | List + create + rename; leave delete, invites and roles for later |
| 6 | Paginate `GET /api/workspaces`? | No; return all (a user has few), capped at 100 per user on create |
| 7 | Which workspace do `/` and `/app` open: the oldest membership, or the last one used (remembered in a cookie or `users.last_workspace_id`)? | Oldest for now; last-used later |
| 8 | Account settings stays at `/app/settings` (no workspace in the URL)? Workspace settings (rename) at `/app/wsp_…/settings`? | Yes to both |
| 9 | Old numeric project URLs: 404, or redirect to the workspace's project list? | 404; this is a demo with no external links |
| 10 | Client shape: `api.projects(ws)` with an explicit workspace, or `useApi()` reading the current workspace from `useWorkspace()`? | Explicit `ws`: `paths` stays usable outside components and in `getServerSideProps` |
| 11 | Malformed IDs (`/projects/abc`): 400 like today's "Invalid project ID.", or 404? | 404: to a user it's just a URL that doesn't exist |
| 12 | Membership roles (owner/member) now, or later with invites? Your schema has no `role` column | Later; every member can do everything for now |
| 13 | Ship step 1 (`prj_` IDs) as its own change before workspaces? | Yes; it's smaller and easier to review |
