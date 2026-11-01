# lewcontext docs

Short summaries of how the system works. Start with the project rules in [`AGENTS.md`](../AGENTS.md), then read the doc for the area you're changing.

## API (Go, `apps/api`)

| Doc | Read it when you… |
| --- | --- |
| [api/README.md](api/README.md) | need the package layout, request flow, how Go serves the web app, or environment variables |
| [api/endpoints.md](api/endpoints.md) | call or add an endpoint |
| [api/auth.md](api/auth.md) | touch sign-in, sign-up and email verification, sessions, CSRF or security headers |
| [api/admin.md](api/admin.md) | touch the admin dashboard: its secret key (and the previous one, and the address allowlist), admins and their roles, admin sign-in and sessions, what admins see (Users, Workspaces, Events, Mail, Email blocks, Admins), and its one action, Unblock, behind a fresh code (step-up) and a reason |
| [api/rate-limits.md](api/rate-limits.md) | add an endpoint, a limit or a quota, or touch `RequireSession` |
| [api/events.md](api/events.md) | record something that happened (the `events` audit trail), or add an event type |
| [api/observability.md](api/observability.md) | log anything, or touch the access line, request IDs, a 500, panic recovery, grouped errors, metrics or their labels, `/metrics`, server instances, slow queries, the shutdown order or the health checks (`LOG_FORMAT`, `LOG_LEVEL`, `ACCESS_LOG`, `METRICS_*`, `SLOW_QUERY_MS`) |
| [api/monitoring.md](api/monitoring.md) | touch the admin dashboard's Monitoring pages: their signals and thresholds (`internal/monitor`), the queries behind them, `users.last_seen_at`, or the `monitoring` seeder |
| [api/mail.md](api/mail.md) | send an email, add a mail template, or read the development mailbox |
| [api/pagination.md](api/pagination.md) | list, search or sort a resource |
| [api/database.md](api/database.md) | write a query, add a migration or a table, or prune old rows (`make prune`) |
| [api/seeding.md](api/seeding.md) | need fake data: factories, seeders, `make seed` |
| [api/testing.md](api/testing.md) | write or run Go tests |

## UI (React Router single-page app, `apps/web`)

| Doc | Read it when you… |
| --- | --- |
| [ui/README.md](ui/README.md) | need the folder layout and where new code goes |
| [ui/pages.md](ui/pages.md) | add a page or a route, load a page's data, or prerender a public page |
| [ui/api.md](ui/api.md) | fetch or mutate API data from components or route loaders |
| [ui/pagination.md](ui/pagination.md) | build a paginated, searchable, sortable list |
| [ui/design.md](ui/design.md) | build or style components |
| [ui/forms-and-actions.md](ui/forms-and-actions.md) | add a form or a button: where actions go (bottom right; full width only on sign-in pages), their order and style |
| [ui/testing.md](ui/testing.md) | write or run Vitest and Playwright tests |
| [ui/base-ui.md](ui/base-ui.md) | use or customise a BaseUI component, or hit one of its known issues |
| [ui/strict-mode.md](ui/strict-mode.md) | see something only under `make dev` (double renders, effects running twice) |

## Everything else

- [naming.md](naming.md): how routes, controllers (and the `httpapi/endpoints` layout), store methods, permissions, events, rate limits, client methods, page routes and components are named, and how lists of declarations are grouped and commented. Read it before adding any of them.
- [checks.md](checks.md): the tests and script that enforce [naming.md](naming.md) and the docs' links (store methods, routes, permissions, events, the API client, page routes, page exports, file names, links), how to read a failure, and what to do when a name is new, for people and AI agents.
- [deployment.md](deployment.md): deploying to production: the image and commands, required configuration, migrating and releasing, several servers, proxies and client IPs, Cloudflare, email, and a security checklist.
- [testing.md](testing.md): local environment setup, running the dev server (`make dev`), building and running the production binary (`make build`, `make serve`), manual demo and failure checks.
- [`plans/`](../plans): feature plans and the decisions behind them.

## Adding a resource end to end

1. Migration (`make migration name=create_tasks`) → [api/database.md](api/database.md)
2. Store model (public ID if users open or search its records) and queries scoped by `workspace_id` → [api/database.md](api/database.md)
3. Factory (required for every table) and seeder if useful → [api/seeding.md](api/seeding.md)
4. Record `<resource>.created` / `.updated` events in the store methods, in their transaction → [api/events.md](api/events.md)
5. The controller and its validation in `httpapi/endpoints/<area>`, route on the `ws` group in `httpapi/router/routes.go` (session + membership, `ws.Can` with a permission for mutations) → [api/endpoints.md](api/endpoints.md)
6. Lists use `Paginate[T]` → [api/pagination.md](api/pagination.md)
7. Integration tests including non-members, other workspaces and the role × route matrix → [api/testing.md](api/testing.md)
8. Types in `src/types/`, a client in `src/lib/api/<resource>.ts` → [ui/api.md](ui/api.md)
9. Page routes in `lib/routes.ts` (and `returnRoutes`), pages under `pages/app/[workspace]/`, registered in `src/routes.ts` inside the app layout → [ui/pages.md](ui/pages.md)
10. Update [api/endpoints.md](api/endpoints.md) and any doc whose summary changed.
