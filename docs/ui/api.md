# Talking to the API

All API calls go through the typed client in `src/lib/api/`, with one file per resource. Nothing else calls `fetch` or builds `/api/...` paths.

```ts
const api = useApi();                                    // in a component body
const { id: ws } = useWorkspace();                       // projects belong to a workspace
api.projects(ws).index({ status: "Planned", page: 2 })   // Promise<Page<Project>>
api.projects(ws).show("prj_j292s2S2j7Qx4B")              // Promise<Project>
api.projects(ws).store(input)                            // input: ProjectInput
api.workspaces().store({ name: "Acme" })                 // Promise<Workspace>, you're the owner
paths.projects(ws).show(id)                              // ApiPath<Project>: "/api/workspaces/wsp_…/projects/prj_…"
```

## Conventions

Client methods and typed paths use the API's action names (`index`, `show`, `store`, `update`, `destroy`, plus named verbs such as `accept`); see [../naming.md](../naming.md#web-app).

| Method | HTTP | Returns |
| --- | --- | --- |
| `index(query?)` | `GET /api/<resource>?…` | `Page<T>` ([pagination.md](pagination.md)) |
| `show(id)` | `GET /api/<resource>/{id}` | `T` |
| `store(input)` | `POST /api/<resource>` | `T` |
| `update(id, input)` | `PATCH /api/<resource>/{id}` | `T` |
| `destroy(id)` | `DELETE /api/<resource>/{id}` | `void` |

- `paths.<resource>` has a path helper for each method, plus `matches(key)` to recognise the resource's paths. **Workspace resources take the workspace ID explicitly**, both in `paths.projects(ws)` and `api.projects(ws)`, so the same paths work in components, SWR keys and route loaders.
- Resources implement only what the API supports.
- **`index()` takes a typed query object** (`ProjectIndexQuery`: `page`, `perPage`, `q`, `status`, `sort`). Unknown keys and values the API would reject are type errors. Defaults are left out, so each query has one path.
- **Bodies contain only the editable fields** (`store`/`update` pick them from the input), because the API rejects unknown fields.

