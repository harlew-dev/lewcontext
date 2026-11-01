# UI overview

React 18, strict TypeScript, a single-page app on React Router v7 (framework mode, `ssr: false`, public pages prerendered at build time) built by Vite, Tailwind CSS v4 on top of BaseUI behaviour, SWR, Heroicons, `motion`, and Inter (self-hosted). Code is in `apps/web/src`.

## Where code goes

| Path | Holds |
| --- | --- |
| `routes.ts` | The page registry: each URL and its file under `pages/` ([pages.md](pages.md)), in two lists, the app's and the admin dashboard's, one per build |
| `../web-build.ts` | `WEB_BUILD`: which build `react-router build` makes, `app` (the default) or `admin` ([../api/admin.md](../api/admin.md#the-admin-build)), read by `routes.ts`, `react-router.config.ts` and `vite.config.ts` |
| `root.tsx` | The HTML document, providers, default title/description, error boundary (which reports crashes, as does the listener for uncaught errors it installs: `lib/client-errors.ts`) |
| `entry.client.tsx`, `entry.server.tsx` | Hydration in the browser; build-time rendering of prerendered pages with Styletron collection |
| `pages/home.tsx` | The public homepage (`/`, prerendered) |
| `pages/auth/*` | Signed-out pages (`/auth/login`, `/auth/register`, `/auth/forgot-password` and its link page `forgot-password/[token]`), and the invitation link page `invitations/[token]`, which works signed in too |
| `pages/app/*` | Signed-in pages: `layout.tsx` (the layout route), `/app` (redirect), `/app/onboarding`, `/app/settings`, and per workspace `/app/[workspace]/dashboard`, `projects`, `projects/[project]`, `settings` ([pages.md](pages.md)) |
| `pages/admin/*` | The admin dashboard at `/admin/<key>`, its own build: the sign-in page `[key]/index.tsx`, and inside `layout.tsx` (`AdminLayout`) `[key]/users`, `users/[user]`, `workspaces`, `workspaces/[workspace]`, `events`, `events/[event]`, `mail`, `email-blocks`, `admins`, `monitoring/` (`index`, `security`, `limits`, `ips`, `ips/[ip]`, `accounts`, `workspaces`, `usage`); `not-found.tsx`, the admin build's every-other-URL page, which loads links to the app as a new page ([../api/admin.md](../api/admin.md#web-app)) |
| `components/ui/*` | Reusable Tailwind components on BaseUI ([design.md](design.md)) |
| `components/layout/*` | Page chrome: `application-layout`, `admin-layout`, `sidebar-layout`, `auth-layout`, `status-page`, `page-transition` |
| `components/app/<feature>/*` | Feature UI shared by several pages (`projects`: table parts, dialog, editor hook, query helpers; `workspaces`: avatar, name field, create form and dialog, role badge, settings tabs; `account`: the password fields shared by the change-password dialog and the reset link page, and the delete-account dialog shared by account settings and onboarding; `mfa`: the QR code, the two-factor setup parts (the scan step, the code field, the authenticator-or-recovery code field and the recovery code list) and the sign-in's code step (`CodeStep`), shared by account settings, login, the admin door and the admin step-up; `admin`: the admin lists' URL state, sortable header and badges, shared by Users, Workspaces and Email blocks, the delivery status badge, the events table and cursor navigation, shared by Events, an event's page and Mail, the Events filters' URL state (`event-filters.ts`), shared by Events, an event's page and the pages linking in, the Monitoring pages' header, tiles, charts and states (`monitoring.tsx`), and `AdminActionDialog`, an action's reason and step-up; `invitations`: the callout shared by the invitation page, login and register) |
| `lib/api/*` | The typed API client, one file per resource: `const api = useApi()`, `api.projects(ws).store()`, `paths.projects(ws).index()`, `api.workspaces().store()` ([api.md](api.md)); the admin dashboard's is `lib/api/admin.ts` alone (`useAdminApi`, `adminPaths`) |
| `lib/*` | Other non-visual helpers: `session` (`useSession`), `admin` (`AdminProvider`, `useAdminKey`, `useAdminSession`, `usePreviousAdminKey`, `adminCan`: the admin pages' key, session, role and whether the key is being retired; `adminLoginLoader`, `adminLoader`), `admin-routes` (`adminRoutes`, `adminNavigation`, `adminMonitoringNavigation`: the admin pages' URLs and sidebar), `use-admin-resource` (`useAdminResource`, SWR for admin paths), `use-action` (`useAction` for mutations), `use-clear-cache` (`useClearCache`: forget every cached response after signing out or deleting the account), `use-delayed-loading` (`useDelayedLoading`: show a loading state only after 200 ms), `use-docked-sidebar` (`useDockedSidebar`: the desktop sidebar, remembered on this device), `use-open-count` (`useOpenCount`: key a dialog per opening so its form starts empty), `format` (`formatDate`, `formatDateTime`, in UTC), `workspace` (`useWorkspace`, `useWorkspaceList`), `loaders` (route loaders), `use-resource`, `use-resource-pages`, `pagination`, `ids`, `routes`, `return-focus`, `baseui`, `interactive`, `styletron` |
| `types/*` | Domain types and constants per resource (`types/projects.ts`, `types/workspaces.ts`) |
| `styles/*` | `globals.css` imports `theme.css` (font) and `base.css` (element defaults) |

## Rules of thumb

- **Content used by one page stays in that page file**, split into named local functions. Move it to `components/` once a second page needs it.
- **Reuse before adding.** Extend an existing `components/ui` primitive before writing new markup. Use BaseUI for interactive controls and overlays.
- **Admin code stays in the admin build** ([../../plans/features/admin/README.md](../../plans/features/admin/README.md), rule 7): only `pages/admin`, `components/app/admin`, `components/layout/admin-layout.tsx` and the admin modules (`lib/admin.tsx`, `lib/admin-routes.ts`, `lib/use-admin-resource.ts`, `lib/api/admin.ts`, `types/admin.ts`) may import admin code. ESLint refuses any other import of them (static or `import()`), the app build fails if it bundles one (`no-admin-modules`, `vite.config.ts`), and `scripts/check-app-build.sh` fails it if `/admin/`, `/api/admin`, `X-Admin-Key`, `pages/admin` or `admin-assets` shows up in its files, in any case.
- **Page routes** are registered in `routes.ts`, with their URLs in `lib/routes.ts` (the admin dashboard's in `lib/admin-routes.ts`). CSP and security headers come from the Go server (`apps/api/internal/web`). **API paths and calls** live only in `lib/api/<resource>.ts`. Don't call `fetch` or write `/api/...` strings anywhere else.
- **Every async UI handles pending, success, failure and empty states**: `<Button loading>`, `ListSkeleton`, `Notice` with Retry, `toast` for finished actions, `EmptyState`.
- **Accessibility:** keyboard navigation, visible focus, accessible names, announced errors and status, and reduced motion.

## Dependencies

- BaseUI compatibility overrides in `package.json` unify the React 18 types and pin patched minor versions of BaseUI's older dependencies. Check them when upgrading BaseUI.
- Don't silence type errors with `any` or unsafe casts. Fix the dependency instead.
- **`qrcode-generator`** (MIT, no dependencies) draws the two-factor QR code as an SVG path in the page (`components/app/mfa/qr-code.tsx`), so the CSP needs no image or inline-HTML exception.
- **`baseui/datepicker`** (part of `baseui`, with its `date-fns` adapter: no new dependency) is behind `components/ui/calendar.tsx`, used only by the admin Events page's custom range for now, so it is only in the admin build.
- **`recharts`** (MIT), pinned at 3.10.1 with its peer `react-is` 18.3.1 (matching React), draws charts as SVG. Only `components/ui/chart.tsx` imports it, and only admin pages use that, so it is only in the admin build. It sets styles through React (`element.style`), which the CSP's `style-src 'unsafe-inline'` allows, and uses no `eval`. Chosen in [events and mail](../../plans/features/implemented/admin/events-and-mail.md#charts-decision-12) decision 12.
- Run `npm audit` after dependency changes.
- **React Router stays on v7.** v8 needs React 19, and BaseUI 18 is used with React 18. React Router v7 supports React 18 and Vite 8.
- **Lint rules** are those eslint-config-next applied before the move (TypeScript, React, the hooks rules and its subset of jsx-a11y), configured directly in `eslint.config.mjs`.

## More

[pages.md](pages.md) · [api.md](api.md) · [pagination.md](pagination.md) · [design.md](design.md) · [testing.md](testing.md) · [base-ui.md](base-ui.md) · [strict-mode.md](strict-mode.md)
