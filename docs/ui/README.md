# UI overview

React 18, strict TypeScript, Next.js Pages Router with server rendering on every request, Tailwind CSS v4 on top of BaseUI behaviour, SWR, Heroicons, `motion`, and Inter (self-hosted). Code is in `apps/web/src`.

## Where code goes

| Path | Holds |
| --- | --- |
| `pages/auth/*` | Signed-out pages (`/auth/login`, `/auth/register`) |
| `pages/app/*` | Signed-in pages (`/app/dashboard`, `/app/projects`, `/app/projects/[id]`, `/app/settings`) |
| `components/ui/*` | Reusable Tailwind components on BaseUI ([design.md](design.md)) |
| `components/layout/*` | Page chrome: `application-layout`, `sidebar-layout`, `auth-layout`, `status-page` |
| `components/app/<feature>/*` | Feature UI shared by several pages (e.g. `projects`: table parts, dialog, editor hook, query helpers) |
| `lib/*` | Non-visual helpers: `api`, `server`, `use-api`, `pagination`, `ids`, `routes`, `baseui`, `interactive`, `styletron` |
| `types/*` | Domain types and constants per resource (e.g. `types/projects.ts`) |
| `styles/*` | `globals.css` imports `theme.css` (font) and `base.css` (element defaults) |
| `proxy.ts` | Per-request CSP nonce and security headers |

## Rules of thumb

- **Content used by one page stays in that page file**, split into named local functions. Move it to `components/` once a second page needs it.
- **Reuse before adding.** Extend an existing `components/ui` primitive before writing new markup. Use BaseUI for interactive controls and overlays.
- **Routes** live in `lib/routes.ts` (`routes` for pages, `apiRoutes` for the API). Redirects live in `next.config.mjs`.
- **Every async UI handles pending, success, failure and empty states**: `<Button loading>`, `ListSkeleton`, `Notice` with Retry, `EmptyState`.
- **Accessibility:** keyboard navigation, visible focus, accessible names, announced errors and status, and reduced motion.

## Dependencies

- BaseUI compatibility overrides in `package.json` unify the React 18 types and pin patched minor versions of BaseUI's older dependencies. Check them when upgrading BaseUI.
- Don't silence type errors with `any` or unsafe casts. Fix the dependency instead.
- Run `npm audit` after dependency changes.

## More

[pages.md](pages.md) · [api.md](api.md) · [pagination.md](pagination.md) · [design.md](design.md) · [testing.md](testing.md)
