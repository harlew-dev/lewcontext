# Findings: A8, the separate admin build

Plan: [separate-build](../../implemented/admin/separate-build.md), implemented 2026-11-01. Built by a sub-agent, checked by the orchestrator, then reviewed by a second agent that saw only the diff and the plan.

## What shipped

- **Two builds from one package.** The app build has no admin routes. The admin build (`WEB_BUILD=admin`) is served only behind the key.
- **The assets pass.**
  - A stateless HMAC cookie, `Path=/admin-assets/`, valid for 8 hours. Its key is derived from `APP_KEY` with its own label.
  - It's checked in constant time, before any file is looked up.
  - Without a valid pass, `/admin-assets/…` gets exactly what `GET /nowhere` gets.
- **Guards that keep admin code out of the app build:**
  - an ESLint `no-restricted-imports` rule;
  - `scripts/check-app-build.sh`, run by `make build`, the e2e server and the Docker image.
- **Admin code moved out of shared modules:** `lib/admin-routes.ts`, `lib/api/admin.ts` (with `adminHttp`, `useAdminApi` and `adminPaths`), and the admin loaders in `lib/admin.tsx`.
- **The server refuses to start** when `ADMIN_ROUTE_KEY` is set and the admin build is missing (`ErrAdminNotBuilt`).

## Where the build differs from the plan

The plan records each of these with the spike's results.

- **Typegen can't serve two route lists at once.** It deletes its directory and registers one global type. So the dev servers and typegen see both route lists; only `react-router build` gets one, and any unexpected command gets one, so it fails safe.
- **The admin dev server needed a small Vite plugin** (`adminPagesUnderBase`). Without it, Vite's dev server answers `/admin/<key>` with a 404 when `base` is `/admin-assets/`.
- **The admin build's files are at `/admin-assets/assets/<file>`.**
- **A new admin catch-all route** (`pages/admin/not-found.tsx`) sends app links ("Back to app", Account) to a full page load, so Go serves them from the app build.
- **Renamed:** the admin API paths are now `adminPaths` (was `paths.admin`), so they don't clash with the page-route helper `adminRoutes`.
- **The bare `/admin-assets` path is registered as an unknown page.** Otherwise Go's router redirected it to `/admin-assets/`, which showed the route exists.
- **A pass's expiry may be up to 1 minute past 8 hours,** for clock differences between servers.
- **Admin files also send `no-referrer` and `noindex`.**

## Decisions made while building (worth your review)

- **The assets pass is always signed with the current key's digest**, whichever key served the page. This was the orchestrator's addition, so that A2's `ADMIN_ROUTE_KEY_PREVIOUS` sessions can still load assets without A2 changing the pass. Rotating the key still ends every pass.
- **AGENTS.md: two lines updated by the orchestrator.** `make dev` now describes the third process (the admin Vite on 127.0.0.1:3002) and `make web-admin`. The `internal/web` ownership line now names the admin build and the assets pass.

## Risks that remain (accepted by the plan or noted)

- **A script injected into an app page** could read admin *code* in a browser that opened the dashboard in the last 8 hours. It gets no key and no API access. The plan accepted this risk, and `docs/api/admin.md` now states it.
- **The pass cookie can't use the `__Host-` prefix**, because its path isn't `/`. A sibling subdomain could plant a cookie with the same name, which would break admin assets for that browser (a denial of service). It can't forge a pass.
- **`routes.ts` picks its route list from the CLI command.** This fails safe, and the output check catches a mix-up.
- **In development, each dev server knows the other's routes.** A cross-build link that's missing a full reload would show up only on real builds (`make serve`, e2e).

## For later plans

