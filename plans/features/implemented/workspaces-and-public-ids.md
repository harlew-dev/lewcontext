# Workspaces and public IDs

Status: **implemented 2026-10-30.** Where the build differs from the design below:

- `appProps` also loads the URL's workspace with `GET /api/workspaces/{workspace}` (the list is paginated, so it may not be on page 1); pages get `load(api, workspace)`.
- Registration goes straight to `/app/onboarding` (`useAuthSubmit(register, routes.onboarding)`); `?next=` only applies to sign-in.
- `DropdownItem` gained `keepOpen` (for "Show more"/"Retry"), and `Dialog`/`Dropdown` now return focus to their opener themselves (`lib/return-focus.tsx`): BaseUI's `returnFocus` sent focus to "Skip to content", which then covered the switcher.
- `GET /api/workspaces` rejects `q`/`sort` (400) like any unknown parameter.

## Goal

1. Every table a user can open, link to or search gets a `public_id` such as `prj_j292s2S2j7Qx4B`. Requests, responses and page URLs use only that ID. The bigint `id` stays internal, for primary keys, foreign keys and sort tie-breaks, and never appears in a response.
2. Projects belong to a **workspace**. Users reach workspaces through `workspace_user` memberships, which have a role (`owner`, `admin`, `member`). Workspace URLs look like `/app/wsp_2yEHYLwiwPVhLW/projects`.
3. A new account creates its first workspace in an onboarding step. More can be created from the sidebar switcher, which lists the user's workspaces by name. Each workspace has a settings page.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Where public IDs are generated | PostgreSQL column default, `DEFAULT public_id('prj')`, from a plpgsql function on core `gen_random_uuid()` (no extension). See [How the ID function works](#how-the-id-function-works) |
| 2 | Which tables get one | Every table a user can open or search: `users` (`usr_`), `workspaces` (`wsp_`), `projects` (`prj_`). Internal bigint IDs never leave the API |
| 3 | First workspace | Not created at registration. An **onboarding** page in the auth-style centred layout: "New workspace", a name input, and a "Create workspace" button |
| 4 | `projects.user_id` | **Dropped.** Access comes only from `workspace_user`, which gains `role` (`owner` / `admin` / `member`) |
| 5 | Workspace features | Create (a dialog from the switcher) and a **workspace settings page** like account settings: a form to rename the workspace and its ID read-only with a copy button |
| 6 | Default workspace | The **most recently created** workspace the user belongs to (`workspaces.created_at DESC, id DESC`). Nothing is stamped on visits, and no column is added |
| 7 | Migrations | **Squashed while in development.** Edit the existing create migrations in place and rebuild the local database with a new, guarded `make migrate-fresh`. No add-column or data-move migrations. See [Migrations](#migrations-squashed-while-in-development) |
| 8 | Existing local data | Not migrated. Rebuild the database (`make migrate-fresh CONFIRM=1`), then `make seed` |
| 9 | Workspaces per user | No cap |
| 10 | Members and workspace settings | Members can open the settings page read-only: name, ID with copy, role |
| 11 | Old URLs | **Removed, with no redirects or aliases.** This covers the old `/api/projects*` routes, numeric project URLs, and the `/login`, `/projects`, `/settings`, `/app/dashboard`, `/app/projects` redirects in `next.config.mjs`. Every UI link and test uses the new routes |
| 12 | Delivery | One change: public IDs, workspaces, onboarding, the switcher and settings ship together |
| 13 | Migration numbering | Renumbered into dependency order: function, users, sessions, workspaces, projects, auth attempts |
| 14 | Fresh vs. seed | Separate steps: `make migrate-fresh CONFIRM=1 && make seed` |
| 15 | Legacy history adoption | Removed: `adoptLegacyHistory`, `legacyMigrations` and `TestMigrateAdoptsLegacyRunnerHistory` |
| 16 | Workspace list | **Always paginated** (`page`, `perPage`, `Page<T>`, like projects). The switcher scrolls infinitely, loading the next page only when there is one. See [Sidebar switcher](#sidebar-switcher) |
| 17 | Demo workspace | "Acme". The `users` seeder names fake workspaces with `gofakeit` company names |
| 18 | Client shape | Explicit workspace: `paths.projects(ws)…`, `api.projects(ws)…` |

## Schema

```
users                                workspaces
---                                  ---
+ public_id  text UNIQUE NOT NULL    id          bigint PK
                                     public_id   text UNIQUE NOT NULL DEFAULT public_id('wsp')
                                     name        text NOT NULL CHECK (length 1–80)
                                     created_at  timestamptz NOT NULL DEFAULT now()
                                     updated_at  timestamptz NOT NULL DEFAULT now()

workspace_user                       projects
---                                  ---
id            bigint PK              + public_id     text UNIQUE NOT NULL DEFAULT public_id('prj')
user_id       bigint → users         + workspace_id  bigint NOT NULL → workspaces ON DELETE CASCADE
              ON DELETE CASCADE      - user_id       (dropped)
workspace_id  bigint → workspaces      INDEX (workspace_id, updated_at DESC)
              ON DELETE CASCADE          replaces (user_id, updated_at DESC)
role          text NOT NULL CHECK (role IN ('owner','admin','member'))
created_at    timestamptz NOT NULL DEFAULT now()
UNIQUE (workspace_id, user_id)
INDEX (user_id)
```

Each `public_id` column also gets `CHECK (public_id ~ '^prj_[0-9A-Za-z]{14}$')`, with the prefix changed per table. `workspace_user` has no public ID: memberships aren't something a user opens or searches. When member management arrives, members are addressed by their `usr_` ID inside a workspace.

## Public IDs

Format: `<prefix>_<14 base62 characters>`, for example `wsp_2yEHYLwiwPVhLW`. That's 62¹⁴ ≈ 2⁸³ possibilities, so IDs can't be guessed or enumerated, and they don't reveal counts or creation order the way `/projects/42` does. Access still depends on membership checks; secrecy of the ID is not the protection.

### How the ID function works

PostgreSQL has had `gen_random_uuid()` built in since version 13 (this machine runs 17.11). It draws from the operating system's secure random source, the same as Go's `crypto/rand`. A UUID is 16 random bytes, apart from 6 fixed version/variant bits. The function turns those bytes into base62 characters:

```sql
CREATE FUNCTION public_id(prefix text) RETURNS text
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
  alphabet constant text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
  id text := '';
  bytes bytea;
  b int;
BEGIN
  WHILE length(id) < 14 LOOP
    bytes := uuid_send(gen_random_uuid());   -- 16 bytes
    FOR i IN 0..15 LOOP
      CONTINUE WHEN i IN (6, 8);             -- bytes holding the fixed version/variant bits
      b := get_byte(bytes, i);
      CONTINUE WHEN b >= 248;                -- 248 = 4 × 62, so every character is equally likely
      id := id || substr(alphabet, b % 62 + 1, 1);
      EXIT WHEN length(id) = 14;
    END LOOP;
  END LOOP;
  RETURN prefix || '_' || id;
END $$;
```

- **Bias:** a byte has 256 values, which isn't a multiple of 62. Plain `b % 62` would make the first 8 characters slightly more common. Dropping bytes 248–255 removes that bias; it discards about 3% of bytes and occasionally costs a second UUID.
- **As a column default:** `public_id text NOT NULL DEFAULT public_id('prj')`. Every insert gets an ID, whether it comes from the API, a factory or a seeder. Bun reads it back with `Returning("*")`, so Go never generates IDs.
- **Collisions:** `UNIQUE` is the backstop. At 10 million rows, the chance of any collision is about 1 in 10¹¹. A collision would surface as a 500 on one insert; no retry loop is planned.
- **Tested on the local database** (a temporary function in a rolled-back transaction, nothing kept): 200,000 IDs were all distinct and all matched `^wsp_[0-9A-Za-z]{14}$`. Generating 100,000 took 5.8 s, about 58 µs each, which is negligible next to the insert itself.
- Go (`httpapi`) and TS (`lib/ids.ts`) check the same regex before querying, so a malformed ID gets a 404 without a database round trip.
- In JSON, the `id` field becomes the public ID (`"id": "prj_…"`), and the internal bigint is tagged `json:"-"`. In the web types, `User.id`, `Workspace.id` and `Project.id` become `string`.

## Roles

There are no invites yet, so every workspace has one member, its owner. Roles are stored now so later features can check them without another migration.

| Action | owner | admin | member |
| --- | --- | --- | --- |
| View the workspace and its projects | ✓ | ✓ | ✓ |
| Create and edit projects | ✓ | ✓ | ✓ |
| View workspace settings (name, ID) | ✓ | ✓ | ✓ (read-only) |
| Rename the workspace | ✓ | ✓ | |
| Later: invite and remove members, change roles | ✓ | ✓ (not owners) | |
| Later: delete the workspace, transfer ownership | ✓ | | |

- The creator becomes `owner` in the same transaction as the workspace insert.
- The API checks roles: `requireRole(admin)` wraps `PATCH /api/workspaces/{ws}`, and a member gets a 403. The web only hides or disables controls.
- The role is returned with the workspace (`"role": "owner"`), so the UI knows what to show.

## Migrations (squashed while in development)

There's no production database yet, so each table keeps a single create migration, and changes are made to that file instead of being added as new migrations. There's no backfill or data-move SQL; the schema is simply what the files say. Each pair is renumbered so the files run in dependency order (decision 13):

| File | Change |
| --- | --- |
| `001_create_public_id_function` | **New:** the `public_id(prefix)` function |
| `002_create_users` | Was 001. Adds `public_id` (default, `CHECK`, `UNIQUE`) |
| `003_create_sessions` | Was 002. Unchanged |
| `004_create_workspaces` | **New:** `workspaces` and `workspace_user` (with `role`, `UNIQUE`, the index) |
| `005_create_projects` | Was 003. `workspace_id` replaces `user_id`, adds `public_id`, and indexes `(workspace_id, updated_at DESC)` |
| `006_create_auth_attempts` | Was 004. Unchanged |

Down files stay as the matching `DROP`s, so `make migrate-rollback` still works.

### `make migrate-fresh`

A new `fresh` command in `cmd/migrate`, following `rollback`'s preview-then-confirm pattern:

```
make migrate-fresh               # preview: database name, tables and row counts that would be dropped
make migrate-fresh CONFIRM=1     # DROP SCHEMA public CASCADE; CREATE SCHEMA public; then migrate up
make seed                        # recreate the demo account (separate, explicit)
```

- It refuses when `APP_ENV=production`, whether or not `CONFIRM` is set.
- It drops only the `public` schema. Integration tests' temporary schemas aren't affected. The `lewcontext` role owns the database, so it can recreate the schema; this has been checked.
- Seeding stays a separate step, so a fresh database is empty unless you ask for demo data (decision 14).
- For this change: once the edited migrations are in, run `make migrate-fresh CONFIRM=1 && make seed` on the local database (decision 8).

### What this changes

- **Local data:** the dev database currently has 26 users and 769 projects (the demo account, seeded volume and e2e leftovers). `migrate-fresh` deletes all of it. `make seed` recreates the demo account; fake data is recreated with `make seed only=volume|users`. I'll ask before running it.
- **Rules:** AGENTS.md and `docs/api/database.md` currently say "never rewrite applied migrations" and "do not drop or reset databases". Both change to:
  - **While in development,** change a table by editing its create migration, then run `make migrate-fresh CONFIRM=1` on the local database only.
  - **Once a database holds data that must be kept,** migrations become append-only again.
  - `migrate-fresh` is never run against a non-local database, and is refused in production.
- **Legacy adoption:** `adoptLegacyHistory` and `legacyMigrations` in `migrate.go` copy the previous runner's history into Bun's. After a fresh rebuild no such database remains, and renumbered files would break the mapping anyway. They're removed, along with `TestMigrateAdoptsLegacyRunnerHistory` (decision 15).
- **Other checkouts:** worktrees that share this database need `make migrate-fresh` after pulling, because Bun only runs files it hasn't recorded, and edited files with the same name won't re-run. The docs say so next to the command.

## API

### Routes

| Method | Path | Who | Notes |
| --- | --- | --- | --- |
| GET | `/api/workspaces?page&perPage` | session | `Page<Workspace>` of the user's workspaces with their role, newest first (`created_at DESC, id DESC`). Parameters as for projects: page 1–10 000, perPage 1–100, default 20. No search or sort for now (decision 16) |
| POST | `/api/workspaces` | session | `{ name }`; the creator becomes `owner` |
| GET | `/api/workspaces/{workspace}` | member | |
| PATCH | `/api/workspaces/{workspace}` | admin+ | `{ name }` |
| GET | `/api/workspaces/{workspace}/projects` | member | Was `/api/projects`; same pagination, search and sort |
| POST | `/api/workspaces/{workspace}/projects` | member | |
| GET | `/api/workspaces/{workspace}/projects/{project}` | member | `{project}` is `prj_…` |
| PATCH | `/api/workspaces/{workspace}/projects/{project}` | member | |

`/api/auth/*` stays per account. `/api/auth/me` returns `usr_` IDs. The old `/api/projects*` routes are removed: the web app is the only client, and it changes in the same commit.

### Grouping

`net/http`'s `ServeMux` has no `Group()`. Mounting a sub-mux doesn't help, because `http.StripPrefix` can't strip a prefix that contains `{workspace}`. A ~15-line `group()` helper in `routes.go` prefixes the patterns and wraps the handlers. `routes.go` stays the single registry, and no workspace route can be added without the membership check:

```go
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// …health and auth as today…
	mux.Handle("GET /api/workspaces", s.requireSession(http.HandlerFunc(s.workspaces)))
	mux.Handle("POST /api/workspaces", s.requireSession(http.HandlerFunc(s.createWorkspace)))

	ws := group(mux, "/api/workspaces/{workspace}", s.requireSession, s.requireWorkspace)
	ws.HandleFunc("GET", "", s.workspace)
	ws.Handle("PATCH", "", s.requireRole(store.RoleAdmin, http.HandlerFunc(s.updateWorkspace)))
	ws.HandleFunc("GET", "/projects", s.projects)
	ws.HandleFunc("POST", "/projects", s.createProject)
	ws.HandleFunc("GET", "/projects/{project}", s.project)
	ws.HandleFunc("PATCH", "/projects/{project}", s.updateProject)
	return s.security(mux)
}
```

### Authorization

- `requireWorkspace` (in `middleware.go`) runs after `requireSession`. It validates the `{workspace}` format, then makes **one query** that finds the workspace through a `workspace_user` row for the session's user. A user who isn't a member, or a workspace that doesn't exist, both get `404 Workspace not found.`, so the API doesn't reveal which workspaces exist. On success it puts the workspace and role in the request context (`currentWorkspace(r)`).
- `requireRole(min)` compares the stored role with `owner > admin > member` and returns 403 when it's too low.
- Project queries take the internal workspace ID from the context and the `prj_` ID from the path: `WHERE p.public_id = ? AND p.workspace_id = ?`. A project requested under another workspace's URL, even one the user belongs to, is a 404.
- Request bodies never carry workspace, user or role IDs.

### Store, factories and seeds

- `store`: `Workspace` and `Membership` models, plus `Role` constants. `WorkspacePage(userID, PageRequest)` (scoped by the membership join, then `Paginate`), `MemberWorkspace(userID, publicID)`, `CreateWorkspace(userID, name)` (a transaction that inserts the workspace and the owner membership), `RenameWorkspace(id, name)`. Project methods change from `(userID, id int64)` to `(workspaceID int64, publicID string)`.
- `factory`: `Workspaces`, `WorkspaceUsers` (role defaults to `owner`), `WorkspaceFor(userID)`, `ProjectsIn(workspaceID)`. `TestEveryTableHasAFactory` covers the new tables.
- `seed`: `demo` creates the demo user, an "Acme" workspace with an owner membership, and the demo projects. `volume` adds to the account's most recently created workspace. `users` gives each fake user a workspace.
- Registration only creates the user. The new account has no workspace and no projects until onboarding.

## Web

### Pages

```
src/pages/app/
  index.tsx                          → redirect: the newest workspace's dashboard, or onboarding when there's none
  onboarding.tsx                     "New workspace" [Name] [Create workspace]; AuthLayout, not the sidebar
  settings.tsx                       account & security (unchanged, per user)
  [workspace]/dashboard.tsx          was app/dashboard.tsx
  [workspace]/projects.tsx           was app/projects.tsx
  [workspace]/projects/[project].tsx was app/projects/[id].tsx
  [workspace]/settings.tsx           new: workspace settings
```

- Next.js serves static segments (`/app/onboarding`, `/app/settings`) before `[workspace]`, and workspace IDs always start with `wsp_`, so they can't collide.
- **`appProps`** loads, in parallel:
  - the session;
  - the first page of `GET /api/workspaces` (20, newest first);
  - on a `[workspace]` page, `GET /api/workspaces/{ws}`.

  The list is paginated, so the current workspace may not be on page 1. That's why it's loaded on its own, and its 404 becomes `notFound`.
- **With no workspaces** (`pages.items === 0`), `appProps` redirects to `/app/onboarding`.
- **Page props** gain `workspace` (current, with `role`) and `workspaces` (`Initial<Page<Workspace>>`, page 1), so the switcher renders on the server with no flash.
- **Pages without a workspace in the URL** (`/app/settings`): `workspace` is the newest one (`data[0]`), so the sidebar links still go somewhere sensible.
- `_app` adds `WorkspaceProvider` next to `SessionProvider`, read with `useWorkspace()` (the current workspace and role). The switcher reads the list itself (see below).
- **`/app`** (`app/index.tsx`) loads `GET /api/workspaces?perPage=1` and redirects to `data[0]`'s dashboard, or to onboarding.
- **Sign-in flow:**
  - `useAuthSubmit` goes to `returnPath(next)` or `/app`, and `/app` picks the newest workspace or onboarding.
  - Registration always lands on onboarding.
  - Onboarding redirects to `/app` when the user already has a workspace.
- **Redirects** (decision 11):
  - `/` → `/app` is the only redirect left in `next.config.mjs`. `/app` is `app/index.tsx`, which needs data to choose a workspace.
  - The `/app → /app/dashboard`, `/auth`, `/login`, `/projects` and `/settings` redirects are deleted.
  - `/app/dashboard`, `/app/projects` and numeric project URLs no longer exist, so they 404.
  - `404.tsx`'s link becomes `/app`.
  - Every `routes.*` call site (dashboard, the project table, the project page, the layout, `use-auth-submit`, `server.ts`) uses the workspace-scoped routes.
  - `routes.test.ts` and `workspace.spec.ts` are updated to match.

### Onboarding

The page reuses `AuthLayout`, laid out like login and register:

```
            [Logo]
        New workspace
  Projects live in a workspace. You can create more later.
  Name  [ Acme                    ]
        [ Create workspace ]
```

It has the same validation as the API (1–80 characters), `<Button loading>` while submitting, and an error `Notice` on failure. On success it goes to `/app/wsp_…/dashboard`. The account menu (sign out) is still reachable from the page.

### Create workspace dialog

The switcher's "New workspace…" item opens a `Dialog` with the same name field. The form is shared by the onboarding page and the dialog, so it lives in `components/app/workspaces/workspace-form.tsx` (rule 3: used by more than one page). On success, it updates the cached workspace list and navigates to the new workspace's dashboard.

### Workspace settings (`/app/wsp_…/settings`)

It's laid out like account settings, using the same `SettingsSection` (title and description on the left, content on the right). `SettingsSection` is currently local to `settings.tsx`; with a second page using it, it moves to `components/ui/settings-section.tsx` (rule 3).

| Section | Content |
| --- | --- |
| General | A name `Input` with a Save `<Button loading>`, and a success or error `Notice`. Admins and owners get the form; members see the name as text |
| Workspace ID | A read-only `Input` with `wsp_…` and a Copy button (`navigator.clipboard.writeText`, then "Copied" announced through a `role="status"` region). If the clipboard isn't available, the field stays selectable |
| Your role | A `Badge`: Owner / Admin / Member |

The sidebar's workspace dropdown gets "Workspace settings" pointing here. Account & security stays in the account menu.

### Sidebar switcher

`WorkspaceDropdown` shows the current workspace's name and initial. The menu has:
- "Workspace settings";
- the workspace list, each linking to its dashboard, with a check on the current one;
- "New workspace…" (the dialog).

**Infinite scroll** (decision 16):
- **Hook:** `useResourcePages(pathFor(page), initial)`, a new hook in `lib/use-resource-pages.ts`, wraps SWR's built-in `useSWRInfinite` (SWR 2, no new dependency). It works like `useResource`: the key includes the user ID, page 1 comes from the server, and a 401 redirects to login. It returns `items` (all loaded pages, de-duplicated by `id`), `hasMore` (`current < total`), `loadMore()`, and `loading`/`error`/`retry`.
- **Loading the next page:** the list scrolls inside the menu (`max-h-80 overflow-y-auto`). A sentinel element after the last item is watched with `IntersectionObserver` (root: the menu), and when it comes into view the next page loads. It's only rendered when `hasMore`, so with 20 workspaces or fewer there's nothing extra.
- **Keyboard:** arrowing to the last item scrolls it into view, which triggers the sentinel. As a fallback for keyboard users and environments without `IntersectionObserver`, the sentinel is a focusable "Show more" item that loads on click.
- **States** (rule 8):
  - **Loading:** a skeleton row with `aria-busy`.
  - **Failure:** a "Couldn't load more · Retry" item that keeps the loaded items.
  - **Empty:** can't happen, because users without a workspace are redirected to onboarding.
- **Data changes:** offset pages shift when a workspace is created, so items are de-duplicated by `id`, and creating or renaming one revalidates the whole list (`mutate()` on the infinite key). Switching is a normal navigation, so `ApplicationLayout` stays mounted. `navigation(workspace)` builds the sidebar links. The current-item check moves from `router.pathname`, which would now be `/app/[workspace]/…`, to `router.asPath` without the query.

### Routes, paths and client

```ts
// lib/routes.ts
routes.dashboard(ws)                 // "/app/wsp_…/dashboard"
routes.projects(ws)
routes.project(ws, project)          // "/app/wsp_…/projects/prj_…"
routes.workspaceSettings(ws)
routes.settings, routes.onboarding, routes.app

// lib/api
paths.workspaces.index({ page })     api.workspaces().create({ name })
paths.workspaces.show(ws)            api.workspaces().update(ws, { name })
paths.projects(ws).index(query)      api.projects(ws).create(input)
paths.projects(ws).show(project)     api.projects(ws).update(project, input)
```

- `lib/ids.ts`: `parseId` becomes `parsePublicId("prj", value)`, using the same regex as Go.
- `returnPath` patterns become `^/app/wsp_[0-9A-Za-z]{14}/projects/prj_[0-9A-Za-z]{14}$` and so on; `/app/onboarding` isn't a return target. The existing test that every `/app` route has a pattern still applies.
- SWR keys already include the user ID, and workspace paths include `wsp_`, so caches never mix workspaces. `paths.projects(ws).matches(key)` limits the project editor's cache invalidation to one workspace.

## Tests

- **Go integration:**
  - A non-member gets 404 on every `/api/workspaces/{ws}/…` route (GET, POST and PATCH).
  - A project requested through another of the user's workspaces is a 404.
  - A `member` gets 403 renaming; `admin` and `owner` succeed.
  - Malformed `wsp_`/`prj_` IDs get a 404 without a query.
  - Registration creates no workspace.
  - Creating a workspace makes the creator `owner`.
  - `GET /api/workspaces` pages newest first, returns only the user's memberships (another user's workspaces never appear in `items`), and rejects bad `page`/`perPage` with 400.
  - The old `/api/projects` routes return 404.
  - The existing migrate round-trip test covers the squashed files (up, down, up in a temp schema).
  - `migrate fresh` refuses in production and without `-confirm`.
  - `public_id` defaults are unique and match the format.
  - Responses never contain a numeric `id`.
- **Vitest:**
  - `parsePublicId`.
  - `returnPath` accepts the new routes and rejects numeric IDs and `/app/onboarding`.
  - The route builders.
  - `paths.projects(ws).matches` ignores another workspace's keys.
  - The workspace form's validation and its pending and error states.
  - The copy button's status message.
  - `useResourcePages`: it starts from the server-rendered page 1; `loadMore` fetches page 2 and de-duplicates by `id`; `hasMore` is false on the last page; a failure keeps the loaded items and `retry` works; a 401 redirects to login.
- **Playwright:**
  - Register → onboarding → create workspace → `/app/wsp_…/dashboard`.
  - Create a project and open it at `/app/wsp_…/projects/prj_…`.
  - Create a second workspace from the switcher; it's listed and opens.
  - With 25 seeded workspaces (factories through the test database), scrolling the switcher loads the rest.
  - Rename it in settings, and the switcher shows the new name.
  - Copy the ID.
  - Another user's workspace URL is a 404.
  - Sign out and back in → the most recently created workspace opens.
  - A member-role user sees settings read-only (seeded with a `member` membership through the test database, since invites don't exist yet).

## Docs to update

- `docs/api/endpoints.md`.
- `api/database.md`: public IDs, the `public_id()` default, scoping by workspace instead of `user_id`, squashed migrations and `make migrate-fresh`. Drop the "Legacy history" bullet.
- `docs/testing.md` (local setup): `make migrate-fresh CONFIRM=1 && make seed` after pulling schema changes.
- `api/auth.md`: roles.
- `api/seeding.md` and `api/testing.md`.
- `ui/pages.md` (onboarding, `[workspace]`), `ui/api.md`, `ui/pagination.md` (`useResourcePages`, infinite lists), `api/pagination.md` (`GET /api/workspaces` parameters; scoping example by workspace).
- `docs/README.md`, "Adding a resource end to end": a public ID prefix, scoping by workspace, a role check.
- AGENTS.md:
  - Rule 4: paths.
  - Rule 9: "new registrations start with no workspace; onboarding creates the first".
  - The auth invariant: "scope SQL by the authenticated user's workspace membership; never expose internal IDs".
  - Database and migrations: squashed in development and `migrate-fresh` on the local database (replaces "never rewrite applied migrations" and "do not drop/reset databases" until there's data to keep).

## Build order

It all ships as one change (decision 12), built in this order so each layer's tests pass before the next:

1. `make migrate-fresh` and its tests, plus the AGENTS.md and database-doc rule change.
2. Edited migrations (public IDs, workspaces, projects on workspaces), factories, seeds. Then `migrate-fresh` and `seed` on the local database, after asking.
3. Store, `requireWorkspace`/`requireRole`, the grouped routes, handlers, and integration tests.
4. Web: types, `lib/ids`, `lib/api`, routes, `appProps`, `WorkspaceProvider`, `[workspace]` pages, the `/app` redirect, and removing the old routes.
5. Onboarding, the switcher, the create dialog, and workspace settings.
6. Docs and AGENTS.md. Run `make test`, `make check`, `make test-integration`, `make test-e2e` and `make build`.

## Open questions

None. All answered as decisions 1–18.
