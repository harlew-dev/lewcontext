# UI overview

React 18, strict TypeScript, a single-page app on React Router v7 (framework mode, `ssr: false`, public pages prerendered at build time) built by Vite, Tailwind CSS v4 on top of BaseUI behaviour, SWR, Heroicons, `motion`, and Inter (self-hosted). Code is in `apps/web/src`.

## Where code goes

| Path | Holds |
| --- | --- |
| `routes.ts` | The page registry: each URL and its file under `pages/` ([pages.md](pages.md)) |
| `root.tsx` | The HTML document, providers, default title/description, error boundary |
| `entry.client.tsx`, `entry.server.tsx` | Hydration in the browser; build-time rendering of prerendered pages with Styletron collection |
| `pages/home.tsx` | The public homepage (`/`, prerendered) |
| `pages/auth/*` | Signed-out pages (`/auth/login`, `/auth/register`, `/auth/forgot-password` and its link page `forgot-password/[token]`) |
| `pages/app/*` | Signed-in pages: `layout.tsx` (the layout route), `/app` (redirect), `/app/onboarding`, `/app/settings`, and per workspace `/app/[workspace]/dashboard`, `projects`, `projects/[project]`, `settings` ([pages.md](pages.md)) |
| `components/ui/*` | Reusable Tailwind components on BaseUI ([design.md](design.md)) |
| `components/layout/*` | Page chrome: `application-layout`, `sidebar-layout`, `auth-layout`, `status-page` |
| `components/app/<feature>/*` | Feature UI shared by several pages (`projects`: table parts, dialog, editor hook, query helpers; `workspaces`: avatar, name field, create form and dialog, role badge, settings tabs; `account`: the password fields shared by the change-password dialog and the reset link page, and the delete-account dialog shared by account settings and onboarding) |
| `lib/api/*` | The typed API client, one file per resource: `const api = useApi()`, `api.projects(ws).store()`, `paths.projects(ws).index()`, `api.workspaces().store()` ([api.md](api.md)) |
| `lib/*` | Other non-visual helpers: `session` (`useSession`), `use-action` (`useAction` for mutations), `use-clear-cache` (`useClearCache`: forget every cached response after signing out or deleting the account), `use-delayed-loading` (`useDelayedLoading`: show a loading state only after 200 ms), `use-docked-sidebar` (`useDockedSidebar`: the desktop sidebar, remembered on this device), `format` (`formatDate`, `formatDateTime`, in UTC), `workspace` (`useWorkspace`, `useWorkspaceList`), `loaders` (route loaders), `use-resource`, `use-resource-pages`, `pagination`, `ids`, `routes`, `return-focus`, `baseui`, `interactive`, `styletron` |
| `types/*` | Domain types and constants per resource (`types/projects.ts`, `types/workspaces.ts`) |
| `styles/*` | `globals.css` imports `theme.css` (font) and `base.css` (element defaults) |

## Rules of thumb

- **Content used by one page stays in that page file**, split into named local functions. Move it to `components/` once a second page needs it.
- **Reuse before adding.** Extend an existing `components/ui` primitive before writing new markup. Use BaseUI for interactive controls and overlays.
- **Page routes** are registered in `routes.ts`, with their URLs in `lib/routes.ts`. CSP and security headers come from the Go server (`apps/api/internal/web`). **API paths and calls** live only in `lib/api/<resource>.ts`. Don't call `fetch` or write `/api/...` strings anywhere else.
- **Every async UI handles pending, success, failure and empty states**: `<Button loading>`, `ListSkeleton`, `Notice` with Retry, `toast` for finished actions, `EmptyState`.
- **Accessibility:** keyboard navigation, visible focus, accessible names, announced errors and status, and reduced motion.

## Dependencies

- BaseUI compatibility overrides in `package.json` unify the React 18 types and pin patched minor versions of BaseUI's older dependencies. Check them when upgrading BaseUI.
- Don't silence type errors with `any` or unsafe casts. Fix the dependency instead.
- Run `npm audit` after dependency changes.
- **React Router stays on v7.** v8 needs React 19, and BaseUI 18 is used with React 18. React Router v7 supports React 18 and Vite 8.
- **Lint rules** are those eslint-config-next applied before the move (TypeScript, React, the hooks rules and its subset of jsx-a11y), configured directly in `eslint.config.mjs`.

## More

[pages.md](pages.md) · [api.md](api.md) · [pagination.md](pagination.md) · [design.md](design.md) · [testing.md](testing.md) · [base-ui.md](base-ui.md) · [strict-mode.md](strict-mode.md)
