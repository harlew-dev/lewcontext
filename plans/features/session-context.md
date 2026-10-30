# Session context (`useSession()`, `useApi()`)

Status: **agreed and implemented 2026-10-30**

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Name | `useSession()` |
| 2 | SWR hook signature | `(path, initial?)`; the hook reads the user ID from the session for the SWR key |
| 3 | CSRF in mutations | `const api = useApi()` in components; the client sends the session's CSRF token itself |
| 4 | Where it works | The provider wraps every page. `useSession()` is for signed-in UI; guest pages use `useIsGuest()` |
| 5 | `useSession()` type | Non-nullable `Session`. It throws on a signed-out page, which is a programming error |
| 6 | SWR hook name | `useResource(path, initial?)` |
| 7 | API unreachable on guest pages | Render as signed out (the login form still works once the API is back) |
| 8 | Return URL | `?next=`, accepted only when it is a signed-in page route of ours (see below) |
| 9 | Register and `next` | The "Create an account" link on login carries `next` to register, and back |
| 10 | `useOptionalSession()` | Exported; guest pages may use it when they need the user, not just `useIsGuest()` |

## Why

Signed-in pages pass `session`, `session.user.id` or `session.csrfToken` through about 25 props and call sites (`DashboardContent`, `StatusStat userId`, `ProjectDialog csrfToken`, `ApplicationLayout session`, `api(session)`). A context lets any component read it.

It can't be a module-level variable (`session().current`): the Next.js server renders many users' requests in one process, so a shared variable could put one user's session in another user's page (AGENTS.md rule 7). React context belongs to one render tree, so it's per request on the server. A browser-only module variable for the CSRF token was considered and rejected for the same rule.

## Names

| Name | What it is | Used for |
| --- | --- | --- |
| `useSession()` | Hook: the current `Session`; throws when signed out | Signed-in UI: the user's name, `signedInAt` |
| `useIsGuest()` | Hook: `true` when signed out | Guest pages: "Log in" or "Dashboard" |
| `useApi()` | Hook: the API client, bound to the session's CSRF token | Calls from handlers: create, update, logout, login |
| `useResource(path, initial?)` | Hook: SWR-cached GET of a typed path | Lists and records a page renders |
| `paths` | Plain object of typed API paths | SWR keys, module constants, server rendering |
| `client(http)` | Builds a client on a transport | `lib/server.ts` only (`serverApi(context)`) |

Today's `useApi(path, userId, initial)` is the SWR hook. It is renamed so `useApi()` can be the client. The browser `api()` / `api(session)` function goes away: components use `useApi()`, and code outside components only needs `paths`.

## Design

### `lib/session.tsx`

```tsx
const SessionContext = createContext<Session | null>(null);

export function SessionProvider({ session, children }: { session: Session | null; children: ReactNode }) {
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}

// The signed-in session. Only for UI on signed-in pages, where appProps
// guarantees one; guest pages use useIsGuest().
export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) throw new Error("useSession() needs a signed-in page");
  return session;
}

// For guest pages, which may be visited signed in or out.
export function useIsGuest(): boolean {
  return useContext(SessionContext) === null;
}

// For guest pages that need the user, and hooks that work on every page
// (useApi, useResource): the session, or null when signed out.
export function useOptionalSession(): Session | null {
  return useContext(SessionContext);
}
```

Hooks must start with `use` so ESLint's rules-of-hooks checks them, hence `useIsGuest()` rather than `isGuest()`. Signed-in UI uses `useSession()`. Guest pages use `useIsGuest()`, or `useOptionalSession()` when they need the user (e.g. "Continue as Ada").

### `_app.tsx`

```tsx
<SessionProvider session={pageProps.session ?? null}>
  {getLayout(<Component {...pageProps} />, pageProps)}
</SessionProvider>
```

Every navigation re-runs `getServerSideProps`, so the value stays current. After login (`router.push(dashboard)`), the new session and CSRF token arrive with the dashboard's props. After logout (`router.push(login)`), `guestProps` returns `null`.

### `lib/api/index.ts`

