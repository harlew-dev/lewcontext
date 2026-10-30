# Talking to the API

The browser calls the API through the Next proxy at `/api/*` (`next.config.mjs` rewrites to `API_URL`). On the server, pages fetch it directly with the request's cookie (see [pages.md](pages.md)).

## `request()` (`lib/api.ts`)

```ts
const project = await request<Project>(apiRoutes.project(id));
const saved = await request<Project>(apiRoutes.projects, { method: "POST", body: JSON.stringify(input) }, session.csrfToken);
```

- Sends same-origin cookies and a 12 s timeout. It sets `Content-Type: application/json` when there is a body, and `X-CSRF-Token` when you pass the token.
- Throws `ApiError(message, status)` with the API's safe message. `errorMessage(e)` turns any error into display text.
- The CSRF token comes from the session in page props (`session.csrfToken`). Never put it in a URL or localStorage.

## Reading data: `useApi()` (`lib/use-api.ts`)

```ts
const list = useApi(projectsApiPath(query), session.user.id, props.projects);
// list.data, list.error, list.loading, list.refreshing, list.refresh()
```

- **SWR key** is `[path, userId]`. The user ID is always part of it.
- **Server-rendered data** (`Initial<T>` from props) is used while `path` matches the path it was rendered for.
- **Changing path** (another page or filter) keeps the previous data in `data` while `loading` is true, and keeps it if the new request fails. Show it, dimmed, with an error `Notice` that offers Retry.
- **`loading`** is true only when there is no data yet for this path. `refreshing` is true while revalidating.
- **401** redirects to `/auth/login`.

## Changing data

- Disable duplicate submits with `<Button loading={saving}>`. Show API errors in the form with `Notice variant="error"`.
- **After a project is saved,** `useProjectEditor` revalidates every cached key starting with `/api/projects` (lists, counts and single projects), because one change can move a project between pages, filters and counts. Do the same for new resources: revalidate by key prefix rather than patching one list by hand.
- Success messages use `Notice variant="success"` (announced as status).

## IDs

IDs are JSON numbers, so the frontend can represent them exactly only up to `Number.MAX_SAFE_INTEGER`. Parse IDs from URLs with `parseId()` (`lib/ids.ts`). If IDs ever exceed 2⁵³, the API would need to send them as strings.
