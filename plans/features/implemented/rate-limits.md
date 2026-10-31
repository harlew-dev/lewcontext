# Rate limits for every endpoint

Status: **implemented 2026-10-31** (see [How the build differs](#how-the-build-differs)). Docs: [api/rate-limits.md](../../../docs/api/rate-limits.md) (new), and updates to [auth.md](../../../docs/api/auth.md), [endpoints.md](../../../docs/api/endpoints.md), [events.md](../../../docs/api/events.md), [database.md](../../../docs/api/database.md), [seeding.md](../../../docs/api/seeding.md), [api/testing.md](../../../docs/api/testing.md), [api/README.md](../../../docs/api/README.md), [naming.md](../../../docs/naming.md), [deployment.md](../../../docs/deployment.md), [ui/api.md](../../../docs/ui/api.md), [ui/testing.md](../../../docs/ui/testing.md) and [testing.md](../../../docs/testing.md). Extends the limits from [forgot password](forgot-password.md) (`httpapi/limits.go`). Covers the endpoints planned in [workspace members](workspace-members.md) and [workspace invitations](workspace-invitations.md) too.

## Why

Only sign-in and password reset are limited today. Once signed in, a script can create workspaces and projects, rename things and read pages without bound. Every write also records an event, so the `events` table grows with it. Accounts themselves are cheap to make: register shares the 60 / 15 min sign-in budget per IP, which is up to 240 accounts an hour from one address.

Three things are missing:

1. **A budget on every signed-in request**, so a new endpoint is limited by default without anyone remembering to add a limit.
2. **Tighter limits on actions that create rows or send mail** (workspaces, projects, invitations, accounts).
3. **Quotas**: caps on how many rows can exist at once. A rate limit slows growth; only a quota stops it.

## Design

### Three layers

```
request ─▶ security (Origin, content type) ─▶ requireSession (session, CSRF)
                                              │
                                              ├─ GET:   api-read:per-user   (in memory)
                                              └─ write: api:per-user        (database)
                ─▶ requireWorkspace ─▶ can(permission)
                ─▶ limit(<feature>:per-user, …)   only on routes that create rows or send mail
                ─▶ handler ─▶ store: quota check in the insert's transaction
```

- **Budgets** (`api:per-user`, `api-read:per-user`) are applied inside `requireSession` to every signed-in route, so they are on by construction.
- **Feature limits** are named per route in `routes.go`, next to the permission:

  ```go
  ws.Can("POST", "/projects", auth.ProjectStore, s.limit(projectStorePerUser, projects.store))
  mux.Handle("POST /api/workspaces", s.requireSession(s.limit(workspaceStorePerUser, http.HandlerFunc(workspaces.store))))
  ```

  A route then shows everything that guards it on one line.
- **Quotas** are store checks inside the insert's transaction, returning typed errors that the handler maps to 422 (`ErrQuota{What, Max}`).

### Counted by user, not IP, once signed in

Signed-in limits count by the session's user. Forwarded headers aren't trusted, so everyone behind one proxy shares an IP. Counting signed-in traffic by IP would let one noisy user lock out their whole office. Only signed-out endpoints (register, login, reset, invitation check) count by IP.

### Counting only real requests

The budget is counted **after** the CSRF check. A request another site forges, or one without the token, is refused before it counts, so nobody can drain someone else's budget from another page.

### Unmetered recovery routes

`POST /api/auth/logout`, `DELETE /api/auth/sessions`, `POST /api/auth/password/verify` and `PUT|PATCH /api/auth/password` skip `api:per-user`. They use `requireSessionUnmetered`, a variant of `requireSession` without the budget. Otherwise an attacker holding a stolen session could use up the owner's budget and stop them signing the attacker out or changing the password. The two password routes keep their existing `auth:*` limits and hashing slots. Logout and revocation only delete rows.

### Reads are counted in memory

Every signed-in request already writes once: the session lookup updates `last_seen_at`. A database counter on every read would double that. Reads aren't about data growth anyway (pages are capped at 100 rows, queries time out after 10 s), so an approximate limit is enough:

- `httpapi/memlimit.go`: fixed windows in a mutex-guarded `map[string]window`, keyed by user ID, swept every minute. Standard library only, no new dependency.
- It is **per process**, so with N servers a user gets N times the budget. This is fine for load protection, and documented. Limits that must be exact and shared (writes, sign-in, mail) stay in the database.

### Database limits: the table and names

- `auth_attempts` no longer counts only sign-ins, so it is renamed **`rate_limits`** (`key`, `hits`, `resets_at`). That is an edit to the squashed `006` migration, followed by `make migrate-fresh CONFIRM=1 && make seed`. Its factory is renamed too.
- `Store.AllowAuth` → `Store.Allow`, and `allowAuth` → `allow`. `Allow` also returns `resets_at`.
- The event `auth.rate_limited` → **`rate_limit.reached`** (`limit`, `user`, `workspace`, `ip`, `window`, `resetsAt`; defined in [CAPTCHA challenges](../captcha-challenges.md#event-rate_limitreached)), still recorded only the first time a limit or level is reached per key per window.
- Names keep `<feature>:<counted by>`. A limit shared by everyone is counted by `global`: the existing `password-reset:all-accounts` becomes **`password-reset:global`** (constant `passwordResetGlobal`), and the new ones are `register:global` and `invitation:global`. Keys keep `<name>:<sha256 of subject>`, where the subject is now also a user's public ID, so the table holds no addresses or internal IDs.

### Responses

- **429** "Too many requests. Try again in 12 minutes." The `Retry-After` header and the message give the **time left in the window** (from `resets_at`), instead of the whole window as today. Auth limits keep their "Too many attempts" wording.
- **503** with the limit's `busy` message for limits shared by everyone, as today, plus `slog.Warn`.
- **422** with a message for quotas, such as "A workspace can have up to 1,000 projects." The `field` is left out, because no form field is wrong.

## Every endpoint

✓ = exists today. *Bold* = added by this plan. Endpoints from the members and invitations plans are marked *(planned)*.

| Endpoint | Budget | Specific limits | Quota |
| --- | --- | --- | --- |
| `GET /api/health` | none | none (decision 6) | |
| `POST /api/auth/register` | n/a (signed out) | ✓ `auth:per-ip`, `auth:per-account`, hashing slots; ***`register:per-ip`*, *`register:global`*** | |
| `POST /api/auth/login` | n/a | ✓ `auth:per-ip`, `auth:per-account`, hashing slots | |
| `POST /api/auth/password-reset` | n/a | ✓ `auth:per-ip`, `password-reset:*` | |
| `POST /api/auth/password-reset/check` | n/a | ✓ `auth:per-ip` | |
| `PATCH /api/auth/password-reset` | n/a | ✓ `auth:per-ip`, hashing slots | |
| `POST /api/invitations/check` *(planned)* | n/a | `auth:per-ip` | |
| `GET /api/auth/me` | ***read*** | | |
| `PATCH /api/auth/me` | ***write*** | | |
| `POST /api/auth/logout` | unmetered | | |
| `DELETE /api/auth/sessions` | unmetered | | |
| `POST /api/auth/password/verify` | unmetered | ✓ `auth:per-ip`, `auth:per-account`, hashing slots | |
| `PATCH /api/auth/password` | unmetered | ✓ `auth:per-ip`, `auth:per-account`, hashing slots | |
| `GET /api/workspaces` | ***read*** | | |
| `POST /api/workspaces` | ***write*** | ***`workspace:per-user`*** | ***20 owned workspaces per user*** |
| `GET /api/workspaces/{ws}` | ***read*** | | |
| `PATCH /api/workspaces/{ws}` | ***write*** | | |
| `GET …/projects`, `GET …/projects/{prj}` | ***read*** | | |
| `POST …/projects` | ***write*** | ***`project:per-user`*** | ***1,000 projects per workspace*** |
| `PATCH …/projects/{prj}` | ***write*** | | |
| `GET …/members` | read | | |
| `PATCH …/members/{usr}`, `DELETE …/members/{usr}`, `DELETE …/membership` | write | | |
| `GET …/invitations` *(planned)* | read | | |
| `POST …/invitations` *(planned)* | write | `invitation:per-user`, `invitation:global` | 50 pending; ***100 members + pending per workspace*** |
| `DELETE …/invitations/{inv}` *(planned)* | write | | |
| `POST /api/invitations/accept` *(planned)* | write | | ***100 members per workspace*** |
| `DELETE …/projects/{prj}` *([deletion](deletion.md), planned)* | write | | |
| `DELETE /api/workspaces/{ws}` *(deletion, planned)* | write | `auth:per-ip`, `auth:per-account`, hashing slots (password check) | |
| `DELETE /api/auth/me` *(deletion, planned)* | write | `auth:per-ip`, `auth:per-account`, hashing slots (password check) | |
| `GET /api/workspaces?soleOwner=true` *(deletion, planned)* | read | | |
| `GET /app`, `GET /app/*` (page gate) | none: a session lookup only | | |

### The new limits

| Name | Limit | Counted by | Stored | Refusal | Purpose |
| --- | --- | --- | --- | --- | --- |
| `api:per-user` | 300 / 15 min | session user | database | 429 | every signed-in write except the recovery routes |
| `api-read:per-user` | 1,000 / 5 min | session user | memory, per process | 429 | every signed-in GET: about 3 a second sustained, with room for SWR's focus refreshes and the invitations list's polling |
| `register:per-ip` | 10 / hour | connection IP | database | 429 | account creation from one address |
| `register:global` | challenge above 200, block above 1,000 / hour ([CAPTCHA challenges](../captcha-challenges.md)) | everyone | database | 428 challenge, then 503 "Sign-up is busy. Try again later." | caps account creation during a distributed flood; also logs a warning |
| `workspace:per-user` | 10 / hour | session user | database | 429 | workspace creation |
| `project:per-user` | 100 / hour | session user | database | 429 | project creation, across all of the user's workspaces |

Specific limits run after the budget, and per-subject limits run before shared ones (the existing rule), so a refused request doesn't spend the shared budget. Register counts `register:per-ip` → `register:global` after `auth:per-ip`, and only once the body is valid, so malformed requests don't spend the shared limit.

### Quotas

| Quota | Max | Checked in | Concurrency |
| --- | --- | --- | --- |
| Workspaces a user owns | 20 | `CreateWorkspace` | the user's row `FOR UPDATE`, then count `workspace_user` rows with role `owner` |
| Projects in a workspace | 1,000 | `CreateProject` | the workspace row `FOR UPDATE` (the lock the members plan already uses), then count |
| Members + pending invitations in a workspace | 100 | `CreateInvitation`, `AcceptInvitation` | the same workspace lock |
| Pending invitations in a workspace | 50 | `CreateInvitation` | *(invitations plan)* |

- Without the lock, two concurrent creates at 999 could both pass and make 1,001.
- Being promoted to owner by someone else isn't refused at 20. The quota only stops creating new workspaces.
- If a quota is lowered later, existing rows stay. Only new ones are refused.

## Web app

- **Don't retry on 429.** `SWRConfig` (`root.tsx`) has `errorRetryCount: 2`, so a refused read is retried twice and spends more of the budget. `onErrorRetry` skips 4xx responses (`ApiError.status`), and the existing Retry button stays.
- The API's message is shown as it is: `ErrorNotice` for mutations (`useAction`), the list's error notice for reads. No new UI.
- **Quotas:** the create dialogs show the 422 message. Hiding "New project" in advance would need the count, so it isn't done (decision 7).

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A new endpoint ships unlimited | The budgets are inside `requireSession`, so every signed-in route has one. A signed-out route needs a limit written out; signed-out routes are rare and reviewed (this table). |
| An attacker drains a user's budget | Counted after CSRF and Origin, so only requests carrying the user's own token count. Recovery routes are unmetered, so even a stolen session can't block logout, revocation or a password change. |
| One proxy IP locks out many people | Signed-in limits count by user. |
| Distributed sign-up abuse | `register:per-ip` plus `register:global` (503 and a warning). Per-account limits and quotas then cap what each account can create. |
| Unbounded data growth | Quotas, checked under a row lock. |
| Event table flood | Every write is under a budget. A refusal records one `rate_limit.reached` per key per window. |
| Read flood | The in-memory read budget, page caps and query timeouts. |
| Keys reveal who did what | Subjects are SHA-256 digests. |
| Lockout through the shared limits | `register:global` is a deliberate trade-off, as `password-reset:global` is. Signing in isn't affected. |

## Tests

- **Go unit:**
  - `memlimit`: a window resets, the sweep drops old keys, concurrent use under `-race`;
  - `Retry-After` and the message round the time left up to whole minutes.
- **Go integration:** to avoid sending 300 real requests, use the renamed factory to set a key's `hits` to its limit, then send one request:
  - **Each new limit:** request N+1 gets 429 or 503 with `Retry-After` no longer than the window, and one `rate_limit.reached` event with its `limit` (name and level) and `user`;
  - **Per user:** user A at the limit doesn't affect user B on the same IP;
  - **Recovery routes:** with `api:per-user` used up, logout, revocation and a password change still succeed, and a project create is refused;
  - **CSRF first:** a request with a bad CSRF token doesn't count;
  - **Order:** a request refused by `register:per-ip` doesn't spend `register:global`;
  - **Quotas:** the 21st owned workspace gets 422; the 1,001st project gets 422 (factory bulk insert); two concurrent creates at 999 make exactly one; invite and accept at 100 members get 422;
  - **Read budget:** request 1,001 in a window gets 429 (memory, so fast).
- **Vitest:** a 429 isn't retried by SWR; the message shows on a mutation and on a list with Retry.
- **Existing tests:** the auth limit tests pass after the rename (`rate_limits`, `rate_limit.reached`).

## Docs

- A new `docs/api/rate-limits.md`: the layers, the whole table above, budgets, quotas, and how to limit a new endpoint. It is indexed in `docs/README.md` and the AGENTS.md table. The rate-limit section of `docs/api/auth.md` becomes a link to it.
- Also update `docs/api/endpoints.md` (429, 422 for quotas), `docs/api/events.md` (`rate_limit.reached`), `docs/api/database.md` (`rate_limits`) and AGENTS.md ("keep auth rate limits" → "keep rate limits and quotas").

## Decisions (confirmed 2026-10-31)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Default budgets | `api:per-user` 300 writes / 15 min in the database, and `api-read:per-user` 1,000 reads / 5 min in memory. |
| 2 | Reads in memory | Yes: approximate and per process, but no extra write per request. Writes, sign-in and mail stay exact in the database. |
| 3 | Specific limits | `workspace:per-user` 10/h, `project:per-user` 100/h, `register:per-ip` 10/h, `register:global` 1,000/h (503). Updates (renames, edits, role changes) get the budget only. |
| 4 | Quotas | 20 owned workspaces per user, 1,000 projects per workspace, 100 members + pending invitations per workspace. |
| 5 | Unmetered routes | Logout, revoking other sessions, and verifying or changing the password. |
| 6 | `GET /api/health` | Unlimited. Orchestrator probes call it, and it is one ping. Put it behind an edge limiter if it's ever exposed publicly. |
| 7 | Showing quotas in the UI | No: show the 422 when it happens. Counts and remaining quota in the UI can come later. |
| 8 | Renames | `auth_attempts` → `rate_limits`, `auth.rate_limited` → `rate_limit.reached`, `AllowAuth` → `Allow`, `password-reset:all-accounts` → `password-reset:global` (with its test and `docs/api/auth.md`). Migrations are squashed and the rows are short-lived, so nothing needs migrating. |
| 9 | `Retry-After` | The time left in the window, not the whole window. |
| 10 | Configuration | Values stay hard-coded in `limits.go` and the store. Add environment variables only when a deployment needs different numbers. |
| 11 | The end-to-end tests | One run signs up about 12 users from one IP and has one user own 25 workspaces, past `register:per-ip`, `workspace:per-user` and the owned-workspace quota. A `RATE_LIMITS=off` setting, refused in production like `MAIL_DRIVER=file`, switches every rate limit off (quotas and hashing slots stay) for the server the e2e tests run against. The switcher test adds its workspaces past the quota as memberships through `TEST_DATABASE_URL`, and is skipped without it. |
| 12 | Plan order | Built before [convention checks](../convention-checks.md): `TestRateLimitNames` is a test of its own in `httpapi`, which that plan can absorb. |

## Not included

- An edge or proxy limiter, and trusting forwarded IP headers. A scaled deployment still needs one (`auth.md` already says so). That is also where page and anonymous-request floods belong.
- Limits per workspace (shared by its members), on top of per user.
- Plans or tiers with different quotas.
- `RateLimit-*` response headers.
- Exact read limits across several servers (they would need the database or Redis).

## How the build differs

- **`QuotaError{What, Max}`**, not `ErrQuota`, matching `LastOwnerError`. The handlers write the 422 message; the quotas are `store.MaxOwnedWorkspaces` and `store.MaxWorkspaceProjects` in `store/quotas.go`.
- **The members + pending invitations quota isn't built:** no endpoint adds members yet. The [invitations plan](workspace-invitations.md) adds it to `CreateInvitation` and `AcceptInvitation`, under the same workspace lock.
- **`rate_limit.reached` is built here**, with the payload the [CAPTCHA plan](../captcha-challenges.md#event-rate_limitreached) defines. Every limit has one level for now, so `limit` is always `<name>:block`. The `rate_limit` area was added to the event-name test's list.
- **The column is `hits`** (was `attempts`), and `Store.Allow` returns `resetsAt` as a fourth result.
- **`rateLimit` gained two fields:** `attempts` (the 429 says "Too many attempts", for `auth:*` and `password-reset:per-*`) and `inMemory` (counted by `memoryLimiter`), so `s.allow` serves both stores.
- **The memory limiter sweeps lazily:** on the next hit, at most once a minute, so it needs no goroutine or shutdown.
- **`RATE_LIMITS=off`** (decision 11) for the e2e tests, refused in production; CI starts the server with it. The switcher e2e test was split from the rename test: it creates 19 workspaces through the API, checks the 21st gets 422, and adds 4 memberships through `TEST_DATABASE_URL`.
- **`TestRateLimitNames`** (decision 12) lives in `httpapi/limits_test.go` and also checks that only `global` limits have a `busy` message.
- **SWR's retry rule** is `swrConfig` in `apps/web/src/lib/swr.ts`, used by `root.tsx`.
