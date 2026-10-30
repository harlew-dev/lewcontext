# Forma docs

Short summaries of how the system works. Start with the project rules in [`AGENTS.md`](../AGENTS.md), then read the doc for the area you're changing.

## API (Go, `apps/api`)

| Doc | Read it when you… |
| --- | --- |
| [api/README.md](api/README.md) | need the package layout, request flow or environment variables |
| [api/endpoints.md](api/endpoints.md) | call or add an endpoint |
| [api/auth.md](api/auth.md) | touch sign-in, sessions, CSRF, rate limits or security headers |
| [api/pagination.md](api/pagination.md) | list, search or sort a resource |
| [api/database.md](api/database.md) | write a query, add a migration or a table |
| [api/seeding.md](api/seeding.md) | need fake data: factories, seeders, `make seed` |
| [api/testing.md](api/testing.md) | write or run Go tests |

## UI (Next.js, `apps/web`)

| Doc | Read it when you… |
| --- | --- |
| [ui/README.md](ui/README.md) | need the folder layout and where new code goes |
| [ui/pages.md](ui/pages.md) | add a page, a route or server-rendered data |
| [ui/api.md](ui/api.md) | fetch or mutate API data from the browser or the server |
| [ui/pagination.md](ui/pagination.md) | build a paginated, searchable, sortable list |
| [ui/design.md](ui/design.md) | build or style components |
| [ui/testing.md](ui/testing.md) | write or run Vitest and Playwright tests |

## Everything else

- [testing.md](testing.md): local environment setup, manual demo and failure checks.
- [`plans/`](../plans): feature plans and the decisions behind them.

## Adding a resource end to end

1. Migration (`make migration name=create_tasks`) → [api/database.md](api/database.md)
2. Store model and queries scoped by `user_id` → [api/database.md](api/database.md)
3. Factory (required for every table) and seeder if useful → [api/seeding.md](api/seeding.md)
4. Handlers and validation in `httpapi`, route in `routes.go` behind `requireSession` → [api/endpoints.md](api/endpoints.md)
5. Lists use `Paginate[T]` → [api/pagination.md](api/pagination.md)
6. Integration tests including cross-user access → [api/testing.md](api/testing.md)
7. Types in `src/types/`, a client in `src/lib/api/<resource>.ts` → [ui/api.md](ui/api.md)
8. Page routes in `lib/routes.ts`, pages under `pages/app/` → [ui/pages.md](ui/pages.md)
9. Update [api/endpoints.md](api/endpoints.md) and any doc whose summary changed.
