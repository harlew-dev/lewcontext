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

- `paths.<resource>` has a path helper for each method, plus `matches(key)` to recognise the resource's paths. **Workspace resources take the workspace ID explicitly**, both in `paths.projects(ws)` and `api.projects(ws)`, so the same paths work in components, SWR keys and `getServerSideProps`.
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
- **CSRF is automatic.** `useApi()` reads the session's token from the session context ([pages.md](pages.md#the-session)) and sends it as `X-CSRF-Token` on mutations. Signed out (login, register), it sends none. The client is rebuilt only when the token changes. Never put the token in a URL or localStorage.
- **Browser calls** go through the same-origin `/api` proxy with the session cookie and a 12 s timeout.
- **Outside components** you only need `paths` (module constants, SWR keys, server loaders). There is no browser-side `api()` function: a module-level client would have to hold a token, and on the server module state is shared between users.

## Typed paths

`paths` returns `ApiPath<T>`: a string that also carries its response type (only at compile time). Anything that loads a path infers its type from it, so nobody writes `<Page<Project>>` by hand and a path can't be paired with the wrong type.

```ts
const list = useResource(paths.projects(ws).index(query), props.projects); // list.data: Page<Project>
await api.get(path); // load any typed path
```

## Reading data: `useResource()` (`lib/use-resource.ts`)

`useApi()` is for doing something on a click or submit; `useResource()` is for data a component displays. It wraps SWR (cache, background refetch, retry, refetch on focus):

```ts
const list = useResource(path, props.projects);
list.data        // the last loaded data, kept while a new path loads
list.loading     // nothing loaded for this path yet → <ListSkeleton />
list.error       // message → <Notice variant="error"> with Retry
list.refreshing  // revalidating in the background → <RefreshButton>
list.refresh()   // Retry, refresh, or after a mutation
```

- **SWR key** is `[path, userId]`, with the user ID from the session context, so one user never sees another's cached data.
- **Server-rendered data** (`Initial<T>` from props) is used while `path` matches the path it was rendered for.
- **Changing path** keeps the previous data in `data` while `loading` is true, and keeps it if the new request fails. Show it, dimmed, with an error `Notice` that offers Retry.
- **`loading`** is true only when there is no data yet for this path. `refreshing` is true while revalidating.
- **401** redirects to `/auth/login?next=<this page>` ([pages.md](pages.md#returning-after-sign-in-next)).

## On the server

`serverApi(context)` (`lib/server.ts`) builds the same client on a server transport: the fixed `API_URL`, only the incoming cookie, an 8 s timeout, and a new instance per request. `appProps` hands it to the page's loader ([pages.md](pages.md)):

```ts
appProps(context, async (api, workspace) => ({
  projects: await initial(api, paths.projects(workspace.id).index(query), "We couldn’t load your projects."),
}));
```

## Changing data

- Disable duplicate submits with `<Button loading={saving}>`. Show API errors (`ApiError`, `errorMessage(e)`) in the form with `Notice variant="error"`.
- **After a project is saved,** `useProjectEditor` revalidates every cached key that `paths.projects(workspace.id).matches()` (that workspace's lists, counts and single projects), because one change can move a project between pages, filters and counts. Do the same for new resources.
- **After a workspace is created or renamed,** call `useRefreshWorkspaces()`'s function so the switcher reloads, and rerun the page's props (`router.push` to the new workspace, or `router.replace(router.asPath)`).
- Success messages use `Notice variant="success"` (announced as status).

## Adding a resource

1. `src/lib/api/<resource>.ts`: an `<Resource>IndexQuery` type, exported `<resource>Routes` (with `path<T>()` for GET paths), and the methods the API supports.
2. Add it to `client()` and `paths` in `src/lib/api/index.ts`, and export its query type.
3. Tests next to it: paths, methods and bodies, and `@ts-expect-error` checks for rejected queries.

## IDs

IDs are public ID strings (`usr_…`, `wsp_…`, `prj_…`; [../api/database.md](../api/database.md#public-ids)), typed as `string` in `src/types`. Parse them from URLs with `parsePublicId(prefix, value)` (`lib/ids.ts`), which matches the API's check.
