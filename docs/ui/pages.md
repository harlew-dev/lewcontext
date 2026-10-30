# Pages and routing

## Signed-in pages

Each page under `pages/app/` renders only its content and sets `Page.getLayout = appLayout`. `_app` wraps it in `ApplicationLayout`, which stays mounted between pages, so sidebar state and the sliding current-page indicator survive navigation.

```tsx
type TasksProps = SessionProps & { tasks: Initial<Page<Task>> };

export function getServerSideProps(context: GetServerSidePropsContext) {
  const path = url(apiRoutes.tasks, { page: page(context.query) });
  return appProps(context, async (get) => ({
    tasks: await initial<Page<Task>>(get, path, "We couldn’t load your tasks. Please try again."),
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
- forwards **only** the request cookie to the fixed `API_URL`, with a fresh fetch per request,
- loads the session (`/api/auth/me`): 401 → redirect to `/auth/login`, any other failure → 503 `ServiceUnavailable`,
- runs the page's `load(get)`: an API 404 → the Next 404 page, 401 → login.

`initial(get, path, failure)` server-renders one API path. A failure becomes `{ data: null, error }`, so the page still renders and the client retries. Pages that only need the session use `sessionProps`.

Never cache user data globally or reuse an authenticated fetch between requests. Styletron collects styles per request (`_document.tsx`).

## Routes

- **Page URLs:** `routes` in `lib/routes.ts`, e.g. `routes.project(id)` → `/app/projects/42`.
- **API paths:** `apiRoutes`, e.g. `apiRoutes.project(id)` → `/api/projects/42`.
- **Redirects** from `/`, `/app`, `/auth` and pre-nesting URLs (`/login`, `/projects`, `/settings`) live in `next.config.mjs`.
- **Dynamic IDs:** read `[id]` with `parseId(context.params?.id)` from `lib/ids.ts`. It returns `null` (render 404) unless the value is plain digits that JavaScript can represent exactly.
- **Sidebar:** `navigation` in `routes.ts` lists the items. An item is current on its own path and on sub-paths (`/app/projects/42` highlights Projects).

## Signed-out pages

`pages/auth/*` wrap their content in `AuthLayout` and submit with `useAuthSubmit(apiRoutes.login | register)`, which disables the form while pending and opens the dashboard on success.

## Status pages

`ServiceUnavailable` (503, with retry), and `404.tsx` via `StatusPage`. `RouteProgress` shows a top bar for client navigations slower than 200 ms.
