# Findings: terms of service and a privacy policy (policies)

Plan: [policies](../../implemented/policies.md) (on the ui branch it has moved to `implemented/policies.md`). You asked for it in notes.md and then for every plan to be built, so it was built on the **ui** worktree (`../lewcontext-ui`, branch `admin-run/ui`) with every recommendation in its 19-decision table taken as accepted. Screenshots are in [policies/](../../screenshots/policies/).

## What shipped

| Commit | What |
| --- | --- |
| `eb44655` | Go: `POLICIES=on\|off` (**off by default**). With it off, `/policy/*` pages and their data files answer as unknown URLs. `GET /api/policies` tells the sign-up page whether to show the checkbox |
| `95be21e` | Data: three nullable columns on `users` (each document's version, and when they were accepted), held on `pending_registrations` until the code is verified; `auth.registered` records them |
| `a72f092` | The pages: `apps/web/content/policy/terms-of-service.md` and `privacy-policy.md` (template text for you to edit), turned into prerendered pages at build time by `marked`, a new **devDependency** that never reaches the browser |
| `b44b686` | Sign-up: a required checkbox ("I agree to the Terms of Service and have read the Privacy Policy."), a new `Checkbox` component, footers on home, login and register, and a read-only "Terms and privacy" section in Account settings |
| `b05a1e0` | e2e: a second server with `POLICIES=on` |
| `354e9c7` | Docs and plan |

**How it looks:**
- [sign-up with the checkbox](../../screenshots/policies/9-sign-up-checkbox.png), and [ticked](../../screenshots/policies/9-sign-up-checkbox-ticked.png);
- [a policy page](../../screenshots/policies/8-policy-page.png), and the [full page](../../screenshots/policies/8-policy-page-full.png) showing the template warning.

## What you need to do before turning it on

1. **Edit the two Markdown files** in `apps/web/content/policy/`. They're placeholder text marked `boilerplate: true`.
2. **Remove the `boilerplate: true` line.** In production the server **refuses to start** with `POLICIES=on` while either file still has it.
3. **Set the `updated:` date** in each file's header. That date is the document's version, and the "Last updated" line on the page.
4. **Set `POLICIES=on`.**

## Both checks done before building

- **React Router does write data files for prerendered pages with loaders,** so Go can hide them with the pages.
- **`marked` doesn't reach the browser.** A search of the built files for its code and the reader's text finds nothing, on the final build too.

## Where the build differs from the plan (recorded in the plan)

- **The production refusal lives in `internal/web` (`ServePolicies`),** where it's tested.
- **Each policy page has its own error boundary showing "not found".** The root one showed "A brief pause." when the pages were switched off.
- **The template warning shows whenever a file is marked `boilerplate`,** not only in development, because production builds the pages.

## How the security points are met

- **Sign-up still answers the same** whether or not the email has an account, with `POLICIES` on. A missing, stale or invented agreement gets 422 before any lookup or limit, and saves nothing.
- **No raw HTML ever reaches a page.** Markdown becomes typed blocks rendered as text. Raw HTML, images, code blocks and `javascript:`, `data:`, `http:` or `//host` links **fail the build**, naming the file and line.
- **CSP:** the page's inline data script is hashed into its CSP, never `unsafe-inline`.
- **`npm audit`** reports nothing for `marked`. Its 13 moderate advisories all come from `minimatch` in the ESLint toolchain, which was already there.

## Things to know

- **Existing accounts aren't asked to agree,** and nobody is asked again when a document changes. That's for a later plan (`POLICIES_EXISTING=required`, after the email-verification pattern).
- **Merging:** the migration edits to `002_create_users` and `016_create_pending_registrations` mean the local dev database needs `make migrate-fresh CONFIRM=1 && make seed` after merging.

## Checks

The full suite passed:
- `make check`;
- `make test`: 20 Go packages, Vitest 341 tests;
- `make build`;
- e2e: 25 of 25, including the new `policies.spec.ts`.

## Security review

The independent reviewer found **no high or medium defects**. It checked these and found them sound:
- **Same answer at sign-up:** the agreement check reads only the body and the server's versions, before any account lookup.
- **Versions:** accepted only when they match the server's exactly.
- **The Markdown reader:** about 45 crafted inputs (entity-encoded `javascript:`, `www.` autolinks, zero-width characters in links, HTML in tables, …) all fail the build with the file and line; nothing is silently dropped.
- **Switched off:** the pages, data files and `.policies.json` answer exactly like an unknown URL, checked against the real build.
- **CSP:** the hashes cover every inline script on the real pages.
- **Exposure:** agreement dates go only to the account's own responses, never to other workspace members.
- **`slot()`'s new checked state** affects no other component.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Low | A sign-up submitted while `POLICIES` was off, then verified after it was turned on, created an account with no agreement | Verify ends such a sign-up ("sign up again"), the same way for every address |
| 2 | Low | Under `make dev`, Vite serves the policy pages even with `POLICIES=off` | Documented: only `make serve` and production honour the switch |
| 3 | Low | If loading the policies failed, a refused sign-up showed no error | The error appears in the form's notice |
| 4–8 | Nits | A bad server version dropped silently (unreachable); route chunks in the app build when off (the text isn't); `noreferrer` on links; the admin session's user shows `policies: null`; doc drift | Fixed or documented |

_Fix results: filled in once the fixes are committed._
