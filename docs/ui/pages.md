# Pages and routing

## URLs

| Path | Page | Notes |
| --- | --- | --- |
| `/` | (redirect) | → `/app`, the only redirect in `next.config.mjs` |
| `/app` | `pages/app/index.tsx` | Redirects to the newest workspace's dashboard, onboarding, or login |
| `/app/onboarding` | `pages/app/onboarding.tsx` | Signed in without a workspace: create the first one (`AuthLayout`) |
| `/app/settings` | `pages/app/settings.tsx` | Account & security (per user) |
| `/app/wsp_…/dashboard` | `pages/app/[workspace]/dashboard.tsx` | |
| `/app/wsp_…/projects` | `pages/app/[workspace]/projects.tsx` | |
| `/app/wsp_…/projects/prj_…` | `pages/app/[workspace]/projects/[project].tsx` | |
| `/app/wsp_…/settings` | `pages/app/[workspace]/settings.tsx` | Rename (owners and admins), copy the ID, your role |
| `/auth/login`, `/auth/register` | `pages/auth/*` | Signed out |

Static segments (`onboarding`, `settings`) are matched before `[workspace]`, and workspace IDs always start with `wsp_`, so they can't collide. There are no redirects from older URLs (`/app/dashboard`, `/app/projects/42`, `/login`); they 404.

## Signed-in pages

Each page under `pages/app/` renders only its content and sets `Page.getLayout = appLayout`. `_app` wraps it in `ApplicationLayout`, which stays mounted between pages (including when switching workspaces), so sidebar state and the sliding current-page indicator survive navigation. Components read the session with `useSession()` and the workspace with `useWorkspace()` rather than taking them as props.

```tsx
type TasksProps = { tasks: Initial<Page<Task>> };

export function getServerSideProps(context: GetServerSidePropsContext) {
  const query = { page: page(context.query) };
  return appProps(context, async (api, workspace) => ({
    tasks: await initial(api, paths.tasks(workspace.id).index(query), "We couldn’t load your tasks. Please try again."),
  }));
}
export default function Tasks(props: AppPageProps<TasksProps>) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <TasksContent {...props} />;
}
Tasks.getLayout = appLayout;
```

## Server rendering (`lib/server.ts`)

`appProps(context, load)` handles everything a signed-in page has in common:

- sets `Cache-Control: private, no-store`,
- builds `serverApi(context)`: the typed client ([api.md](api.md)) on the fixed `API_URL`, forwarding **only** the request cookie, new for each request,
- loads, in parallel, the session (`/api/auth/me`), the first page of the member's workspaces and, on `[workspace]` pages, that workspace (`/api/workspaces/{workspace}`). A malformed `wsp_` ID or a workspace the user isn't a member of → the 404 page; 401 → `/auth/login?next=<this page>`; any other failure → 503 `ServiceUnavailable`,
- with no workspaces at all, redirects to `/app/onboarding`,
- picks the workspace: the one in the URL, or the newest on pages outside a workspace (`/app/settings`),
- runs the page's `load(api, workspace)`: an API 404 → the Next 404 page, 401 → login.

Page props are `session`, `workspace` (with the member's `role`), `workspaces` (`Initial<Page<Workspace>>`, page 1, for the switcher) and the page's own data.

`initial(api, path, failure)` server-renders one typed path from `paths`. A failure becomes `{ data: null, error }`, so the page still renders and the client retries. Pages that only need the session and workspace use `sessionProps`.

`workspaceHomeProps` (`/app`) and `onboardingProps` (`/app/onboarding`) are the two signed-in pages without the sidebar. Onboarding sends accounts that already have a workspace on to `/app`.

`guestProps(context, { redirectSignedIn })` is the guest-page version: `session` is the visitor's session or `null`, and the response is `no-store`. With `redirectSignedIn` (login, register) a signed-in visitor goes on to `?next=` or `/app`. If the session service is unreachable, the page renders signed out.

Never cache user data globally or reuse an authenticated fetch between requests. Styletron collects styles per request (`_document.tsx`).

## The session and the workspace

`_app` puts `pageProps.session` in `SessionProvider` (`lib/session.tsx`) and `pageProps.workspace`/`workspaces` in `WorkspaceProvider` (`lib/workspace.tsx`) around every page. Every navigation reruns `getServerSideProps`, so they stay current after sign-in, sign-out, switching and renaming (the settings page reruns it with `router.replace(router.asPath)`). They are per render tree, so on the server they belong to one request; never copy them into module-level variables.

| Hook | Returns | Use in |
| --- | --- | --- |
| `useSession()` | `Session`; throws when signed out | Signed-in UI (the name, `signedInAt`) |
| `useIsGuest()` | `true` when signed out | Guest pages: "Log in" or "Dashboard" |
| `useOptionalSession()` | `Session \| null` | Guest pages that need the user; hooks that work everywhere |
| `useWorkspace()` | `Workspace` with `role`; throws outside signed-in pages | Paths and links (`workspace.id`), the name, role checks (`canManage(role)`) |
| `useWorkspaceList()` | All the member's workspaces, a page at a time | The switcher ([pagination.md](pagination.md#infinite-lists)) |
| `useRefreshWorkspaces()` | Reloads that list | After creating or renaming a workspace |

`useApi()` and `useResource()` read the CSRF token and user ID from the session ([api.md](api.md)), so pages don't pass `session`, `csrfToken` or `userId` down.

## Returning after sign-in (`?next=`)

Signed-out visits to a signed-in page go to `/auth/login?next=<path and query>`, and signing in returns there. Login and register link to each other with the same `next`, but registering always goes to onboarding: a new account has no workspace yet.

`returnPath(next)` in `lib/routes.ts` accepts only a path starting with `/` that stays on our origin and matches one of `returnRoutes` (`/app`, `/app/settings` and the workspace pages, with well-formed `wsp_`/`prj_` IDs). Anything else (other sites, `//host`, `/\host`, `/auth/*`, `/api/*`, `/app/onboarding`, numeric IDs) falls back to `/app`. It is only used as a page navigation, so it can't submit anything. **Add a pattern to `returnRoutes` for each new signed-in page.**

## Routes

- **Page URLs:** `routes` in `lib/routes.ts`. Workspace pages take the workspace ID: `routes.project(workspace.id, project.id)` → `/app/wsp_…/projects/prj_…`.
- **API paths** live with their resource in `lib/api/*` and are exported as `paths`, e.g. `paths.projects(workspace.id).show(id)`.
- **Auth links:** `loginPath(path)` and `authPath(page, next)` build login/register URLs that carry an allowed `next`.
- **Dynamic IDs:** read `[workspace]`/`[project]` with `parsePublicId("wsp" | "prj", value)` from `lib/ids.ts`. It returns `null` (render 404) unless the value is that prefix and 14 base62 characters.
- **Sidebar:** `navigation(workspace.id)` in `routes.ts` lists the items. An item is current on its own path and on sub-paths (`/app/wsp_…/projects/prj_…` highlights Projects), compared against `router.asPath`.

## Signed-out pages

`pages/auth/*` use `guestProps(context, { redirectSignedIn: true })`, wrap their content in `AuthLayout` and submit with `useAuthSubmit(api.auth().login)` (or `.register` with `routes.onboarding` as the destination, with `const api = useApi()`), which disables the form while pending and opens `?next=` or `/app` on success. A future public page uses `guestProps(context)` and `useIsGuest()`.

## Status pages

`ServiceUnavailable` (503, with retry), and `404.tsx` via `StatusPage`, linking back to `/app`. `RouteProgress` shows a top bar for client navigations slower than 200 ms.
