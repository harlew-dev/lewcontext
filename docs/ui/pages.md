# Pages and routing

The web app is a single-page app on React Router v7 (framework mode, `ssr: false`), built by Vite. Public pages listed in `react-router.config.ts` (`prerender`) are rendered to HTML at build time; every other URL gets one shell (`__spa-fallback.html`) and renders in the browser. In production the Go server serves both ([api/README.md](../api/README.md#serving-the-web-app)).

## URLs

Every page is registered with its URL in `src/routes.ts`; there is no file-system routing.

| Path | Page | Notes |
| --- | --- | --- |
| `/` | `pages/home.tsx` | Public homepage, **prerendered** for search engines |
| `/auth/login`, `/auth/register` | `pages/auth/*` | Signed out |
| `/app` | `pages/app/index.tsx` | Its loader redirects to the newest workspace's dashboard, onboarding, or login |
| `/app/onboarding` | `pages/app/onboarding.tsx` | Signed in without a workspace: create the first one (`AuthLayout`) |
| `/app/settings` | `pages/app/settings.tsx` | Account & security (per user) |
| `/app/wsp_…/dashboard` | `pages/app/[workspace]/dashboard.tsx` | |
| `/app/wsp_…/projects` | `pages/app/[workspace]/projects.tsx` | |
| `/app/wsp_…/projects/prj_…` | `pages/app/[workspace]/projects/[project].tsx` | |
| `/app/wsp_…/settings` | `pages/app/[workspace]/settings.tsx` | Rename (owners and admins), copy the ID, your role |
| anything else | `pages/not-found.tsx` | Go answers it with status 404 |

`app/settings` and `app/onboarding` are fixed segments, so React Router ranks them above `app/:workspace/...`, and workspace IDs always start with `wsp_`, so they can't collide. There are no redirects from older URLs (`/app/dashboard`, `/app/projects/42`, `/login`).

## Signed-in pages

Pages under `pages/app/` (except `/app` and onboarding) sit inside the **layout route** `pages/app/layout.tsx` in `routes.ts`. It wraps them in `ApplicationLayout`, which stays mounted between pages (including when switching workspaces), so sidebar state and the sliding current-page indicator survive navigation. A page renders only its content, reads the session with `useSession()` and the workspace with `useWorkspace()`, and fetches its own data:

```tsx
// src/routes.ts, inside layout("pages/app/layout.tsx", [...])
route("app/:workspace/tasks", "pages/app/[workspace]/tasks.tsx"),

// pages/app/[workspace]/tasks.tsx
export default function Tasks() {
  const workspace = useWorkspace();
  const [search] = useSearchParams();
  const list = useResource(paths.tasks(workspace.id).index({ page: page(search) }));
  if (!list.data) return list.loading ? <ListSkeleton label="Loading tasks" /> : null;
  // …render list.data, list.error with Retry (Notice), an EmptyState
}
```

**Moving between pages never waits.** Pages have no loaders: they render on the first frame and show skeletons while `useResource` loads ([api.md](api.md#reading-data-useresource)). Revisiting a page shows SWR's cached data at once and refreshes it in the background. A record that doesn't exist (`useResource(...).missing`) renders "not found" inside the layout.

## Route loaders (`lib/loaders.ts`)

Loaders run in the browser before a route renders. They only decide what a page can't render without: the session and where to send the visitor.

| Loader | Route | Does |
| --- | --- | --- |
| `signedInLoader` | the app layout | Loads the session and page 1 of the member's workspaces, once (`shouldRevalidate` is false). 401 → `/auth/login?next=<this page>`; no workspace → `/app/onboarding`; API unreachable → `ServiceUnavailable` |
| `workspaceHomeLoader` | `/app` | Redirects to the newest workspace's dashboard or onboarding |
| `onboardingLoader` | `/app/onboarding` | Needs a session; accounts with a workspace go on to `/app` |
| `guestLoader` | login, register | Signed-in visitors go on to `?next=` or `/app`; if the API is down the form still renders |

While the first page's loader runs, the root `HydrateFallback` shows an empty page. Loaders use `loaderApi()`, a read-only client ([api.md](api.md)).

**Go checks the session first.** For `/app` and `/app/*`, the Go server sends visitors without a valid session to `/auth/login?next=…` before serving any HTML. The 401 handling in the loaders covers a session that expires afterwards, and the Vite dev server, which has no such gate.

## The session and the workspace

The layout route puts the loaded session in `SessionProvider` (`lib/session.tsx`) and the current workspace in `WorkspaceProvider` (`lib/workspace.tsx`). The current workspace is the one in the URL, or the newest on pages outside a workspace (`/app/settings`). A workspace already in the switcher's list shows at once while `useResource(paths.workspaces.show(id))` refreshes it, so switching doesn't wait; a malformed `wsp_` ID or someone else's workspace renders the not-found page. Never copy the session or workspace into module-level variables.

| Hook | Returns | Use in |
| --- | --- | --- |
| `useSession()` | `Session`; throws when signed out | Signed-in UI (the name, `signedInAt`) |
| `useIsGuest()` | `true` when signed out | Guest UI under a `SessionProvider` |
| `useOptionalSession()` | `Session \| null` | Hooks that work everywhere |
| `useWorkspace()` | `Workspace` with `role`; throws outside signed-in pages | Paths and links (`workspace.id`), the name, role checks (`canManage(role)`) |
| `useWorkspaceList()` | All the member's workspaces, a page at a time | The switcher ([pagination.md](pagination.md#infinite-lists)) |
| `useRefreshWorkspaces()` | `(id?) => void`: reloads the list, and `id`'s record | After creating or renaming a workspace |

`useApi()` and `useResource()` read the CSRF token and user ID from the session ([api.md](api.md)), so pages don't pass `session`, `csrfToken` or `userId` down.

## Returning after sign-in (`?next=`)

Signed-out visits to a signed-in page go to `/auth/login?next=<path and query>`, and signing in returns there. Login and register link to each other with the same `next`, but registering always goes to onboarding: a new account has no workspace yet.

`returnPath(next)` in `lib/routes.ts` accepts only a path starting with `/` that stays on our origin and matches one of `returnRoutes` (`/app`, `/app/settings` and the workspace pages, with well-formed `wsp_`/`prj_` IDs). Anything else (other sites, `//host`, `/\host`, `/auth/*`, `/api/*`, `/app/onboarding`, numeric IDs) falls back to `/app`. It is only used as a page navigation, so it can't submit anything. **Add a pattern to `returnRoutes` for each new signed-in page.**

## Routes

- **Page URLs:** `routes` in `lib/routes.ts`. Workspace pages take the workspace ID: `routes.project(workspace.id, project.id)` → `/app/wsp_…/projects/prj_…`.
- **API paths** live with their resource in `lib/api/*` and are exported as `paths`, e.g. `paths.projects(workspace.id).show(id)`.
- **Auth links:** `loginPath(path)` and `authPath(page, next)` build login/register URLs that carry an allowed `next`.
- **Dynamic IDs:** read `:workspace`/`:project` from `useParams()` with `parsePublicId("wsp" | "prj", value)` from `lib/ids.ts`. It returns `null` (render "not found") unless the value is that prefix and 14 base62 characters.
- **Links:** `Link`, `Button href`, `TextLink` and table rows render React Router links. `preventScrollReset` keeps the scroll position (sortable headers use it); `useNavigate()` navigates from code.
- **Sidebar:** `navigation(workspace.id)` in `routes.ts` lists the items. An item is current on its own path and on sub-paths (`/app/wsp_…/projects/prj_…` highlights Projects), compared against the location being navigated to.

## Public pages and prerendering

`pages/home.tsx` is listed in `react-router.config.ts` under `prerender`, so `react-router build` writes `build/client/index.html` with its content, `<title>` and meta description (from `root.tsx`'s `meta`, or the page's own `meta` export). It is built without a session, so it shows "Log in" and switches to "Dashboard" once the browser finds a session (`GET /api/auth/me`).

To add a public page: register it in `routes.ts`, add its path to `prerender`, and keep it free of user data. A prerendered route may have a `loader`, which runs at build time.

BaseUI styles itself with Styletron. `src/entry.server.tsx` runs only at build time: it renders each prerendered page (and the fallback) with a fresh Styletron server engine and writes the collected CSS into `<head>` as `<style class="_styletron_hydrate_">`. In the browser, `lib/styletron.ts` starts from those tags (`entry.client.tsx`).

## Signed-out pages

`pages/auth/*` export `clientLoader` → `guestLoader(request)`, wrap their content in `AuthLayout` and submit with `useAuthSubmit(api.auth().login)` (or `.register` with `routes.onboarding` as the destination, with `const api = useApi()`), which disables the form while pending and opens `?next=` or `/app` on success.

## Status pages

`StatusPage` in `components/layout/status-page.tsx` renders `NotFound` ("This page has wandered off.", linking to `/app`) and `ServiceUnavailable` (retry). The root `ErrorBoundary` shows `NotFound` for a 404 route error and `ServiceUnavailable` otherwise. `RouteProgress` shows a top bar when a navigation waits more than 200 ms on a loader.
