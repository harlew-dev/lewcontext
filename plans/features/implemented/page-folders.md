# Page folders

Status: **implemented 2026-11-02** (decisions 1 and 3 changed by the user: 1 now moves too, 3 is fully uniform). Follows the [CRUD naming convention](crud-routing-convention.md), which left "renaming page files to `index.tsx`" out of scope. Built as planned, with two notes: the folder check also lives in [checks.md](../../../docs/checks.md#whats-checked)'s table, and `docs/ui/pages.md`'s URL table gained its **API** column spanning every row. Docs updated: [naming.md](../../../docs/naming.md#web-app), [ui/pages.md](../../../docs/ui/pages.md#urls), [ui/README.md](../../../docs/ui/README.md), [ui/testing.md](../../../docs/ui/testing.md), [ui/pagination.md](../../../docs/ui/pagination.md), [api/admin.md](../../../docs/api/admin.md), [api/monitoring.md](../../../docs/api/monitoring.md), [compliance.md](../../../docs/compliance.md), [README.md](../../../README.md), AGENTS.md rule 4. The admin tests covering several pages (`admin.test.tsx`, `monitoring*.test.tsx`, `actions.test.tsx`, `alerts.test.tsx`, `events.test.tsx`) stayed at `pages/admin/` as planned.

## Why

A resource's pages are split across two places. The projects list is `pages/app/[workspace]/projects.tsx`, and the project page is `pages/app/[workspace]/projects/[project].tsx`, one folder down. Workspace settings is worse: the General tab is `settings.tsx`, beside a `settings/` folder that holds its own layout and the Members tab. To find all of a resource's pages you have to look in two places, and the list page sits outside the folder named after it.

This is a frontend cleanup: one folder for every page URL, and its own page is the folder's `index.tsx` — fully uniform, so no page file sits outside a folder and no exception list is needed to tell which are. The API already groups by resource (`projects.Index` and `projects.Show` in `router/routes.go`), so after the move each page lines up with the actions it calls:

| Page file | Page | API it reads |
| --- | --- | --- |
| `projects/index.tsx` | `ProjectIndex` | `projects.Index` |
| `projects/[project].tsx` | `ProjectShow` | `projects.Show` |
| `settings/index.tsx` | `WorkspaceEdit` | `workspaces.Show` |
| `settings/members/index.tsx` | `MemberIndex` | `members.Index`, `invitations.Index` |

## The rule (as confirmed)

**Every page is a folder, and the page is the folder's `index.tsx`.** A page URL is one folder; pages beneath it go in the folder as deeper folders. A record's page is `[<resource>].tsx` in the same folder, named after the route param (`[project]`, `[token]`), as it is today. Deeper pages sit in deeper folders (`register/verify/index.tsx`). This is the frontend version of the API's rule, "a resource gets its own folder only when it has sub-resources" ([naming.md](../../../docs/naming.md#controllers-internalhttpapi)) — but stricter, so the check needs no list of who may stay a single file. URLs, route constants, page export names and the API don't change.

Tests sit beside the page they test: a page's test beside it (`login/index.test.tsx`, `settings/members/index.test.tsx`), a folder's pages tested together in the folder's own test (`invitations/[token].test.tsx`), and tests spanning areas (`pages/admin/admin.test.tsx`) stay at the area root.

## Changes

As of 2026-11-01. The checkout holds uncommitted admin work (`pages/admin/[key]/admins.tsx` and its test), so do this after that work is committed. Before starting, recheck `src/pages` and `src/routes.ts` for pages added since, and apply the rule to them too. Move files with `git mv` so their history follows them.

### Every page file becomes a folder's `index.tsx`

Touching, at the time of writing (recheck against the tree):

| Now | Becomes |
| --- | --- |
| `pages/home.tsx` (+ `home.test.tsx`) | `pages/home/index.tsx` (+ `home/index.test.tsx`) |
| `pages/not-found.tsx` | `pages/not-found/index.tsx` |
| `pages/policy/terms-of-service.tsx`, `pages/policy/privacy-policy.tsx` | `pages/policy/terms-of-service/index.tsx`, `pages/policy/privacy-policy/index.tsx` |
| `pages/auth/login.tsx` (+ test) | `pages/auth/login/index.tsx` (+ `login/index.test.tsx`) |
| `pages/auth/register.tsx` | `pages/auth/register/index.tsx` |
| `pages/auth/register/verify.tsx`, `pages/auth/register-verify.test.tsx` | `pages/auth/register/verify/index.tsx`, `pages/auth/register/verify.test.tsx` |
| `pages/auth/register-policies.test.tsx` | `pages/auth/register/index.test.tsx` |
| `pages/auth/forgot-password.tsx`, `pages/auth/forgot-password.test.tsx` | `pages/auth/forgot-password/index.tsx`, `forgot-password/index.test.tsx` |
| `pages/app/onboarding.tsx`, `pages/app/verify-email.tsx` (+ tests) | `pages/app/onboarding/index.tsx`, `verify-email/index.tsx` (+ `index.test.tsx`) |
| `pages/app/settings.tsx`, `pages/app/settings.test.tsx` | `pages/app/settings/index.tsx`, `settings/index.test.tsx` |
| `pages/app/[workspace]/dashboard.tsx` | `pages/app/[workspace]/dashboard/index.tsx` |
| `pages/app/[workspace]/projects.tsx`, `projects.test.tsx` | `pages/app/[workspace]/projects/index.tsx`, `projects/index.test.tsx` |
| `pages/app/[workspace]/settings.tsx` | `pages/app/[workspace]/settings/index.tsx` |
| `pages/app/[workspace]/settings/members.tsx`, `members.test.tsx` | `pages/app/[workspace]/settings/members/index.tsx`, `members/index.test.tsx` |
| `pages/admin/not-found.tsx` | `pages/admin/not-found/index.tsx` |
| `pages/admin/[key]/users.tsx`, `workspaces.tsx`, `events.tsx`, `mail.tsx`, `admins.tsx`, `email-blocks.tsx`, `ip-blocks.tsx`, `alerts.tsx` | each `<name>/index.tsx` |
| `pages/admin/[key]/monitoring/security.tsx`, `limits.tsx`, `ips.tsx`, `accounts.tsx`, `workspaces.tsx`, `usage.tsx`, `data.tsx`, `integrity.tsx`, `performance.tsx`, `errors.tsx`, `configuration.tsx` | each `<name>/index.tsx` |

Record pages already follow the rule: `[workspace]/projects/[project].tsx`, `auth/forgot-password/[token].tsx`, `auth/invitations/[token].tsx`, `auth/email-blocks/[token].tsx`, `admin/[key]/users/[user].tsx`, `[key]/workspaces/[workspace].tsx`, `[key]/events/[event].tsx`, `[key]/alerts/[alert].tsx`, `[key]/monitoring/ips/[ip].tsx` and `errors/[error].tsx`. `pages/app/index.tsx`, `pages/app/layout.tsx`, `pages/admin/[key]/index.tsx` and the layouts already are folder indexes.

### What else changes with the files

- **`src/routes.ts`:** each moved file's path (`route("app/:workspace/projects", "pages/app/[workspace]/projects/index.tsx")`, `index("pages/app/[workspace]/settings/index.tsx")` under the settings layout).
- **Route type imports:** a moved page imports its generated types as `./+types/index` instead of `./+types/<name>`.
- **Imports of pages in tests** (`@/pages/…` and the relative ones in `pages/admin/*.test.tsx`): append `/index` for a page, so a search for the file finds it (`components/app/workspaces/workspace-settings.test.tsx` too).
- **Test files moved** per the rule above (`login/index.test.tsx`, `members/index.test.tsx`, `register/verify.test.tsx`, `invitations/[token].test.tsx`, `email-blocks/[token].test.tsx`, …). The admin root tests (`admin.test.tsx` and friends) cover several pages each and stay where they are; their imports gain `/index`.
- **`pages/naming.test.ts`** (root, checks exports through `routes.ts`) and the new `layout.test.ts` stay where they are.
- **Route IDs** come from file paths, so the moved pages get new ones (`pages/app/[workspace]/projects/index`). Nothing reads page route IDs; only layouts' IDs could be read, and no layout moves.
- **Nothing else:** URLs, `lib/routes.ts`, `returnRoutes`, the Go page gate, prerendering, CSP, the API and its tests stay the same.

## Check

Add `src/pages/layout.test.ts`, a Vitest test in the same spirit as `TestHTTPLayout` and the [convention checks](convention-checks.md). It walks `src/pages` and fails, with the fix, when:

- a page file (`.tsx`, not `.test.tsx` nor `layout.tsx`) isn't named `index.tsx` or `[<param>].tsx` ("move foo.tsx to foo/index.tsx");
- a folder holds a page file but no `index.tsx` and no `layout.tsx`, and isn't listed with a reason as having no page of its own (`auth/invitations` and `auth/email-blocks` hold only `[token]` record pages; every other folder gets its `index.tsx`).

It reads the folder tree, not `routes.ts`, so it also catches a file that isn't registered yet.

## Docs

- **`docs/naming.md` (Web app):** replace "Page files are shaped like the URL" with the rule above, add the page ↔ API table from Why as the example, and say that the API side is already `<resources>.<Action>` in `router/routes.go`.
- **`docs/ui/pages.md`:** the URL table's file paths, and a new **API** column naming each page's controller actions (`projects.Index`; `projects.Show`, `.Update`, `.Destroy`). Also the `tasks` example under Signed-in pages (`pages/app/[workspace]/tasks/index.tsx`) and the tabs paragraph (`settings/index.tsx`).
- **`docs/ui/README.md`:** the `pages/app/*`, `pages/auth/*` and `pages/admin/*` rows.
- **`docs/ui/testing.md`, `docs/ui/design.md`, `docs/ui/pagination.md`, `docs/api/admin.md`, `docs/api/monitoring.md`, `docs/compliance.md`, `README.md`, `docs/README.md`, `AGENTS.md` rule 4:** any moved path they name.
- **Implemented plans:** page paths they name (`admin/alerts.md`, `admin/monitoring.md`) stay as history.
- **Open plans:** none list this feature yet.

## Security risks and how they're avoided

None new. No URL, header, loader, API call or build output changes. `scripts/check-app-build.sh` and the `no-admin-modules` build check still keep admin code out of the app build, and ESLint's `src/pages/admin/**` rule still covers the moved admin pages. The e2e run confirms the Go page gate and CSP still work.

## Tests

- The new `src/pages/layout.test.ts`. Check that it fails on the tree as it is today before moving any files.
- The existing Vitest suites, with their updated imports. The suites and their assertions stay the same.
- `make check` (TypeScript catches any stale `+types` import), `make build` (both builds, through `routes.ts`) and `make test-e2e E2E_PORT=auto`. `make test` runs, but no Go code changes.

## Decisions (confirmed 2026-11-01)

| # | Question | Decision |
| --- | --- | --- |
| 1 | Account settings (`pages/app/settings.tsx`) | **Moves like every page** (`pages/app/settings/index.tsx`). It isn't a resource, but with fully uniform foldering it isn't an exception either. (Original recommendation — stay single until it gets tabs — replaced.) |
| 2 | Where tests go | **Beside the page they test.** A test of one page is the folder's `index.test.tsx`. Tests that span areas (`pages/admin/admin.test.tsx`) stay where they are. `auth/invitations.test.tsx` and `auth/email-blocks.test.tsx` become `[token].test.tsx` in their folders. |
| 3 | Pages with nothing beneath them | **Also get folders.** Every page URL is a folder with `index.tsx`, even `login.tsx`, `home.tsx`, `mail.tsx`, `admins.tsx`, `members.tsx` and the monitoring sections. No exception list; the check stays simple. |
| 4 | The record page's file name | **`[<param>].tsx`, as now**, rather than `show.tsx`. It matches the route param (`:project`) and the existing `[token].tsx` pages, and a future edit page fits as `[project]/edit.tsx` … in a folder of its own (as every page now). |

## Not included

- Any change to URLs, route constants, page export names, API paths, Go controllers or store methods.
- Nesting the Go controllers into one package per resource (`endpoints/workspace/projects/`). The router already reads `projects.Index`, and the API's rule gives a resource its own folder once it has sub-resources ([naming.md](../../../docs/naming.md#controllers-internalhttpapi)).
- A `Settings` controller. The API has no settings resource: workspace settings reads `workspaces.Show`, saves with `workspaces.Update`, and its danger zone calls `workspaces.Destroy` and `membership.Destroy`.
- Redirects. No URL moves.
- Moving `components/app/workspaces/workspace-settings.test.tsx` next to the pages it tests. It's a separate cleanup.