```ts
// Typed API paths, for SWR keys, module constants and server rendering.
export const paths = {
  projects: projectRoutes,
  auth: authRoutes,
  sessions: sessionRoutes,
};

// The API for components. Mutations send the current session's CSRF token.
// Call it in the component body and use the result in handlers.
export function useApi(): ApiClient {
  const csrfToken = useOptionalSession()?.csrfToken;
  return useMemo(() => client(browserHttp(csrfToken)), [csrfToken]);
}

// Builds the API on a transport; lib/server.ts uses it with serverHttp.
export function client(http: Http) {
  return {
    get: <T>(path: ApiPath<T>) => http.get(path),
    projects: () => projects(http),
    auth: () => auth(http),
    sessions: () => sessions(http),
  };
}
```

Resource clients drop `routes()`; `paths.projects` is the same object.

### The SWR hook (today's `lib/use-api.ts`)

```ts
export function useResource<T>(path: ApiPath<T>, initial?: Initial<T>) {
  const api = useApi();
  const router = useRouter();
  const userId = useOptionalSession()?.user.id;
  const rendered = initial?.path === path;
  const { data, error, isValidating, mutate } = useSWR<T>(
    [path, userId],
    () => api.get(path),
    {
      fallbackData: rendered ? (initial.data ?? undefined) : undefined,
      onError(error) {
        if (error instanceof ApiError && error.status === 401)
          void router.replace(loginPath(router.asPath));
      },
    },
  );
  // …unchanged: keep the last data, error, loading, refreshing, refresh
}
```

### Guest pages (`lib/server.ts`)

```ts
// getServerSideProps for guest pages: `session` is null when signed out.
// `redirectSignedIn` sends signed-in visitors to the dashboard (login, register).
export async function guestProps(
  context: GetServerSidePropsContext,
  { redirectSignedIn = false } = {},
): Promise<GetServerSidePropsResult<GuestPageProps>>
```

The redirect happens in `getServerSideProps`, so a signed-in visitor gets a 307 before any HTML is sent. It goes to `returnPath(context.query.next) ?? routes.dashboard`. Guest pages that read the session are `private, no-store`. If `me()` fails with anything but 401, the page renders signed out.

### Return URL (`?next=`)

`next` is where to go after signing in. It is only ever used as a page navigation (`router.push` or a `getServerSideProps` redirect), both of which are GET requests, so it can't submit anything. Mutations also need POST, an exact Origin and the CSRF token on the API side.

The risk is an open redirect (`?next=https://evil.example`, `//evil.example`, `/\evil.example`). So `next` is accepted only when it parses to one of our signed-in page routes; anything else falls back to the dashboard.

```ts
// lib/routes.ts
// Pages a sign-in may return to: signed-in pages only, matched exactly.
const returnRoutes = [
  /^\/app\/dashboard$/,
  /^\/app\/projects$/,
  /^\/app\/projects\/[1-9]\d*$/,
  /^\/app\/settings$/,
];

const origin = "http://forma.invalid";

// A same-site path and query from ?next=, or null. Absolute and
// protocol-relative URLs change the origin and are rejected; URL parsing
// also resolves "..", so "/app/../auth/login" can't slip through.
export function returnPath(next: unknown): string | null {
  if (typeof next !== "string" || next.length > 512) return null;
  const url = new URL(next, origin);
  if (url.origin !== origin) return null;
  if (!returnRoutes.some((route) => route.test(url.pathname))) return null;
  return url.pathname + url.search;
}

// The login page, returning to `path` afterwards when it's allowed.
export function loginPath(path: string): string {
  const next = returnPath(path);
  return next ? `${routes.login}?next=${encodeURIComponent(next)}` : routes.login;
}
```

- `appProps` redirects a 401 to `loginPath(context.resolvedUrl)`; `useResource` does the same with `router.asPath`.
- `useAuthSubmit` pushes `returnPath(router.query.next) ?? routes.dashboard` after signing in.
- Login and register link to each other with the same `next` (`authPath(routes.register, next)`).
- The query string is kept (`/app/projects?status=Planned&page=2`); each page already validates its own query (`projectQuery`, `parseId`).
- A new signed-in page adds its pattern to `returnRoutes`; a unit test checks every `routes` entry under `/app` has one.