| Resource | Methods |
| --- | --- |
| `workspaces()` | `index({ page, perPage, canLeave: false })`, `show(id)`, `store({ name })`, `update(id, { name })` (owners and admins), `destroy(id, { name, password })` (owners), `leave(id)` (`DELETE …/membership`) |
| `projects(workspace)` | `index`, `show`, `store`, `update`, `destroy` (owners and admins) |
| `members(workspace)` | `index({ page, perPage, q })`, `update(id, { role })`, `destroy(id)` (owners and admins) |
| `invitations(workspace)` | `index({ page, perPage })`, `store({ email, role })`, `destroy(id)` (owners and admins) |
| `invitations()` | `check(token)`, `accept(token)`: the link's page, by the token in the body |
| `auth()` | `me`, `login(credentials)` (`needsMfa(result)` when it answers `{mfaRequired}`), `loginMfa(code)`, `register(registration)` (`needsVerification(result)` when it answers `{verificationRequired, email}`), `registerCheck()`, `registerResend()`, `registerVerify(code)` (the sign-up is in its cookie), `logout()`, `destroy({ password })` (deletes the account; `lastOwnerWorkspaces(error)` reads the 409's workspaces) |
| `auth().emailVerification()` | `store()` (emails a code), `update(code)` (`{user}`; a 410 means the code ended). `verificationRequired(error)` recognises the API's 403 for an account that must verify first |
| `auth().mfa()` | `setup(password)` (`{secret, uri}`), `store(code)` (turns it on; `{recoveryCodes}`), `destroy({ password, code })`, `recoveryCodes().store({ password, code })` |
| `sessions()` | `destroyOthers()`, which signs out every session except this one |

The admin dashboard has its own client, `lib/api/admin.ts`: `const api = useAdminApi()` (inside `AdminProvider`) sends the URL's key in `X-Admin-Key` and the admin session's CSRF token, never the app's; `api.login`, `.loginMfaSetup()`, `.loginMfa(code)`, `.logout()`, `.users().index(query)`, `.show(id)`, `.workspaces().index(query)`, `.show(id)`; typed paths under `paths.admin` (the key is never in one); `loaderAdminApi(key)` for its loaders. Pages read with `useAdminResource(path)`, whose SWR key is `["admin", adminUserId, path]` and whose 401 goes back to `/admin/<key>` ([../api/admin.md](../api/admin.md#web-app)).

## Calling the API: `useApi()`

- **`const api = useApi()`** in a component body, then `await api.projects(workspace.id).store(input)` in handlers. It is a hook, so it can't be called in a handler, a condition or at module level.
- **CSRF is automatic.** `useApi()` reads the session's token from the session context ([pages.md](pages.md#the-session-and-the-workspace)) and sends it as `X-CSRF-Token` on mutations. Signed out (login, register), it sends none. The client is rebuilt only when the token changes. Never put the token in a URL or localStorage.
- **Calls** go to the same-origin `/api` (the Go server in production; Vite's dev proxy in development) with the session cookie and a 12 s timeout.
- **Route loaders** run outside components and use `loaderApi()`: the same client without a CSRF token, so it can only read ([pages.md](pages.md#route-loaders-libloadersts)). Elsewhere outside components you only need `paths` (module constants, SWR keys). There is no module-level client holding a token.

## Typed paths

`paths` returns `ApiPath<T>`: a string that also carries its response type (only at compile time). Anything that loads a path infers its type from it, so nobody writes `<Page<Project>>` by hand and a path can't be paired with the wrong type.

```ts
const list = useResource(paths.projects(ws).index(query)); // list.data: Page<Project>
await api.get(path); // load any typed path
```

## Reading data: `useResource()` (`lib/use-resource.ts`)

`useApi()` is for doing something on a click or submit; `useResource()` is for data a component displays. It wraps SWR (cache, background refetch, retry, refetch on focus):

```ts
const list = useResource(path);
list.data        // the last loaded data, kept while a new path loads
list.loading     // nothing loaded for this path yet → <ListSkeleton />
list.error       // message → <Notice variant="error"> with Retry
list.missing     // the API answered 404 → render "not found"
list.refreshing  // revalidating in the background
list.refresh()   // Retry, refresh, or after a mutation
```

- **SWR key** is `[path, userId]`, with the user ID from the session context, so one user never sees another's cached data.
- **Pages fetch their own data**: there is no server rendering, so a page renders at once with skeletons. `useResource(path, fallback)` shows `fallback` (e.g. a record from an already loaded list) until the fetch returns; a `null` path fetches nothing.
- **Polling:** `useResource(path, fallback, { refreshInterval: (data) => ms })` refreshes again after `ms`, or stops at 0: the pending invitations poll every 3 s only while one is "Sending…".
- **Changing path** keeps the previous data in `data` while `loading` is true, and keeps it if the new request fails. Show it, dimmed, with an error `Notice` that offers Retry.
- **`loading`** is true only when there is no data yet for this path. `refreshing` is true while revalidating.
- **401** redirects to `/auth/login?next=<this page>` ([pages.md](pages.md#returning-after-sign-in-next)); a 403 with `verificationRequired` to `/app/verify-email?next=<this page>`.
- **Retries:** a failed read is retried twice (`swrConfig`, `lib/swr.ts`, set in `root.tsx`), except a 4xx: a 403 or 404 won't change, and retrying a 429 would spend more of the rate limit ([../api/rate-limits.md](../api/rate-limits.md)). The API's message ("Too many requests. Try again in 12 minutes.") shows in the error `Notice`, and Retry asks again.

## Changing data

- **Run mutations through `useAction`** (`lib/use-action.ts`), not hand-written `try/catch/finally`:

  ```tsx
  const save = useAction((input: ProjectInput) => api.projects(workspace.id).store(input));
  const result = await save.run(form);   // { ok: true, value } | { ok: false }; never throws
  <ErrorNotice error={save.error} />     // the last failure, announced again if it repeats
  <Button type="submit" loading={save.loading}>Create project</Button>
  ```

  It has two pending flags:

  | Flag | True | Use it for |
  | --- | --- | --- |
  | `busy` | At once, from `run` until the action settles | Logic: refusing to close a dialog mid-save (`onClose={() => !save.busy && onClose()}`, Cancel too) |
  | `loading` | Once `busy` has lasted 200 ms, then for at least 400 ms (`useDelayedLoading`, `lib/use-delayed-loading.ts`) | What the user sees: `<Button loading>`, disabled fields |

  So a quick request shows no spinner and doesn't grey out the form for a moment, and a slower one doesn't flash it for a few milliseconds. A double submit is refused regardless, as soon as `run` starts.

  `run` ignores calls while one is in flight (a double click submits once) and awaits everything the action awaits, so an action that ends with `navigate(...)` stays busy (and loading) until the next page has loaded. A failure goes to `save.error` for `<ErrorNotice>`, unless `onError` handles it and returns `true`: an action that is only a button, with no fields (sign out other sessions), shows `toast.error(errorMessage(e))` there instead; and when the API names the field (`ApiError.field`, e.g. the change-password dialog's `currentPassword`), show the message on that field with `ErrorMessage`.
- **After a project is saved,** `useProjectEditor` revalidates every cached key that `paths.projects(workspace.id).matches()` (that workspace's lists, counts and single projects), because one change can move a project between pages, filters and counts. Do the same for new resources.
- **After a workspace is created or renamed,** call `useRefreshWorkspaces()`'s function, with the workspace's ID after a rename, so the switcher and the current workspace reload.
- **Errors** are `ApiError`s with the message, `status`, the `field` the API named, and `details` (the rest of the body, e.g. the 409's `workspaces`).
- **After signing out or deleting the account,** navigate away, then call `useClearCache()`'s function (`lib/use-clear-cache.ts`), which empties SWR's cache, infinite lists included, so none of the account's data stays in memory.
- **After deleting a project,** `ProjectActions` clears its `show` key without refetching (so Back shows "not found", not stale data) and revalidates every key `projects.matches()`.
- **Success messages are toasts:** `toast.success("Workspace renamed.")` ([design.md](design.md#toasts)), not a `done` state and an inline Notice.

## Adding a resource

1. `src/lib/api/<resource>.ts`: an `<Resource>IndexQuery` type, exported `<resource>Routes` (with `path<T>()` for GET paths), and the methods the API supports.
2. Add it to `client()` and `paths` in `src/lib/api/index.ts`, and export its query type.
3. Tests next to it: paths, methods and bodies, and `@ts-expect-error` checks for rejected queries.

## IDs

IDs are public ID strings (`usr_…`, `wsp_…`, `prj_…`; [../api/database.md](../api/database.md#public-ids)), typed as `string` in `src/types`. Parse them from URLs with `parsePublicId(prefix, value)` (`lib/ids.ts`), which matches the API's check.
