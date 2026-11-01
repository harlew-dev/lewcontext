# Rate limits and quotas

Code: `internal/httpapi/limits.go` (the limits, `Allow`, `Limit`, `meter`), `internal/httpapi/ratelimit` (what a limit is, its keys and messages, the in-memory counter), `internal/store/rate_limits.go` (`Store.Allow`), `internal/store/quotas.go`. Plan: [plans/features/implemented/rate-limits.md](../../plans/features/implemented/rate-limits.md).

## Three layers

```
request ─▶ security (Origin, content type) ─▶ RequireSession (session, CSRF)
                                              │
                                              ├─ GET:   api-read:per-user   (in memory)
                                              └─ write: api:per-user        (database)
                ─▶ RequireWorkspace ─▶ can(permission)
                ─▶ limit(<feature>:per-user, …)   only on routes that create rows
                ─▶ controller ─▶ store: quota check in the insert's transaction
```

- **Budgets** (`api:per-user`, `api-read:per-user`) are counted inside `RequireSession` (`meter`), so every signed-in route has one, including routes added later.
- **Feature limits** are named per route in `router/routes.go`, next to the permission, so a route shows everything that guards it on one line:

  ```go
  ws.Can("POST", "/projects", auth.ProjectStore, s.limit(projectPerUser, projects.store))
  mux.Handle("POST /api/workspaces", s.requireSession(s.limit(workspacePerUser, workspaces.store)))
  ```

- **Quotas** cap how many rows can exist at once. A rate limit slows growth; only a quota stops it. They are store checks inside the insert's transaction, returning a `store.QuotaError` that the handler answers with 422.

## Counting