- **A2:** `ADMIN_ROUTE_KEY_PREVIOUS` only needs the page gate to accept the old key; the pass already uses the current key. **Open question for A2:** should the IP allowlist also cover `/admin-assets/`? A stolen pass works from any IP for up to 8 hours, though it opens only code.
- **A9:** its plan still says `routes.admin.emailBlocks.index(key)` and `adminRoutes.emailBlocks.index(query)`. After A8 these are `adminRoutes.emailBlocks.index(key)` (page URLs) and `adminPaths.emailBlocks…` (API paths). The A9 builder is told this.
- **A3:** access-log `route` labels need a fixed label for `/admin-assets/`, and the pass cookie must never be logged (A3's redaction covers `cookie`).
- **A4:** new admin pages go in the admin route list in `routes.ts`, with their code in the admin modules. ESLint and the output check enforce this.

## Environment notes

- **Playwright can't find Chromium by default** on this machine (`/usr/bin/chromium` is missing). E2E runs need `CHROMIUM_PATH=/home/homelab/.cache/ms-playwright/chromium-1248/chrome-linux64/chrome`.
- **Hot reload through the `:3000` proxy wasn't checked in a real browser**, because `:3000` was held by another process. On a spare port, the proxy served admin pages and modules and upgraded the websocket.

## Security review

The independent reviewer found no high-severity defects. It probed `/admin-assets/` with an out-of-tree router test and found the refusals byte-identical to `GET /nowhere`: no pass, a bad pass, an unknown file, HTML, dot-files, a directory, the bare path, HEAD, and Range without a pass. It also confirmed sound: the HMAC and its key derivation, the cookie's attributes, that the pass is set only on a 200, the embedding, and the `WEB_BUILD` route split.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Medium | **The two leak guards could both be bypassed at once.** ESLint missed dynamic `import()`, `..` and `./` in alias paths, `.js` extensions and `/src/…`. The output check's four strings don't appear in several compiled admin modules (`admin-routes.ts`, `admin-list.tsx`, the constants in `types/admin.ts`), and it was case-sensitive. The open A9 plan still pointed at `routes.admin…` in the shared `lib/routes.ts`, which would have leaked admin page URLs with neither guard firing | A Vite plugin fails the app build if any admin module is in its module graph; `/admin/` added as a marker; a case-insensitive grep; an ESLint rule for dynamic imports; A9's plan text corrected. _Result below_ |
| 2 | Low | **Only the first cookie with the pass's name was checked.** A sibling subdomain could plant a junk one on a longer path and stick an admin in a reload loop | Every cookie with that name is checked, up to a small cap |
| 3 | Low | **Admin files were cached privately for a year,** longer than the pass or the session | `private, max-age=` the pass lifetime (8 hours), `immutable` |
| 4 | Low (dev only) | **In development, `:3000` binds every interface,** so anyone on the LAN can load admin code (not data) through the proxy. Not a regression: before, the same code was in the app dev bundle | Documented in `docs/testing.md`. The binding isn't changed, in case you browse the dev server from another machine. **Your call** whether to bind `127.0.0.1` |
| 5 | Nit | The pass was set before knowing the shell would be served (unreachable today) | Set only once the shell is served |
| 6 | Nit | A negative expiry passed the format check (always expired, so harmless) | Rejected, with a test |
| 7 | Nit | With a valid pass, `/admin-assets/x.js/` serves the file (as the app's assets do) | Left as is |
| 8 | Nit | One HMAC (~1 µs) more than `/nowhere` when a pass cookie is present | Not measurable over a network; left |

Tests the reviewer asked for, added with the fixes: refusals with `Range:` and `If-None-Match:`, a self-test for `check-app-build.sh`, a pass behind a junk cookie, and no cookie without an admin build.

**Fix results.** Each guard was shown to fail by planting code, which was then reverted:
- The build guard stopped a dynamic `import("@/lib/../lib/api/admin")` and an `./api/admin.js` import.
- The output check stopped a rendered `/Admin/planted` string.
- ESLint flagged all ten bypass forms.

The cookie cap, the no-cookie-without-a-build guard and the case-insensitive grep were each mutation-tested: weakening each one made its test fail. All checks passed:
- `make check`, including the new `scripts/check-app-build.test.sh`;
- `make test`: 20 Go packages, 266 Vitest tests;
- `make build`;
- `make test-e2e`: 22 of 22.

Two flaky runs came before the last one. One was a strict-mode heading match on the Mail page, now fixed with `exact: true`. The other was a one-off Chromium "Target crashed".

**Kept by hand in two places:** the list of admin modules lives in `web-build.ts` (the build guard) and in the ESLint regex. A comment in each says to keep them in sync. If a new admin module is added to only one, the build guard still catches imports by path, but the ESLint message won't.
