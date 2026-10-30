# Talking to the API

All API calls go through the typed client in `src/lib/api/`, with one file per resource. Nothing else calls `fetch` or builds `/api/...` paths.

```ts
const api = useApi();                                    // in a component body
const { id: ws } = useWorkspace();                       // projects belong to a workspace
api.projects(ws).index({ status: "Planned", page: 2 })   // Promise<Page<Project>>
api.projects(ws).show("prj_j292s2S2j7Qx4B")              // Promise<Project>
api.projects(ws).create(input)                           // input: ProjectInput
api.workspaces().create({ name: "Acme" })                // Promise<Workspace>, you're the owner
paths.projects(ws).show(id)                              // ApiPath<Project>: "/api/workspaces/wsp_…/projects/prj_…"
```

## Conventions

| Method | HTTP | Returns |
| --- | --- | --- |
| `index(query?)` | `GET /api/<resource>?…` | `Page<T>` ([pagination.md](pagination.md)) |
| `show(id)` | `GET /api/<resource>/{id}` | `T` |
| `create(input)` | `POST /api/<resource>` | `T` |
| `update(id, input)` | `PATCH /api/<resource>/{id}` | `T` |
| `destroy(id)` | `DELETE /api/<resource>/{id}` | `void` |

- `paths.<resource>` has a path helper for each method, plus `matches(key)` to recognise the resource's paths. **Workspace resources take the workspace ID explicitly**, both in `paths.projects(ws)` and `api.projects(ws)`, so the same paths work in components, SWR keys and route loaders.
- Resources implement only what the API supports. Projects have `destroy` commented out until a delete endpoint exists.
- **`index()` takes a typed query object** (`ProjectIndexQuery`: `page`, `perPage`, `q`, `status`, `sort`). Unknown keys and values the API would reject are type errors. Defaults are left out, so each query has one path.
- **Bodies contain only the editable fields** (`create`/`update` pick them from the input), because the API rejects unknown fields.

| Resource | Methods |
| --- | --- |
| `workspaces()` | `index({ page, perPage })`, `show(id)`, `create({ name })`, `update(id, { name })` (owners and admins) |
| `projects(workspace)` | `index`, `show`, `create`, `update` (`destroy` commented out) |
| `auth()` | `me`, `login(credentials)`, `register(registration)`, `logout()` |
| `sessions()` | `destroyOthers()`, which signs out every session except this one |

## Calling the API: `useApi()`

- **`const api = useApi()`** in a component body, then `await api.projects(workspace.id).create(input)` in handlers. It is a hook, so it can't be called in a handler, a condition or at module level.
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
list.refreshing  // revalidating in the background → <RefreshButton>
list.refresh()   // Retry, refresh, or after a mutation
```

- **SWR key** is `[path, userId]`, with the user ID from the session context, so one user never sees another's cached data.
- **Pages fetch their own data**: there is no server rendering, so a page renders at once with skeletons. `useResource(path, fallback)` shows `fallback` (e.g. a record from an already loaded list) until the fetch returns; a `null` path fetches nothing.
- **Changing path** keeps the previous data in `data` while `loading` is true, and keeps it if the new request fails. Show it, dimmed, with an error `Notice` that offers Retry.
- **`loading`** is true only when there is no data yet for this path. `refreshing` is true while revalidating.
- **401** redirects to `/auth/login?next=<this page>` ([pages.md](pages.md#returning-after-sign-in-next)).

## Changing data

- **Run mutations through `useAction`** (`lib/use-action.ts`), not hand-written `try/catch/finally`:

  ```tsx
  const save = useAction((input: ProjectInput) => api.projects(workspace.id).create(input));
  const result = await save.run(form);   // { ok: true, value } | { ok: false }; never throws
  <ErrorNotice error={save.error} />     // the last failure, announced again if it repeats
  <Button type="submit" loading={save.loading}>Create project</Button>
  ```

  `run` ignores calls while one is in flight (a double click submits once) and awaits everything the action awaits, so an action that ends with `navigate(...)` stays loading until the next page has loaded. A failure goes to `save.error` for `<ErrorNotice>`, unless `onError` handles it and returns `true`: an action that is only a button, with no fields (sign out other sessions), shows `toast.error(errorMessage(e))` there instead; and when the API names the field (`ApiError.field`, e.g. the change-password dialog's `currentPassword`), show the message on that field with `ErrorMessage`.
- **After a project is saved,** `useProjectEditor` revalidates every cached key that `paths.projects(workspace.id).matches()` (that workspace's lists, counts and single projects), because one change can move a project between pages, filters and counts. Do the same for new resources.
- **After a workspace is created or renamed,** call `useRefreshWorkspaces()`'s function, with the workspace's ID after a rename, so the switcher and the current workspace reload.
- **Success messages are toasts:** `toast.success("Workspace renamed.")` ([design.md](design.md#toasts)), not a `done` state and an inline Notice.

## Adding a resource

1. `src/lib/api/<resource>.ts`: an `<Resource>IndexQuery` type, exported `<resource>Routes` (with `path<T>()` for GET paths), and the methods the API supports.
2. Add it to `client()` and `paths` in `src/lib/api/index.ts`, and export its query type.
3. Tests next to it: paths, methods and bodies, and `@ts-expect-error` checks for rejected queries.

## IDs

IDs are public ID strings (`usr_…`, `wsp_…`, `prj_…`; [../api/database.md](../api/database.md#public-ids)), typed as `string` in `src/types`. Parse them from URLs with `parsePublicId(prefix, value)` (`lib/ids.ts`), which matches the API's check.