- **Fixed windows.** A key's count starts at the first hit and resets when its window ends. Successful requests count too.
- **The same for every email.** Limits counted by an email that may or may not have an account (`password-reset:per-email`, `invitation:per-email`, `register:per-email`, `email-code:per-email`) count every address alike, so a 429 never says whether the address is registered.
- **Signed in, counted by user;** signed out (register, login, password reset), by client IP: `l.IPKey(httpapi.ClientIP(r))`, never `l.Key(httpapi.ClientIP(r))`. Counting signed-in traffic by IP would let one user lock out their whole office.
- **The client IP** is the connection's address, or behind a proxy the one in `CLIENT_IP_HEADER`, believed only from `TRUSTED_PROXIES` ([auth.md](auth.md#rate-limits), [deployment](../deployment.md#behind-a-proxy-or-load-balancer)). Without them, everyone behind one proxy shares an IP. **IPv6 is counted by its /64** (`Limit.IPKey`), since one user usually holds a whole /64 and could otherwise step around a limit by changing the last bits; IPv4 by the address.
- **After the CSRF check.** A forged request, or one without the user's token, is refused before it counts, so another site can't spend someone's budget.
- **In the database** (`rate_limits`: `key`, `hits`, `resets_at`), exact across servers, for writes, sign-in and mail. Keys are `<name>:<sha256 of the subject>` (an IPv4 address or IPv6 /64, an email or a user's `usr_` ID), or the bare name for a limit shared by everyone, so the table holds no addresses or IDs. The server's 15-minute cleanup deletes ended windows.
- **In memory** (`ratelimit.Memory`) for reads only: no database write per request, but each server process counts on its own, so with N servers a user gets N times the read budget. Ended windows are swept at most once a minute.
- **Order:** the budget first, then feature limits, and per-subject limits before shared ones, so a client its own limit refuses doesn't spend a limit shared by everyone.

## Every limit

Names are `<feature>:<counted by>` ([naming.md](../naming.md#rate-limits-httpapilimitsgo)), checked by `TestRateLimitNames`.

| Name | Limit | Counted by | Stored | Where | Refusal |
| --- | --- | --- | --- | --- | --- |
| `api:per-user` | 300 / 15 min | session user | database | every signed-in write, except the recovery routes below | 429 |
| `api-read:per-user` | 1,000 / 5 min | session user | memory | every signed-in GET: about 3 a second sustained | 429 |
| `admin-read:per-user` | 1,000 / 5 min | admin user | memory | every admin dashboard read (`RequireAdminSession`), the admin's own budget ([admin.md](admin.md)) | 429 |
| `workspace:per-user` | 10 / hour | session user | database | `POST /api/workspaces` | 429 |
| `project:per-user` | 100 / hour | session user | database | `POST …/projects`, across all of the user's workspaces | 429 |
| `invitation:per-user` | 20 / hour | session user | database | `POST …/invitations`, across all of the user's workspaces: each one emails an address the inviter chooses | 429 |
| `invitation:per-email` | 5 / hour | invited email | database | `POST …/invitations`, once the body is valid, from any inviter or workspace: caps mail to one inbox. Counted for every address, registered or not | 429 "This email address has had too many invitations recently. Try again in …" |
| `invitation:global` | 200 / hour | everyone | database | `POST …/invitations`, once the body is valid. Logs a warning | 503 "Invitations are busy. Try again later." |
| `auth:per-ip` | 60 / 15 min | IP | database | `LimitAuth`: register, login (and the admin door's), both signed-in password endpoints, deleting a workspace or the account, setting up, turning off and renewing the codes of two-factor authentication, the reset itself; directly on reset requests and checks, on a sign-in's second factor, on a sign-up's `register/check`, `/resend` and `/verify`, and on a block link's check, block and unblock (`/api/email-blocks`: the tokens are unguessable, so it only bounds load) | 429 |
| `auth:per-account` | 10 / 15 min | email | database | `AccountLimit`: register, login (the app's and the admin door's, sharing one allowance), both signed-in password endpoints (changing the password only without MFA), both deletions and the MFA routes that check the password (the session user's email), so a stolen session can't guess faster than a login. Password reset never touches it | 429 |
| `mfa:per-account` | 5 / 15 min | user (`usr_` ID) | database | every second-factor check: `POST /api/auth/login/mfa` and the admin door's `POST /api/admin/login/mfa` (after `auth:per-ip`, once the challenge names the user), an admin's step-up (`POST /api/admin/step-up`, so a stolen admin session guesses no faster than the door; it's counted with the admin session alone, no password first, so one stolen session can spend this allowance once, until its fifth wrong code ends it: [admin.md](admin.md#step-up)), enabling, disabling, new recovery codes, changing the password with MFA on. Each guess covers 3 of a million codes, so an attacker who has the password reaches about 0.14% a day. A challenge also ends after 5 wrong codes | 429 |
| `register:per-ip` | 10 / hour | IP | database | register, once the body is valid | 429 |
| `register:per-email` | 3 / hour | email typed | with `EMAIL_VERIFICATION=on`: register and `register/resend`, once the body is valid, every address alike, account or not (each one emails it) | 429 "This email address has had too many sign-up attempts recently. Try again in …" |
| `register:global` | 1,000 / hour | everyone | register (and `register/resend`), after the per-subject limits; with verification on it also caps sign-up mail. Logs a warning | 503 "Sign-up is busy. Try again later." |
| `account-exists:per-account` | 5 / day | the account (`usr_` ID) | **the `mail.registration` listener**, before sending "You already have an account" (`mail.AccountExistsPerAccount`, not in `limits.go`'s list; `TestRateLimitNames` checks it too). The one limit counted after the response, so it can't say which emails have accounts ([mail.md](mail.md#a-limit-in-a-listener)) | none: nothing is sent, and the requester can't tell |
| `email-code:per-email` | 10 / day | the email | every emailed code checked: `register/verify` (the sign-up's email) and `PATCH /api/auth/email-verification` (the account's), from any browser or IP. Each code also ends after 5 wrong tries | 429 "Too many attempts. Try again in …" |
| `email-verification:per-email` | 3 / hour | the session user's email | `POST /api/auth/email-verification` | 429 "You’ve asked for too many codes recently. Try again in …" |
| `email-verification:global` | 500 / hour | everyone | `POST /api/auth/email-verification`, after the per-email limit: caps the mail when `EMAIL_VERIFICATION_EXISTING=required` sends many accounts to verify. Logs a warning | 503 "Email verification is busy. Try again in a few minutes." |
| `password-reset:per-ip` | 10 / hour | IP | database | reset requests: one IP can't mail many people | 429 |
| `password-reset:per-email` | 3 / hour | email | database | reset requests, known email or not: caps mail to one inbox | 429 |
| `password-reset:global` | 100 / hour | everyone | database | reset requests: caps mail in a distributed flood. Logs a warning | 503 "Password reset is busy. Try again later." |
| `client-error:per-user` | 30 / hour | session user | database | `POST /api/client-errors`: errors one account's browsers report. The route skips the write budget (below), so this is its per-user limit. Each report is grouped in memory, never stored per report ([observability.md](observability.md#errors-from-the-browser)) | 429 |
| `client-error:global` | 500 / hour | everyone | database | `POST /api/client-errors`, after `client-error:per-user`, once the body is valid: caps what all browsers together add to `app_errors`. Logs a warning | 503 "Error reports are busy. Try again later." |
| `admin-action:per-user` | 60 / hour | admin user | database | the admin dashboard's account-and-access actions (`DELETE /api/admin/email-blocks`), after the role and the step-up (`LimitAdmin`). Every attempt that gets that far counts, refused or not (an address that isn't blocked too), so a stolen operator session can't act faster than the limit; `Action` in `router/routes.go` adds it to every action ([admin.md](admin.md#email-blocks)) | 429 |

Register counts `auth:per-ip` → `auth:per-account` → `register:per-ip` → `register:per-email` (verification on) → `register:global`; a sign-up's code `auth:per-ip` → `email-code:per-email`; an invitation `invitation:per-user` → `invitation:per-email` → `invitation:global`; an error report `client-error:per-user` → `client-error:global`; a reset request `auth:per-ip` → `password-reset:per-ip` → `password-reset:per-email` → `password-reset:global`. Each stops at the first refusal.

**Unmetered error reports:** `POST /api/client-errors` uses `RequireSessionUnmetered` too, for another reason: a page stuck in an error loop would otherwise spend its user's write budget and leave them unable to save anything. It is bounded by `client-error:per-user` (counted after the CSRF check, so no other site can spend it) and `client-error:global` instead, and only updates memory.

**Unmetered recovery routes:** `POST /api/auth/logout`, `DELETE /api/auth/sessions`, `POST /api/auth/password/verify`, `PATCH /api/auth/password`, `DELETE /api/auth/mfa` and `POST /api/auth/mfa/recovery-codes` use `RequireSessionUnmetered`, which skips the budget. Otherwise someone holding a stolen session could use up the owner's budget and stop them signing it out or changing the password. The two password routes and the two MFA routes keep `auth:*`, the hashing slots and (MFA) `mfa:per-account`; logout and revoking sessions only delete rows.

**Not limited:** a wrong admin key (no query is made, and guessing 122 bits or more is hopeless), `GET /api/health` and `/api/health/live` (orchestrator probes; put them behind an edge limiter if they're ever public) and the page gate (`GET /app/*`, a session lookup only). Page and anonymous floods belong to an edge limiter.

## Quotas

| Quota | Max | Checked in | Under | Refusal |
| --- | --- | --- | --- | --- |
| Workspaces a user owns | 20 (`store.MaxOwnedWorkspaces`) | `CreateWorkspace` | the user's row `FOR UPDATE` | 422 "You can own up to 20 workspaces." |
| Projects in a workspace | 1,000 (`store.MaxWorkspaceProjects`) | `CreateProject` | the workspace row `FOR UPDATE` (the workspace lock) | 422 "A workspace can have up to 1,000 projects." |
| Members plus live invitations in a workspace | 100 (`store.MaxWorkspaceMembers`) | `CreateInvitation`, `AcceptInvitation` | the workspace lock | 422 "A workspace can have up to 100 members, counting pending invitations." Accepting turns its invitation into a member, so it only fails for a workspace already full without it ("This workspace is full.") |
| Live invitations in a workspace | 50 (`store.MaxPendingInvitations`) | `CreateInvitation` | the workspace lock | 422 "A workspace can have up to 50 pending invitations." |

- The lock makes two creates at 999 make exactly one: without it both could pass.
- Being made an owner by someone else isn't refused at 20; the quota only stops creating workspaces.
- Lowering a quota later keeps existing rows and refuses only new ones. Seeders insert directly and aren't held to quotas.
- The 422 has no `field`: no form field is wrong. The web app shows the message in the create dialog.

## Responses

- **429** "Too many requests. Try again in 12 minutes." (or "Too many attempts…" for `auth:*`, `password-reset:per-*` and `email-code:per-email`). `Retry-After` and the message give the **time left in the window**: seconds rounded up, and minutes rounded up ("a minute", "12 minutes", "an hour"), or past an hour whole hours ("3 hours").
- **503** with the limit's `Busy` message for limits shared by everyone, with `Retry-After`, and `slog.Warn` the first time in a window.
- **`rate_limit.reached`** is recorded the first time a limit is reached in a window, with `limit` (`<name>:block`), `user` and `workspace` when known, `window` and `resetsAt`, and the client IP in its `ip` column ([events.md](events.md)). A flood writes one event per limit per window. **Every** refusal is counted in the `rate_limit_refused_total` metric (by `limit`, the same `<name>:block`), every quota refusal in `quota_refused_total` (by `quota`: `owned_workspaces`, `workspace_projects`, `workspace_members`), and every sign-in turned away because the hashing slots were full in `password_hash_busy_total` ([observability.md](observability.md#metrics)).
- The web app shows the API's message as it is, and SWR doesn't retry a 4xx read, so a 429 doesn't spend more of the budget ([ui/api.md](../ui/api.md#reading-data-useresource-libuse-resourcets)).

## Switching limits off for tests: `RATE_LIMITS`

`RATE_LIMITS=off` makes `Allow` pass everything and count nothing; quotas and the hashing slots still apply, and the server logs a warning at startup. It exists for the end-to-end tests, which sign up about a dozen users from one IP per run ([ui/testing.md](../ui/testing.md)). Production refuses it. The default is `on`.

## Limiting a new endpoint

1. **Signed in:** nothing to do for the budget; `RequireSession` meters it. Use `RequireSessionUnmetered` only for a route that must keep working when the budget is used up (recovery), or one a page calls on its own that mustn't spend it (error reports), with limits of its own; say why in `router/routes.go`.
2. **Creates rows or sends mail:** declare a `ratelimit.Limit` in `httpapi/limits.go` named `<feature>:per-user` (add it to `RateLimits`), and wrap the action with `s.Limit(httpapi.<Feature>PerUser, …)` in `router/routes.go`.
3. **Grows without bound:** add a quota check in the store method's transaction, under a row lock, returning `QuotaError`; map it to 422 in the controller.
4. **Signed out:** write the limits out in the controller with `h.Allow(w, r, limit, key)`, per IP first, then any shared (`global`) one with a `Busy` message. Count only once the body is valid if a shared limit is involved.
5. **Tests:** pre-fill the counter with `factory.RateLimits.State(factory.ForLimit(name, subject)).State(factory.Exhausted(n))` and send one request: 429 or 503, `Retry-After` within the window, one `rate_limit.reached` ([testing.md](testing.md)). Add the route to the table above.
