# Terms of Service and Privacy Policy

Status: **implemented 2026-11-01**, every recommendation in the Decisions table confirmed and built as written; see [How the build differs](#how-the-build-differs) for the differences and the docs updated. Builds on [the SPA](spa.md) (prerendered pages, CSP hashes computed by Go at startup), [email verification](email-verification.md) (pending registrations; sign-up answers the same for every address) and [the admin build](admin/separate-build.md) (a page that is always built but that Go serves only when a setting is on).

## Why

An operator who opens sign-up to the public needs two documents and proof that each account agreed to them:

1. **Two public pages**, `/policy/terms-of-service` and `/policy/privacy-policy`, written by the operator in Markdown files that ship as boilerplate, prerendered so search engines and people without JavaScript can read them.
2. **A required checkbox on sign-up** linking to both, enforced by the API, so nobody can create an account through the API without agreeing.
3. **A record** of which version each account agreed to, and when, so a later change to the terms can tell who agreed to what.
4. **One setting** that turns all of it on or off. Off is the default: the boilerplate isn't fit to publish until someone has edited it.

Today there are no such pages, no checkbox component, and nothing on `users` about agreement. Sign-up (`account.Registrations.Register`) decodes `httpapi.Credentials` (name, email, password), which login and the admin login share.

## Flow

```
build (make build, docker build)
  apps/web/content/policy/terms-of-service.md, privacy-policy.md
    → each page's loader (build time): front matter + Markdown → a typed block tree
    → prerendered /policy/terms-of-service, /policy/privacy-policy (HTML + .data)
    → build/client/.policies.json   {termsOfService: {version, boilerplate}, privacyPolicy: {…}}

server start (POLICIES=on)
  web.New reads .policies.json → the versions; missing → refuse to start
  production and either document still marked boilerplate → refuse to start

GET  /policy/terms-of-service       on: the prerendered page   off: the ordinary 404 page
GET  /api/policies                  {enabled: true, termsOfService: "2026-11-01", privacyPolicy: "2026-11-01"} | {enabled: false}
/auth/register                      its loader asks /api/policies; on: a required checkbox with both links
POST /api/auth/register {name, email, password, policies: {termsOfService, privacyPolicy}}
  on: both versions present and current, or 422 field "policies"   (before any limit or lookup)
  → pending_registrations keeps the versions → verify copies them to users (policies_accepted_at)
  → auth.registered {…, policies}
```

## Configuration: `POLICIES`

| Variable | Values | Default | Does |
| --- | --- | --- | --- |
| `POLICIES` | `on`, `off` | `off` | `on`: Go serves the two policy pages, `GET /api/policies` says so, sign-up shows the checkbox and the API refuses a sign-up without agreement. `off`: the pages are the ordinary 404, no checkbox, nothing recorded |

### A runtime switch for build-time pages (decision 2)

The documents are content, so they are always in the build; the question is only what decides whether they're live.

| | **Runtime (recommended):** Go reads `POLICIES`, the pages are always built | Build time: `POLICIES` baked in like `APP_NAME` | Go writes the switch into the served HTML |
| --- | --- | --- | --- |
| Pages when off | Go answers `/policy/*` (and its `.data` files) as any unknown URL: the fallback with 404 | Not in the build at all | Not in the build, or hidden |
| How sign-up learns it | `GET /api/policies` in the register page's loader | `import.meta.env` | A `<meta>` Go adds at startup |
| How the API learns it | Its own config | The build's manifest | Its own config |
| Home footer links | After a request, not in the prerendered HTML | In the prerendered HTML | In the HTML |
| Turning it on or off | Restart with the variable | Rebuild the image | Restart |
| e2e covers both modes | One build, a second server with `POLICIES=on` (the existing `E2E_SETTINGS_PORT` pattern) | Two builds | One build |
| Mismatch possible | No: Go decides everything | Runtime env set without the build arg silently does nothing | No |
| Fits the codebase | Like `EMAIL_VERIFICATION` and `ADMIN_ROUTE_KEY` (the admin build is always built, served only with the key) | Like `APP_NAME` | Rewrites React-rendered HTML, which breaks hydration and the CSP hashes |

**Recommended: runtime.** Every other feature switch is a Go setting read at startup, one image can run either way, and the API, which must enforce the checkbox, is the single source of truth. The cost is one small public endpoint and footer links that appear after the page loads; the policy pages themselves are still prerendered and indexable.

### Startup rules (`internal/config/policies.go`, `cmd/server`)

- `POLICIES` must be `on` or `off`, else refused, like `EMAIL_VERIFICATION`.
- **On needs the build's manifest.** `web.New` reads `.policies.json`; with `POLICIES=on` and no web build, or a build without it, the server refuses to start ("POLICIES=on needs a web build with the policies: run make build"), as `ADMIN_ROUTE_KEY` does without the admin build (`web.ErrAdminNotBuilt`).
- **No template text in production.** With `APP_ENV=production`, `POLICIES=on` and either document still marked `boilerplate: true`, the server refuses to start, naming the file, the way production refuses `.env.example`'s `APP_KEY`. In development it starts and the page shows a warning `Notice` ("Template text: edit `apps/web/content/policy/terms-of-service.md` before publishing.").
- `cmd/server` hands the versions to the server (`httpapi.Server.Policies`, empty when off). Tests set them by hand.

## Content

### The files (decision 3)

```
apps/web/content/policy/terms-of-service.md
apps/web/content/policy/privacy-policy.md
```

Committed boilerplate for a small SaaS, with bracketed placeholders (`[Company name]`, `[contact email]`, `[jurisdiction]`) and a front matter block:

```markdown
---
title: Terms of Service
description: The terms for using lewcontext.
updated: 2026-11-01
boilerplate: true
---

## 1. Who we are
…
```

- The operator edits the text, sets `updated`, deletes the `boilerplate` line and rebuilds. deployment.md gets a section on it, and says plainly that the boilerplate is a starting point, not legal advice.
- They live in `apps/web` because only the web build reads them; the Docker build's web stage already copies `apps/web`, so no Dockerfile change.
- Front matter is parsed by our reader (a fenced block of `key: value` lines). `title` and `updated` are required; `updated` must be a real `YYYY-MM-DD` date; an unknown key fails the build.

### Markdown to page (decision 4)

| | **Build-time loader + `marked`'s lexer (recommended)** | Hand-written parser for a subset | A Markdown library in the browser | `marked` to HTML, rendered with `dangerouslySetInnerHTML` |
| --- | --- | --- | --- | --- |
| Where it runs | The pages' `loader`, at prerender | Same | Every visit | Build |
| Client bundle | No parser: the loader's output is data | No parser | A parser (tens of KB) | No parser |
| Raw HTML | `html` tokens fail the build; we render React elements, so nothing is ever inserted as HTML | Same, if we write it right | Needs a sanitizer | Inserted as HTML: what this plan must not do |
| Styling | Our `Heading`, `Text`, `TextLink`, `Table` components | Same | Same, through a renderer | Element selectors, a second styling system |
| Correctness | A maintained CommonMark/GFM tokenizer | Ours to get right (nested lists, emphasis, escapes) | Maintained | Maintained |
| New dependency | `marked`, a devDependency only | None | A runtime dependency | `marked` |

**Recommended: the pages' loaders run at build time** (pages.md: "A prerendered route may have a loader, which runs at build time"). The loader calls `readPolicy("terms-of-service")` in `src/lib/policies.server.ts`, which reads the file, parses the front matter, tokenizes the Markdown with `marked.lexer` and maps the tokens to our own typed blocks (`types/policies.ts`). `PolicyDocument` renders the blocks with the existing components; React escapes all text. The `.server.ts` suffix makes React Router fail the build if client code imports it, so `marked` and `node:fs` can't reach the browser.

**`marked`** (MIT, no dependencies, widely used) goes in `devDependencies`, pinned to an exact version, and is listed in docs/ui/README.md's Dependencies with this reason. `npm audit` afterwards. Only its lexer is used; its HTML renderer never runs.

### What the Markdown may hold (decision 5)

| Allowed | Becomes |
| --- | --- |
| `##`–`####` headings | `Subheading` levels 2–4, each with an `id` from its text so sections can be linked (`#cookies`). `#` is refused: the title comes from front matter |
| Paragraphs, `**bold**`, `*italic*`, `` `code` `` | `Text`, `Strong`, `em`, `Code` |
| Ordered and unordered lists, nested | `ol` / `ul` with list classes |
| Links | `TextLink`, only to `https:`, `mailto:` or a path on this site (`/…`, not `//…`) |
| Tables (GFM) | `Table` (privacy policies list data, purpose and retention) |
| `---` | `Divider` |

Anything else (raw HTML, images, code blocks, block quotes, footnotes, other link schemes) fails the build with the file and line, so the operator finds out before deploying, never in production.

### Version and "Last updated" (decision 6)

**The version is the `updated` date.** The page shows "Last updated 1 November 2026" (`formatDate`), and the same date is what an account agrees to and what the API checks. One value that people already see, not a second number to forget to bump. Changing a document means changing its date.

### The manifest Go reads

A small Vite plugin in the app build (`policyManifest()` in `vite.config.ts`, beside `noAdminModules`) writes `build/client/.policies.json` from the same reader:

```json
{"termsOfService": {"version": "2026-11-01", "boilerplate": true}, "privacyPolicy": {"version": "2026-11-01", "boilerplate": true}}
```

It's a dot-file, so `internal/web` never serves it (`hidden`), and `//go:embed all:dist` and `install_build` carry it into the binary. `web.New` parses it; `Pages.Policies()` returns it.

## Pages (decision 8)

| Path | Page file | Export | Route constant |
| --- | --- | --- | --- |
| `/policy/terms-of-service` | `pages/policy/terms-of-service.tsx` | `TermsOfService` | `routes.termsOfService` |
| `/policy/privacy-policy` | `pages/policy/privacy-policy.tsx` | `PrivacyPolicy` | `routes.privacyPolicy` |

- Registered in `appRoutes` in `src/routes.ts` and added to `prerender` in `react-router.config.ts` (`["/", "/policy/terms-of-service", "/policy/privacy-policy"]`). Not resources, so plain names (naming.md's "Pages that aren't a resource"). Each page file is a `loader` (`readPolicy`), a `meta` export (title and description from the front matter) and `<PolicyPage policy={…} />`.
- **Shared UI** goes in `components/app/policies/` since two pages (and sign-up, home, login and settings, for the links) use it: `policy-document.tsx` (`PolicyPage`, `PolicyDocument`) and `policy-links.tsx` (`PolicyLinks`).
- **Layout (decision 9):** `AuthLayout` with a wider column (`max-w-2xl` article): `Logo href={routes.home}`, `Heading` (the title), "Last updated …", the boilerplate notice in development, the document, then `PolicyLinks`. No new layout: `AuthLayout` already lets its child set the width, and a long page just scrolls.
- No session and no `guestLoader`: they read the same signed in or out, and hold no user data (rule 7).
- **Go serving:** with `POLICIES=on`, the prerendered HTML is served as `/` is (`no-cache`, its own CSP), and `/policy/<page>.data` (React Router's data file for in-app navigation) as a built file. With `off`, `web.New` leaves both out of what it serves, so they get the 404 fallback; an in-app link that slips through gets the root `ErrorBoundary`'s `NotFound`.
- **Links (decision 16):** `AuthLayout` gains an optional `footer` (below the card), and `StatusPage` passes one through. `PolicyLinks` ("Terms of Service · Privacy Policy") shows on home, login and register while `GET /api/policies` says enabled; the two policy pages link each other. Account settings gets a read-only "Terms and privacy" section (below).

## Sign-up agreement

### The form

```
/auth/register     Email · Full name · Password
  [ ] I agree to the Terms of Service and have read the Privacy Policy.     (both links open a new tab)
  [Create account]
```

- **Shown only when `GET /api/policies` says enabled.** `registerLoader` in `lib/loaders.ts` runs `guestLoader` and asks for the policies in parallel. If that request fails (API down), the form renders without the checkbox, and the API's 422 (below) brings it back once the API answers.
- **Required** like the other fields (`required`, so the browser stops an unticked submit), with the API's 422 shown under it (`ErrorMessage`).
- **The wording (decision 10):** a privacy policy informs rather than asks for consent, so the label says the person *agrees* to the terms and *has read* the privacy policy.
- **Links open a new tab**, so the typed fields survive reading the documents.
- **A new `components/ui/checkbox.tsx`** (`Checkbox`, `CheckboxField`) on BaseUI's `Checkbox`, styled with `slot()` like the other controls (decision 19). There is no checkbox component yet; later forms reuse it.
- **Invitation sign-ups** use the same form, so they agree the same way. Accepting an invitation with an existing account doesn't involve the policies.

### The API

```json
POST /api/auth/register
{"name": "Ada", "email": "ada@example.com", "password": "…",
 "policies": {"termsOfService": "2026-11-01", "privacyPolicy": "2026-11-01"}}
```

- **The client sends the versions it showed** (from `GET /api/policies`), not a boolean (decision 11). The API accepts only the current ones, so the recorded version is exactly what the page offered.
- **Its own input type.** `Credentials` is shared with login and the admin login, which must keep refusing unknown fields, so `registrations.go` decodes a `registration` struct embedding `httpapi.Credentials` plus `Policies`.
- **With `POLICIES=on`:**
  - missing, or either version empty → 422 `{field: "policies", error: "Agree to the Terms of Service to create an account."}`;
  - not the current versions (the page was opened before a deploy changed a document) → 422 `{field: "policies", error: "Our Terms of Service or Privacy Policy changed since this page opened. Read them again, then agree."}`. The page reloads the versions and unticks the box.
- **With `off`:** the field is accepted and ignored (a page opened just before the switch still signs up), and nothing is recorded.
- **Where the check runs:** with the existing body validation, before `AccountLimit`, the registration limits and hashing, so a refused body counts against nothing, as a malformed one doesn't today.

### Sign-up still answers the same for every address

The check reads only the body and the server's versions, never `users`, so it answers identically for a new, taken or blocked address. With verification on, `register` inserts the same `pending_registrations` row shape (now with the versions) for every address. `TestSignUpLooksTheSameForATakenEmail` sends the agreement and keeps comparing status, body, headers, cookie and the tables each request touches.

## Recording acceptance (decisions 12, 13)

- **With verification on**, `pending_registrations` keeps the two versions until the code is right; `CompleteRegistration` copies them to the new `users` row with `policies_accepted_at` set to the pending row's `created_at`: the moment the person ticked the box and submitted, which `register/resend` doesn't change. A document updated while the sign-up waits for its code (15 minutes, renewed by each resend) doesn't matter: the record is the version they agreed to. A sign-up that began while `POLICIES` was off agreed to nothing: once it's on, verify ends it as an expired one ("This sign-up has expired. Start again.", the same for every address), so no account is made without a record (review fix).
- **With verification off**, `CreateUser` takes the versions and records them with `now()`.
- **`auth.registered`** gains `policies: {termsOfService, privacyPolicy}` (absent when off), so the audit trail shows it for 90 days. The `users` columns are the lasting record.
- **The user JSON** gains `policies: {termsOfService, privacyPolicy, acceptedAt}` or `null`: the account's own data, in every response that carries the user.
- **Not a history table yet.** One agreement per account (at sign-up) fits three columns. Re-accepting later versions (below) is when a `policy_acceptances` table would earn its place, and that plan decides.

## Existing accounts and changed policies (decision 14)

**In scope now:** new sign-ups agree to the current versions, and every agreement is recorded with its versions.

**Later, in its own plan:** accounts made while `POLICIES` was off (or before this ships) have `policies: null` and aren't asked anything; a changed `updated` date doesn't ask anyone to agree again. The recorded versions are what a later gate needs, after the `EMAIL_VERIFICATION_EXISTING=required` pattern: a `POLICIES_EXISTING=required` setting, an "Our terms have changed" page after sign-in, a short allowlist of routes, and an `auth.policies_accepted` event. Emailing everyone about a change would also need its own decision under mail.md's rules.

## Account settings

A read-only `SettingsSection` "Terms and privacy" on `/app/settings`, only while enabled (`useResource(paths.policies.show())`), with no buttons:

- agreed: "You agreed to the Terms of Service (1 November 2026) and read the Privacy Policy (1 November 2026) on 3 November 2026.", each name a link;
- a document updated since: adds "The Terms of Service were updated on 1 December 2026.";
- no record: "Read our Terms of Service and Privacy Policy."

## Admin (decision 17)

Nothing now. The Events page already shows `auth.registered` with its `policies` for 90 days. Showing the agreed versions on an admin user's page is a small follow-up for the admin plans (it would live in the admin build, rule 7).

## API

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| GET | `/api/policies` (new) | Public, not limited (answered from memory, like `/api/health`) | 200 `{enabled: true, termsOfService: "2026-11-01", privacyPolicy: "2026-11-01"}`, or `{enabled: false}`. Always registered, so the client can tell "off" from "API down" |
| POST | `/api/auth/register` | unchanged | Body gains `policies`; with `POLICIES=on`, 422 `field: "policies"` when it's missing or stale (above). Responses unchanged otherwise |
| every response with the user | | | gains `policies` (or `null`) |

- **Controller:** `account.Policies.Show` (`endpoints/account/policies.go`), a singleton read. `/api/policies` would be its own `policy` area by naming.md's rule; it joins `account` with the other signed-out routes, as `/api/email-blocks` did, and naming.md notes the exception (decision 15). `TestHTTPLayout` lists it if needed.
- **Client:** `lib/api/policies.ts`: `paths.policies.show()`, `api.policies().show()`. `Registration` in `lib/api/auth.ts` gains `policies?`.

## Data

Migrations are squashed in development: edit the create migrations, then `make migrate-fresh CONFIRM=1 && make seed`.

```sql
-- 002_create_users: add
 terms_of_service_version date,
 privacy_policy_version date,
 policies_accepted_at timestamptz,
 CHECK ((terms_of_service_version IS NULL) = (policies_accepted_at IS NULL)
    AND (privacy_policy_version IS NULL) = (policies_accepted_at IS NULL))

-- 016_create_pending_registrations: add
 terms_of_service_version date,
 privacy_policy_version date,
 CHECK ((terms_of_service_version IS NULL) = (privacy_policy_version IS NULL))
```

- Nullable: accounts made while off, before this ships, and every seeded account have none.
- Store: `NewRegistration` and `CreateUser` take the versions; `CompleteRegistration` copies them. `User` gains `Policies *PolicyAgreement`.
- Factories: `factory.Users` and `factory.PendingRegistrations` leave them null by default, with an `AgreedToPolicies(terms, privacy)` state. The demo seed stays as it is.
- Deleting an account deletes its record with it (no copy is kept; see Not included).

## Events

| Event | Change |
| --- | --- |
| `auth.registered` | Payload gains `policies` (`termsOfService`, `privacyPolicy`), absent with `POLICIES=off`. Recorded by `CompleteRegistration` and `CreateUser` as today |

No new event. Versions aren't sensitive, so `TestPayloadsHoldNothingSensitive` is unaffected.

## Security risks and how they're avoided

| Risk | Avoided by |
| --- | --- |
| Script or markup injected through a Markdown file | Raw HTML tokens fail the build; pages render React elements from typed blocks, never `dangerouslySetInnerHTML`; link schemes are an allowlist (`https:`, `mailto:`, `/`), so `javascript:` fails the build |
| Weakening the CSP | No inline script of ours. React Router's loader data is an inline script in the prerendered HTML, which `internal/web` hashes at startup like every other; never `'unsafe-inline'`. Playwright checks for CSP violations on both pages |
| The parser shipping to browsers | `policies.server.ts` (React Router refuses `.server` imports from client code); `marked` is a devDependency, pinned; `npm audit` |
| Signing up through the API without agreeing | The API enforces it, not the checkbox |
| Recording a version nobody was shown | Only the current versions are accepted; a stale or invented one is 422 |
| Sign-up revealing whether an email has an account | The check reads only the body and config, runs before every limit and lookup, and verification-on writes the same row for every address; the same-answer test covers it |
| Publishing template text | Production refuses `POLICIES=on` while a file is marked `boilerplate` |
| A switch that says on while the pages are missing | `POLICIES=on` without the build's manifest refuses to start; `off` hides the HTML and `.data` files |
| The manifest leaking anything | It's a dot-file (never served) and holds only dates and two flags |
| Personal data | Two dates and a timestamp per account, deleted with it; the event is pruned after 90 days |

## Spike first

Confirm on React Router 7.18 with `ssr: false` that (1) a prerendered route's `loader` runs at build time and writes `policy/terms-of-service.data` for in-app navigation, and the name Go must hide when off; and (2) the client assets contain neither `marked` nor `node:fs`. If (1) fails, fall back to a Vite virtual module holding the parsed blocks, which keeps everything else in this plan.

**Results (2026-11-01, React Router 7.18.5, Vite 8.3.5, `marked` 18.1.1).** A throwaway route `/policy/spike`, prerendered, whose `loader` called a `.server.ts` module that read a file with `node:fs` and tokenized Markdown with `marked.lexer`:

1. **Yes.** `react-router build` printed `Prerender (data): /policy/spike -> build/client/policy/spike.data` and `Prerender (html): … build/client/policy/spike/index.html`. The loader ran at build time (it read `package.json`; its token types were in both files), and the HTML carries the loader data as an inline `streamController.enqueue(…)` script, which Go hashes like any other. So Go hides `policy/<name>/index.html` (served at `/policy/<name>`) and the sibling `policy/<name>.data`: everything under `policy/`. The fallback wasn't needed.
2. **Yes.** The route's client chunk was only the component (`spike-*.js`, a `p` of the loader's data); a grep of `build/client/assets/*.js` for `marked`, `Lexer`, `readFileSync`, `node:fs`, `spike.server`, and two strings from `marked`'s own code (`pedantic`, `blockquote`, which `node_modules/marked/lib/marked.esm.js` holds) found nothing. Rerun on the real build (both policy pages): the same grep, plus `policies.server` and the reader's "isn't allowed", finds nothing in `build/client/assets`.

## Tests

- **Go unit (`internal/web`, `fstest.MapFS`):** with policies on, both pages are prerendered with their own CSP hashes and the `.data` files are served; with off, both answer as `/nowhere` (404 fallback, same headers); `.policies.json` is never served; a malformed manifest fails `New`; `Policies()` returns its versions.
- **Go unit (`internal/config`, `cmd/server` startup):** `POLICIES` defaults to off; bad values refused; on without a manifest refused; production with a boilerplate document refused, development allowed.
- **Go integration:**
  - `GET /api/policies` on and off.
  - Register with on: missing `policies` → 422 on the field, nothing written, no limit counted; stale versions → 422; current versions → 202, and the pending row holds them; verify → the user has both versions and `policies_accepted_at` equal to the row's `created_at`, and `auth.registered` carries them.
  - Verification off with on: `CreateUser` records them; a taken email is still the 409.
  - Off: `policies` accepted and ignored; nothing recorded.
  - `TestSignUpLooksTheSameForATakenEmail` with the agreement: identical answers and tables touched.
  - Login and the admin login still refuse a `policies` field (unknown field, 400).
  - The users `CHECK` refuses a version without a timestamp.
- **Vitest:**
  - `policies.server.ts`: front matter (required keys, bad dates, unknown keys), each allowed block, nested lists, tables, heading ids; raw HTML, images, `#` headings and `javascript:` links fail with the file and line.
  - `PolicyDocument`: renders text as text (a `<script>` in a paragraph is shown, not run), links with the allowed hrefs, the "Last updated" date, the boilerplate notice.
  - Register: no checkbox when off or the request fails; with on, a required checkbox whose links open a new tab, the versions sent, a 422 on `policies` under it, a stale 422 reloading the versions and unticking.
  - Home and login footers appear only when enabled; settings' section in its three states.
- **Playwright (`policies.spec.ts`):** the main server keeps the default (off): `/policy/terms-of-service` is the not-found page and sign-up has no checkbox. A second server with `POLICIES=on` (the `E2E_SETTINGS_PORT` pattern from `email-verification-settings.spec.ts`, same database and `APP_KEY`): both pages' raw HTML has the title, heading and "Last updated" with no CSP violations; navigating between them in-app works; sign-up can't be submitted unticked; ticked, it goes through the code to onboarding, and Account settings shows the agreement.

## Docs

- `docs/api/README.md`: `POLICIES`; the serving table's `/policy/*` row; `internal/web` reading the manifest.
- `docs/api/endpoints.md`: `GET /api/policies`; register's `policies` and 422; the user's `policies`.
- `docs/api/auth.md`: a "Terms and privacy policy" section (the checkbox, enforcement, recording, the same-answer rule).
- `docs/api/database.md`: the new columns. `docs/api/events.md`: `auth.registered`'s payload. `docs/api/rate-limits.md`: `/api/policies` under "Not limited". `docs/api/seeding.md`: the factory state. `docs/api/testing.md`: the notable tests.
- `docs/naming.md`: `account.Policies` outside its own area; `routes.termsOfService`.
- `docs/ui/pages.md`: the two pages, `prerender`, `registerLoader`, the footer. `docs/ui/README.md`: `content/policy`, `pages/policy`, `components/app/policies`, and `marked` under Dependencies. `docs/ui/design.md` and `docs/ui/base-ui.md`: `checkbox.tsx` on BaseUI's `Checkbox`; `AuthLayout footer`. `docs/ui/forms-and-actions.md`: the sign-in pages' row mentions a required agreement above the button. `docs/ui/api.md`: `api.policies()`. `docs/ui/testing.md`: the new spec.
- `docs/deployment.md`: the variable, a "Terms of Service and Privacy Policy" section (edit, remove `boilerplate`, rebuild, turn on; not legal advice), and a checklist line.
- `docs/testing.md`: trying it locally. `.env.example`: `POLICIES=off` with a comment.
- `AGENTS.md`: rule 4's page areas gain `src/pages/policy/*` (public, prerendered).

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Names | The setting is `POLICIES` (`on` or `off`), default `off`; the plan, endpoint and folders are "policies", after the `/policy/*` URLs. Off by default, because the boilerplate isn't fit to publish |
| 2 | Runtime or build-time switch | **Runtime** (Go's `POLICIES`), with both pages always built and hidden by Go when off; the browser asks `GET /api/policies`. Rejected: a build argument (rebuild to switch, two builds to test, a runtime value that silently does nothing) and Go editing the HTML (breaks hydration and the hashes) |
| 3 | Where the files live | `apps/web/content/policy/terms-of-service.md` and `privacy-policy.md`, committed boilerplate marked `boilerplate: true`, edited by the operator |
| 4 | Markdown to HTML | At build time, in the pages' loaders, with `marked`'s lexer (devDependency) mapped to typed blocks rendered by our components; no parser in the browser, no `dangerouslySetInnerHTML`. Rejected: a hand-written parser, a client library, `marked`'s HTML output |
| 5 | Markdown allowed | Headings 2–4, paragraphs, bold, italic, inline code, nested lists, links (`https:`, `mailto:`, `/`), tables, rules. Anything else fails the build |
| 6 | Version | The front matter's `updated` date (`YYYY-MM-DD`), also shown as "Last updated" |
| 7 | Unedited boilerplate | Production refuses `POLICIES=on` while either file is marked `boilerplate`; development shows a warning on the page |
| 8 | Page names | `pages/policy/terms-of-service.tsx` (`TermsOfService`) and `privacy-policy.tsx` (`PrivacyPolicy`); `routes.termsOfService`, `routes.privacyPolicy`; shared UI in `components/app/policies/` |
| 9 | Layout | `AuthLayout` with a `max-w-2xl` article; no new layout |
| 10 | Checkbox wording | "I agree to the Terms of Service and have read the Privacy Policy.", both links opening a new tab |
| 11 | What the client sends | The versions it showed; the API accepts only the current ones (422 `policies` otherwise), so the record is what the page offered |
| 12 | Where acceptance is recorded | Three nullable columns on `users` (`terms_of_service_version`, `privacy_policy_version`, `policies_accepted_at`), carried on `pending_registrations` until verify, plus `policies` on `auth.registered`. No history table until re-acceptance exists |
| 13 | When it was accepted | When the sign-up was submitted (the pending row's `created_at`), not when the code was entered |
| 14 | Existing accounts, changed versions | Not asked now; a later plan adds a `POLICIES_EXISTING=required` gate after the email-verification pattern, using the recorded versions |
| 15 | The endpoint | `GET /api/policies`, `account.Policies.Show`, public, not limited, always registered (`{enabled: false}` when off); naming.md notes it lives in `account` |
| 16 | Where the links go | Footers on home, login and register (while enabled), each policy page linking the other, and a read-only "Terms and privacy" section in Account settings |
| 17 | Admin dashboard | Nothing now (the Events page shows `auth.registered`'s versions); the user page later, in the admin plans |
| 18 | e2e | The main server keeps the default (off); `policies.spec.ts` runs a second server with `POLICIES=on` |
| 19 | Checkbox component | A new `components/ui/checkbox.tsx` (`Checkbox`, `CheckboxField`) on BaseUI's `Checkbox`, styled with `slot()` |

## How the build differs

Built 2026-11-01 on `admin-run/ui`. Where the build differs from the plan above:

- **Where the startup checks live.** `internal/config/policies.go` only parses `POLICIES`. The manifest is read by `web.New` (a malformed one fails it), and `Pages.ServePolicies()` publishes the pages and returns the versions, refusing a build without the policies (`web.ErrPoliciesNotBuilt`) and, in production, a document still marked `boilerplate` (naming its file). `cmd/server` calls it with `POLICIES=on` and sets `httpapi.Server.Policies`, a `store.PolicyVersions`. The production refusal is tested in `internal/web` (`TestProductionRefusesTemplatePolicies`), not through `cmd/server`.
- **Off, the browser shows NotFound through the page's own `ErrorBoundary`.** The plan expected the root `ErrorBoundary`'s `NotFound`, but a `.data` file answered with the 404 page isn't a route 404 to React Router, so the root showed "A brief pause.". Each policy page exports an `ErrorBoundary` rendering `NotFound` (its `meta` copes with no loader data).
- **The template warning shows whenever the document is marked `boilerplate`,** not "in development": the pages are prerendered by a production build, and production never publishes a template anyway.
- **The reader** reads `content/policy` from the working directory (`apps/web`, where every build and test runs; the manifest plugin passes the directory explicitly). `description` is optional in the front matter. Character references (`&amp;`) are decoded to text. A link must also have no whitespace or backslash; `#anchor` alone isn't allowed (as planned: `/…#anchor` is).
- **The manifest plugin** (`policyManifest`) runs only in the app build's client environment (`applyToEnvironment`) and emits `.policies.json` as an asset.
- **The user JSON** gets `policies` from `store.User.MarshalJSON`; the queries that load the account's own user (the session, MFA sign-in, email verification) select the new columns through one `userColumns` list. The admin views keep their own columns.
- **Account settings' dates** use `formatDate(…, "long")`, so "November 1, 2026" (en-US, as elsewhere), not "1 November 2026".
- **`Checkbox`** needed `slot()` to map `$checked` to `data-checked`; the row is the `<label>`, so a link inside opens without ticking it. `CheckboxField` puts the error under the label as the input's description.
- **e2e:** the second-server helper moved into `tests/e2e/settings-server.ts`, shared with `email-verification-settings.spec.ts`. The admin login's refusal of a `policies` field is covered by login's (they share `Credentials`).
- **Register keeps "Already have an account? Sign in"** beside Back (a review fix that landed alongside).
- **Also in this run (not in the plan):** sign-up names now follow the profile's name rule (`input.ValidText`), so the agreement check isn't the only body validation before limits.

**Docs updated:** [api/README.md](../../../docs/api/README.md) (`POLICIES`, the `/policy/*` row, the manifest), [api/endpoints.md](../../../docs/api/endpoints.md), [api/auth.md](../../../docs/api/auth.md#terms-of-service-and-privacy-policy), [api/database.md](../../../docs/api/database.md), [api/events.md](../../../docs/api/events.md), [api/rate-limits.md](../../../docs/api/rate-limits.md), [api/seeding.md](../../../docs/api/seeding.md), [api/testing.md](../../../docs/api/testing.md), [naming.md](../../../docs/naming.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/README.md](../../../docs/ui/README.md) (with `marked` under Dependencies), [ui/design.md](../../../docs/ui/design.md), [ui/base-ui.md](../../../docs/ui/base-ui.md), [ui/forms-and-actions.md](../../../docs/ui/forms-and-actions.md), [ui/api.md](../../../docs/ui/api.md), [ui/testing.md](../../../docs/ui/testing.md), [deployment.md](../../../docs/deployment.md#terms-of-service-and-privacy-policy), [testing.md](../../../docs/testing.md), `.env.example`, AGENTS.md (rule 4).

## Not included

- **Asking existing accounts to agree, or anyone to re-agree** after a change (decision 14), and emailing users about changes.
- **A history of agreements**, or keeping proof of agreement after an account is deleted. Both are decisions for the re-acceptance plan and [compliance](../admin/compliance.md).
- **Showing agreement on the admin dashboard** (decision 17).
- **More documents** (cookie policy, acceptable use, a DPA). The reader is generic, so another is a Markdown file, a page file and a `prerender` entry.
- **A cookie banner.** The app sets only the cookies it needs to work (session, CSRF, sign-up, MFA, admin) and loads no tracking.
- **Filling in values** such as `APP_NAME` or a contact address in the documents: the operator writes them.
- **Translations, scheduled effective dates, a table of contents, a sitemap.**
- **Legal review.** The boilerplate is a starting point; the operator is responsible for its content.