## Call sites

### Dashboard

```tsx
const recentPath = paths.projects.index({ perPage: 5 });

function countPath(status: Status) {
  return paths.projects.index({ status, perPage: 1 });
}

function DashboardContent({ recent, counts }: DashboardProps) {
  const session = useSession();
  const list = useResource(recentPath, recent);
  // …
  <ProjectDialog editor={editor} />
}

function StatusStat({ status, total, initial }: StatusStatProps) {
  const count = useResource(countPath(status), initial);
  // …
}
```

### Project dialog

```tsx
function ProjectForm({ open, project, onClose, onSaved }: ProjectFormProps) {
  const api = useApi();
  // …
  async function submit(event: FormEvent) {
    // …
    const saved = project
      ? await api.projects().update(project.id, form)
      : await api.projects().create(form);
  }
}
```

### Settings and sign-out

```tsx
function SessionControls() {
  const api = useApi();
  const session = useSession();
  async function revokeOthers() {
    // …
    await api.sessions().destroyOthers();
  }
}

// application-layout.tsx
const api = useApi();
async function signOut() {
  await api.auth().logout();
  await router.push(routes.login);
}
```

### Login and register

```tsx
export function getServerSideProps(context: GetServerSidePropsContext) {
  return guestProps(context, { redirectSignedIn: true });
}

export default function Login() {
  const api = useApi();
  const { busy, error, submit } = useAuthSubmit(api.auth().login);
  // …
}
```

### A guest page

```tsx
function AccountLink() {
  return useIsGuest()
    ? <Button href={routes.login}>Log in</Button>
    : <Button href={routes.dashboard}>Dashboard</Button>;
}
```

### Server rendering

```ts
return appProps(context, async (api) => ({
  project: await initial(api, paths.projects.show(id), "We couldn’t load this project. Please try again."),
}));
```

### Summary

| File | Change |
| --- | --- |
| `lib/session.tsx` | New: `SessionProvider`, `useSession`, `useIsGuest`, `useOptionalSession` |
| `lib/api/index.ts` | `useApi()`, `paths`; browser `api()` removed |
| `lib/use-api.ts` | Renamed to the SWR hook (`useResource`), `(path, initial?)` |
| `lib/server.ts` | `guestProps`; 401 redirects use `loginPath`; server loaders use `paths` |
| `lib/routes.ts` | `returnPath`, `loginPath` |
| `lib/use-auth-submit.ts` | Goes to `returnPath(next)` or the dashboard |
| `pages/_app.tsx` | `SessionProvider` around every page |
| `pages/app/*.tsx` | `paths`, the SWR hook without `userId`, no `session`/`csrfToken`/`userId` props |
| `pages/auth/login.tsx`, `register.tsx` | `guestProps(..., { redirectSignedIn: true })`, `useApi()` |
| `components/app/projects/project-dialog.tsx` | `useApi()`, no `csrfToken` prop |
| `components/app/projects/use-project-editor.ts` | `paths.projects.matches` |
| `components/layout/application-layout.tsx` | `useSession()`, `useApi()`, no `session` prop |
| `docs/ui/api.md`, `plans/features/api-client.md` | Updated for `useApi()` and `paths` (replaces api-client decision 1) |

Page props keep `session`: that's how it gets from the server to the provider.

## Tests

- Vitest: `useSession` throws without a session; `useIsGuest`; `useApi` sends the CSRF token only when signed in; `useResource`'s key includes the user ID.
- Vitest: `returnPath` accepts each signed-in route (with a query) and rejects `https://…`, `//…`, `/\…`, `/auth/login`, `/api/…`, `/app/projects/0`, `/app/../auth/login`, non-strings and over-long values; every `/app` route has a return pattern.
- E2E: a signed-in visit to `/auth/login` lands on the dashboard; visiting `/app/projects?status=Planned` signed out, then signing in, returns there; `?next=https://example.com` lands on the dashboard; logout clears the session.
