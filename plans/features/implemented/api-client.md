# Typed API client (`lib/api/*`)

Status: **implemented 2026-10-30.** Two details differ from the design below:

- `initial(api, path, failure)` takes the server client and a typed path; `client()` also exposes `get(path)` to load any typed path.
- The projects page's URL state converts to `ProjectIndexQuery` with `indexQuery()` in `components/app/projects/project-query.ts`.

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | CSRF for mutations | ~~`api(session)` binds the session; `api()` is for reads and routes~~ Replaced by `const api = useApi()` and `paths` ([session-context.md](session-context.md)) |
| 2 | `destroy()` | Written in `lib/api/projects.ts` but **commented out**, with a note that it needs `DELETE /api/projects/{id}`. No Go endpoint now |
| 3 | Auth naming | `api().auth().me() / login() / register()`, `api(session).auth().logout()`, `api(session).sessions().destroyOthers()` |
| 4 | Paths and queries | Typed paths (`ApiPath<T>`), and `index()` takes a **typed query object** per resource |

## Why

Today the web app talks to the API in three different ways:

- `request<T>(path, init, csrf)` with hand-built paths (`apiRoutes.project(id)`) in components and pages,
- path strings as SWR keys (`useApi(path, …)`),
- `initial<T>(get, path, …)` on the server.

The response type is repeated at every call site, and nothing ties a path to its type: `request<Project>(apiRoutes.projects)` compiles even though that route returns a page. One client per resource puts the paths, payload types and response types in a single place:

```ts
api().projects().index({ status: "Planned", page: 2 })   // Promise<Page<Project>>
api().projects().show(42)                                 // Promise<Project>
api(session).projects().create(input)                     // Promise<Project>, input: ProjectInput
api(session).projects().update(42, input)                 // Promise<Project>
api().projects().routes().show(42)                        // "/api/projects/42" (typed path)
```

## Conventions

| Method | HTTP | Route helper | Returns |
| --- | --- | --- | --- |
| `index(query?)` | `GET /api/<resource>?…` | `routes().index(query?)` | `Page<T>` |
| `show(id)` | `GET /api/<resource>/{id}` | `routes().show(id)` | `T` |
| `create(input)` | `POST /api/<resource>` | `routes().create()` | `T` |
| `update(id, input)` | `PATCH /api/<resource>/{id}` | `routes().update(id)` | `T` |
| `destroy(id)` | `DELETE /api/<resource>/{id}` | `routes().destroy(id)` | `void` |

Resources implement only the methods the API supports. Unsupported ones stay commented out with a pointer to the missing endpoint (`destroy` for projects).

### Typed index queries

Each resource exports its own query type, so `index()` and `routes().index()` only accept the parameters the API accepts:

```ts
// lib/api/projects.ts
export type ProjectIndexQuery = {
  page?: number;               // 1–10 000
  perPage?: number;            // 1–100
  q?: string;                  // ≤ 100 characters
  status?: Project["status"];  // "Planned" | "In progress" | "Completed"
  sort?: ProjectSort;          // "name" | "-name" | "status" | … (from types/projects.ts)
};

api().projects().index({ status: "Planned", sort: "-updated", page: 2 }); // ok
api().projects().index({ status: "Archived" });                            // type error
api().projects().index({ owner: 3 });                                      // type error
```

Defaults (page 1, `-updated`, empty search) are left out of the URL, so each query has one path and one SWR key. The projects page converts its URL state (`ProjectQuery`, where `status` can be `"all"`) into a `ProjectIndexQuery`.

### Typed paths

`routes()` returns `ApiPath<T>`: a string that also carries its response type (`string & { readonly response?: T }`). `useApi` and server rendering infer the type from the path, so nobody writes `<Page<Project>>` by hand and a path can't be paired with the wrong type:

```ts
const list = useApi(api().projects().routes().index(query), session.user.id, props.projects); // data: Page<Project>
```

## Files

```
src/lib/api/
  index.ts      api(session?) → { projects(), auth(), sessions() }; ApiError, errorMessage
  http.ts       transports: browser (same-origin, CSRF header, 12 s) and server (fixed API_URL, request cookie only, 8 s)
  paths.ts      ApiPath<T>, the path() helper
  projects.ts   projects(http): routes(), index, show, create, update
  auth.ts       auth(http): me, login, register, logout · sessions(http): destroyOthers
```

