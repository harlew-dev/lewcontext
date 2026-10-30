# Pages and routing

## Signed-in pages

Each page under `pages/app/` renders only its content and sets `Page.getLayout = appLayout`. `_app` wraps it in `ApplicationLayout`, which stays mounted between pages, so sidebar state and the sliding current-page indicator survive navigation. Components read the session with `useSession()` rather than taking it as a prop (see [The session](#the-session)).

```tsx
type TasksProps = SessionProps & { tasks: Initial<Page<Task>> };

export function getServerSideProps(context: GetServerSidePropsContext) {
  const query = { page: page(context.query) };
  return appProps(context, async (api) => ({
    tasks: await initial(api, paths.tasks.index(query), "We couldn’t load your tasks. Please try again."),
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
- loads the session (`/api/auth/me`): 401 → redirect to `/auth/login?next=<this page>`, any other failure → 503 `ServiceUnavailable`,
- runs the page's `load(api)`: an API 404 → the Next 404 page, 401 → login.

`initial(api, path, failure)` server-renders one typed path from `paths`. A failure becomes `{ data: null, error }`, so the page still renders and the client retries. Pages that only need the session use `sessionProps`.

`guestProps(context, { redirectSignedIn })` is the guest-page version: `session` is the visitor's session or `null`, and the response is `no-store`. With `redirectSignedIn` (login, register) a signed-in visitor goes on to `?next=` or the dashboard. If the session service is unreachable, the page renders signed out.

Never cache user data globally or reuse an authenticated fetch between requests. Styletron collects styles per request (`_document.tsx`).

## The session

`_app` puts `pageProps.session` in a React context (`SessionProvider`, `lib/session.tsx`) around every page. Every navigation reruns `getServerSideProps`, so it stays current after sign-in and sign-out. It is per render tree, so on the server it belongs to one request; never copy it into a module-level variable.

| Hook | Returns | Use in |
| --- | --- | --- |
| `useSession()` | `Session`; throws when signed out | Signed-in UI (the name, `signedInAt`) |
| `useIsGuest()` | `true` when signed out | Guest pages: "Log in" or "Dashboard" |
| `useOptionalSession()` | `Session \| null` | Guest pages that need the user; hooks that work everywhere |

`useApi()` and `useResource()` read the CSRF token and user ID from it ([api.md](api.md)), so pages don't pass `session`, `csrfToken` or `userId` down. Page props still include `session`: that's how it reaches the provider.

## Returning after sign-in (`?next=`)

Signed-out visits to a signed-in page go to `/auth/login?next=<path and query>`, and signing in or registering returns there. Login and register link to each other with the same `next`.

`returnPath(next)` in `lib/routes.ts` accepts only a path starting with `/` that stays on our origin and matches one of `returnRoutes` (the `/app` pages, `[id]` as a positive integer). Anything else (other sites, `//host`, `/\host`, `/auth/*`, `/api/*`) falls back to the dashboard. It is only used as a page navigation, so it can't submit anything. **Add a pattern to `returnRoutes` for each new signed-in page.**

## Routes

- **Page URLs:** `routes` in `lib/routes.ts`, e.g. `routes.project(id)` → `/app/projects/42`.
- **API paths** live with their resource in `lib/api/*` and are exported as `paths`, e.g. `paths.projects.show(id)` → `/api/projects/42`.
- **Auth links:** `loginPath(path)` and `authPath(page, next)` build login/register URLs that carry an allowed `next`.
- **Redirects** from `/`, `/app`, `/auth` and pre-nesting URLs (`/login`, `/projects`, `/settings`) live in `next.config.mjs`.
- **Dynamic IDs:** read `[id]` with `parseId(context.params?.id)` from `lib/ids.ts`. It returns `null` (render 404) unless the value is plain digits that JavaScript can represent exactly.
- **Sidebar:** `navigation` in `routes.ts` lists the items. An item is current on its own path and on sub-paths (`/app/projects/42` highlights Projects).

## Signed-out pages

`pages/auth/*` use `guestProps(context, { redirectSignedIn: true })`, wrap their content in `AuthLayout` and submit with `useAuthSubmit(api.auth().login)` (or `.register`, with `const api = useApi()`), which disables the form while pending and opens `?next=` or the dashboard on success. A future public page uses `guestProps(context)` and `useIsGuest()`.

## Status pages

`ServiceUnavailable` (503, with retry), and `404.tsx` via `StatusPage`. `RouteProgress` shows a top bar for client navigations slower than 200 ms.
