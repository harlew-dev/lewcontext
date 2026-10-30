# UI overview

React 18, strict TypeScript, Next.js Pages Router with server rendering on every request, Tailwind CSS v4 on top of BaseUI behaviour, SWR, Heroicons, `motion`, and Inter (self-hosted). Code is in `apps/web/src`.

## Where code goes

| Path | Holds |
| --- | --- |
| `pages/auth/*` | Signed-out pages (`/auth/login`, `/auth/register`) |
| `pages/app/*` | Signed-in pages: `/app` (redirect), `/app/onboarding`, `/app/settings`, and per workspace `/app/[workspace]/dashboard`, `projects`, `projects/[project]`, `settings` ([pages.md](pages.md)) |
| `components/ui/*` | Reusable Tailwind components on BaseUI ([design.md](design.md)) |
| `components/layout/*` | Page chrome: `application-layout`, `sidebar-layout`, `auth-layout`, `status-page` |
| `components/app/<feature>/*` | Feature UI shared by several pages (`projects`: table parts, dialog, editor hook, query helpers; `workspaces`: avatar, name field, create form and dialog) |
| `lib/api/*` | The typed API client, one file per resource: `const api = useApi()`, `api.projects(ws).create()`, `paths.projects(ws).index()`, `api.workspaces().create()` ([api.md](api.md)) |
| `lib/*` | Other non-visual helpers: `session` (`useSession`), `workspace` (`useWorkspace`, `useWorkspaceList`), `server`, `use-resource`, `use-resource-pages`, `pagination`, `ids`, `routes`, `return-focus`, `baseui`, `interactive`, `styletron` |
| `types/*` | Domain types and constants per resource (`types/projects.ts`, `types/workspaces.ts`) |
| `styles/*` | `globals.css` imports `theme.css` (font) and `base.css` (element defaults) |
| `proxy.ts` | Per-request CSP nonce and security headers |

## Rules of thumb

- **Content used by one page stays in that page file**, split into named local functions. Move it to `components/` once a second page needs it.
- **Reuse before adding.** Extend an existing `components/ui` primitive before writing new markup. Use BaseUI for interactive controls and overlays.
- **Page routes** live in `lib/routes.ts`, and the one redirect (`/` → `/app`) in `next.config.mjs`. **API paths and calls** live only in `lib/api/<resource>.ts`. Don't call `fetch` or write `/api/...` strings anywhere else.
- **Every async UI handles pending, success, failure and empty states**: `<Button loading>`, `ListSkeleton`, `Notice` with Retry, `EmptyState`.
- **Accessibility:** keyboard navigation, visible focus, accessible names, announced errors and status, and reduced motion.

## Dependencies

- BaseUI compatibility overrides in `package.json` unify the React 18 types and pin patched minor versions of BaseUI's older dependencies. Check them when upgrading BaseUI.
- Don't silence type errors with `any` or unsafe casts. Fix the dependency instead.
- Run `npm audit` after dependency changes.

## More

[pages.md](pages.md) · [api.md](api.md) · [pagination.md](pagination.md) · [design.md](design.md) · [testing.md](testing.md)