- **One client, two transports.** In the browser, `api()` uses relative `/api/*` URLs through the Next proxy. On the server, `serverApi(context)` from `lib/server.ts` uses the same resource files with the fixed `API_URL` and only the incoming cookie, created fresh per request (AGENTS rule 7).
- **CSRF:** mutations need the session's token. `api(session)` binds it; `api()` without a session is enough for reads and routes (decision 1).
- **API paths move out of `lib/routes.ts`.** `apiRoutes` is deleted, and each resource file owns its paths. `lib/routes.ts` keeps the page routes and navigation only.
- **Query mapping for `index`** (dropping `status: "all"`, the default sort and page 1) moves into `lib/api/projects.ts`. `components/app/projects/project-query.ts` keeps only URL-state parsing (`projectQuery`, `projectsPath`, `cleanSearch`).

## What changes

| File | Today | After |
| --- | --- | --- |
| `lib/api.ts` | `request`, `ApiError`, `errorMessage` | Replaced by `lib/api/` (same `@/lib/api` import path) |
| `lib/routes.ts` | `routes`, `navigation`, `apiRoutes` | `apiRoutes` removed |
| `lib/server.ts` | `apiGet(context)`, `initial(get, path, failure)` | `serverApi(context)`, `initial(path, load, failure)` with a typed path |
| `lib/use-api.ts` | `useApi<T>(path: string, …)` | `useApi(path: ApiPath<T>, …)`, type inferred |
| `lib/use-auth-submit.ts` | Posts to an endpoint string | Takes `api().auth().login` / `.register` |
| `pages/auth/login.tsx`, `register.tsx` | `useAuthSubmit(apiRoutes.login)` | `useAuthSubmit(api().auth().login)` |
| `pages/app/projects.tsx` | `projectsApiPath(query)`, `initial<Page<Project>>` | `api().projects().routes().index(…)`, `serverApi(context).projects().index(…)` |
| `pages/app/projects/[id].tsx` | `apiRoutes.project(id)`, `initial<Project>` | `routes().show(id)` / `.show(id)` |
| `pages/app/dashboard.tsx` | `url(apiRoutes.projects, { perPage: 5 })` etc. | `routes().index({ perPage: 5 })`, `routes().index({ status, perPage: 1 })` |
| `pages/app/settings.tsx` | `request(apiRoutes.sessions, { method: "DELETE" }, csrf)` | `api(session).sessions().destroyOthers()` |
| `components/layout/application-layout.tsx` | `request(apiRoutes.logout, …)` | `api(session).auth().logout()` |
| `components/app/projects/project-dialog.tsx` | `request<Project>(project ? … : …, { method, body }, csrf)` | `api(session).projects().update(id, input)` / `.create(input)` |
| `components/app/projects/project-query.ts` | `projectsApiPath` | Removed (moved to `lib/api/projects.ts`) |
| `components/app/projects/use-project-editor.ts` | `key.startsWith(apiRoutes.projects)` | `api().projects().routes().matches(key)` (or a `base` path) |
| Tests | `lib/api.test.ts`, `projectsApiPath` in `project-table.test.tsx` | `lib/api/*.test.ts`: transports (CSRF, credentials, errors), project routes and methods, the server transport forwarding only the cookie |
| Docs | `ui/api.md`, `ui/pages.md`, `ui/pagination.md`, `ui/README.md`, AGENTS.md rule 4 ("route constants") | Describe `lib/api/*` and the method conventions |

The Go API doesn't change. Playwright tests shouldn't need changes because behaviour stays the same.

## Is it better?

**Yes.**

- **Single source of truth:** one file per resource owns its paths, payloads and responses.
- **Fewer type mistakes:** a path can't be paired with the wrong response type.
- **Consistent everywhere:** the same calls work in components, hooks and server rendering.
- **Simple to extend:** a new resource is one file plus a line in `api()`.

**Trade-offs:**

- It's a small layer to learn.
- `api(session)` has to be passed where mutations happen.
- The `ApiPath<T>` type trick relies on a phantom type field, which is unusual but entirely compile-time.
