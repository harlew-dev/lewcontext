# Endpoints

All routes are registered in `apps/api/internal/httpapi/router/routes.go`. The browser reaches them through the Next proxy at `/api/*`.

`GET /metrics` isn't an API route and never answers on the public listener: with `METRICS_ADDR` set, a listener of its own serves it, in the Prometheus text format, behind `METRICS_TOKEN` when set ([observability.md](observability.md#metrics-for-an-external-system)).

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| GET | `/api/health` | Public | Readiness: `{"status":"ok"}`, or 503 if the database is down or the server is shutting down ([observability.md](observability.md#health)) |
| GET | `/api/health/live` | Public | Liveness: `{"status":"ok"}` while the process answers; no database |
| GET | `/api/policies` | Public, not limited | 200 `{enabled: false}`, or `{enabled: true, termsOfService, privacyPolicy}` (the versions, YYYY-MM-DD) with `POLICIES=on`. Always registered, so the web app tells "off" from "API down" (`account.Policies.Show`) |
| POST | `/api/auth/register` | Origin, JSON, rate limits | With `EMAIL_VERIFICATION=on` (the default): 202 `{verificationRequired: true, email}` and the registration cookie, the same for every valid email, taken or not; no account yet ([auth.md](auth.md#email-verification)). With it off: 201 `{user, csrfToken, expiresAt, signedInAt, emailVerification}` and a new session cookie, or 409 for a taken email or a blocked one (the same answer). A name of 1–80 characters with an invisible formatting character is 422 with `field: "name"`, checked from the body after `auth:per-ip` (which counts the address, never the email) and before every other limit or lookup. Body `{name, email, password, policies?}`: with `POLICIES=on`, `policies: {termsOfService, privacyPolicy}` must be the current versions, or 422 `field: "policies"` ("Agree to the Terms of Service…", or "…changed since this page opened…"), checked from the body after `auth:per-ip` (which counts the address, never the email) and before every other limit or lookup; with `off` it is accepted and ignored |
| POST | `/api/auth/register/check` | Origin, JSON, the registration cookie, `auth:per-ip` | 200 `{email, expiresAt}` of this browser's sign-up; 401 (and the cookie cleared) when it has ended. Only with verification on |
| POST | `/api/auth/register/resend` | Origin, JSON, the registration cookie, `auth:per-ip`, `register:per-email`, `register:global`, a challenge (`register`, 428) | 202 `{verificationRequired: true, email}`: a new code (or "you already have an account") for this browser's sign-up, 15 more minutes, its tries back to 5; 401 when it has ended |
| POST | `/api/auth/register/verify` | Origin, JSON, the registration cookie, `auth:per-ip`, `email-code:per-email` | 201 `{user, csrfToken, expiresAt, signedInAt, emailVerification}` and a session cookie for the right `{code}` (6 digits, spaces allowed): the account, verified. 422 with `field: "code"` for a wrong one; 401 when the sign-up has ended (expired, the 5th wrong code, or the address blocked since the code was sent); 409 when an account took the email since the code was sent |
| POST | `/api/auth/email-verification` | Session (unverified allowed), CSRF, Origin, `email-verification:*` limits | 202: a code is emailed to the account's address a moment later. 409 when it's already verified. Only with verification on |
| PATCH | `/api/auth/email-verification` | Session (unverified allowed), CSRF, Origin, `email-code:per-email` | 200 `{user}`, its email verified, for the right `{code}`. 422 with `field: "code"`; 410 when no code is live (none sent, expired, or the 5th wrong one); 409 when already verified |
| GET | `/api/auth/email-change` | Session (unverified allowed) | 200 `{pending: null}` or this session's `{pending: {email, expiresAt, state}}`, state `sending` or `code` |
| POST | `/api/auth/email-change` | Session (unverified allowed), CSRF, Origin, `LimitAuth`, fresh password/MFA proof and `email-change:*` | `{email, currentPassword}` without MFA, `{email, code}` with MFA. Verification on: 202 pending request, old address unchanged. Off: 200 ordinary complete session response, new email unverified, works without mail. 422 field errors; generic 409 for unavailable direct-mode destinations |
| PATCH | `/api/auth/email-change` | Session (unverified allowed), CSRF, Origin, `email-code:per-email`, completion cap | `{code}`: 200 complete rotated session response. Wrong issued code 422 on `code`; missing/expired/replaced/cancelled/fifth wrong or verification off 410; not issued yet 409 `{error, pending: true}`; destination now unavailable generic 409 ending request |
| DELETE | `/api/auth/email-change` | Session (unverified allowed), CSRF, Origin | 204; cancel this session's pending request, idempotently |
| POST | `/api/auth/login` | Origin, JSON, rate limit | 200 `{user, csrfToken, expiresAt, signedInAt}` and a rotated session cookie; with two-factor authentication on, 200 `{mfaRequired: true}` and the challenge cookie instead, with no session ([auth.md](auth.md#two-factor-authentication)). A suspended account's right password (MFA off): 403 `{error, suspended: true}` ([auth.md](auth.md#suspended-accounts)) |
| POST | `/api/auth/login/mfa` | Origin, JSON, the challenge cookie, `auth:per-ip`, `mfa:per-account` | 200 `{user, csrfToken, expiresAt, signedInAt}` and a session cookie for a right `{code}` (authenticator or recovery code). 422 with `field: "code"` for a wrong one; 401 when the challenge is missing, expired or has had its 5 tries (the password is needed again); 403 `{error, suspended: true}` for a suspended account's right code |
| GET | `/api/auth/me` | Session (unverified allowed) | `{user, csrfToken, expiresAt, signedInAt, emailVerification}`; `emailVerification` is `off`, `optional` or `required`. The `user` (here and in every session answer) carries `policies: {termsOfService, privacyPolicy, acceptedAt}`, what it agreed to at sign-up, or `null` |
| PATCH | `/api/auth/me` | Session, CSRF, Origin | 200 with the updated user. Body `{name}` (trimmed, 1–80 characters, no invisible formatting characters: `input.ValidText`); email changes use their separate singleton, so other fields are rejected (400) |
| DELETE | `/api/auth/me` | Session, CSRF, Origin, rate limit | 204; the account, its sessions, memberships and the events about it are deleted, and the cookie is cleared. Body `{password}`. 422 with `field: "password"`; 409 for the only owner of a workspace, listing them ([below](#deleting)) |
| POST | `/api/auth/export` | Session, CSRF, Origin, rate limit, `export:per-user` | 200, a JSON file to download (`Content-Disposition: attachment`): the account's data, "Download my data" ([compliance.md](../compliance.md#download-my-data)); recorded as `auth.data_exported`. Body `{password}`. 422 with `field: "password"`; 429 past 5 a day |
| POST | `/api/auth/logout` | Session (unverified allowed), CSRF, Origin | 204; session revoked, cookie cleared |
| DELETE | `/api/auth/sessions` | Session (unverified allowed), CSRF, Origin, rate limit | 204; every other session revoked (recorded only when there was one) |
| POST | `/api/auth/password/verify` | Session, CSRF, Origin, rate limit | 204 if `{password}` is the current password; 422 with `field: "password"` if not |
| PATCH | `/api/auth/password` | Session, CSRF, Origin, rate limit (`mfa:per-account` with MFA on) | 200 `{user, csrfToken, expiresAt, signedInAt}` and a new session cookie; every earlier session revoked. Body `{currentPassword, newPassword}`, or with MFA on `{newPassword, code}` (an authenticator or recovery code; the password is ignored). 422 with `field` (`currentPassword`, `code` or `newPassword`) when refused |
| POST | `/api/auth/mfa/setup` | Session, CSRF, Origin, rate limit | 200 `{secret, uri}`: a new authenticator secret, pending until confirmed, shown only here. Body `{password}`. 422 with `field: "password"`; 409 when MFA is on |
| POST | `/api/auth/mfa` | Session, CSRF, Origin, `mfa:per-account` | 201 `{recoveryCodes}` (10, shown only here); MFA is on and every other session is signed out. Body `{code}` from the app. 422 with `field: "code"`; 409 when MFA is on, or no setup from the last 15 minutes is pending |
| DELETE | `/api/auth/mfa` | Session, CSRF, Origin, rate limit, `mfa:per-account` | 204; MFA is off, its secret, recovery codes and challenges deleted. Body `{password, code}` (an authenticator or recovery code). 422 with `field` (`password` or `code`); 409 when it's off, or for an admin |
| POST | `/api/auth/mfa/recovery-codes` | Session, CSRF, Origin, rate limit, `mfa:per-account` | 201 `{recoveryCodes}`, replacing every earlier one. Body `{password, code}`. 422 with `field`; 409 when MFA is off |
| POST | `/api/auth/password-reset` | Origin, JSON, reset rate limits | 202 for every valid `{email}`, whether or not an account uses it; the link is sent later by a listener. 422 with `field: "email"` for a malformed address, 429 per IP or email, 503 when the limit for everyone is reached or `MAIL_DRIVER=off` ([auth.md](auth.md#password-reset-by-link)) |
| POST | `/api/auth/password-reset/check` | Origin, JSON, rate limit | 204 if `{token}` is a live reset link; 422 with `field: "token"` if not |
| PATCH | `/api/auth/password-reset` | Origin, JSON, rate limit | 200 `{email}` after setting `newPassword` with a live `token`; every session revoked, nobody signed in. 422 with `field` (`token` or `newPassword`) when refused |
| GET | `/api/workspaces` | Session | One page of your workspaces with your role and permissions, newest first ([pagination.md](pagination.md)). `canLeave=false` lists only those you can't leave |
| POST | `/api/workspaces` | Session, CSRF, Origin, `workspace:per-user` | 201 with the created workspace; you are its `owner`. 422 when you already own 20 |
| GET | `/api/workspaces/{workspace}` | Member | The workspace with your role and permissions |
| PATCH | `/api/workspaces/{workspace}` | `workspace.update`, CSRF, Origin | 200 with the renamed workspace; 403 for members and viewers |
| DELETE | `/api/workspaces/{workspace}` | `workspace.destroy`, CSRF, Origin, rate limit | 204; its projects and memberships go too, its events stay. Body `{name, password}`. 403 for everyone but owners; 422 with `field` (`name`: not its current name, or `password`) |
| GET | `/api/workspaces/{workspace}/members` | `workspace.member.index` | One page of members, by role then name ([pagination.md](pagination.md)); 403 for members and viewers |
| PATCH | `/api/workspaces/{workspace}/members/{member}` | `workspace.member.update`, CSRF, Origin | 200 with the member. Body `{role}`. 403 when their role or the new one isn't in your `assignableRoles`, 404 for someone not in this workspace, 409 for the last owner, 422 for an unknown role |
| DELETE | `/api/workspaces/{workspace}/members/{member}` | `workspace.member.destroy`, CSRF, Origin | 204; their account stays. 403 not assignable, 404, 422 for yourself ("To leave, use Leave workspace.") |
| DELETE | `/api/workspaces/{workspace}/membership` | Member, CSRF, Origin | 204: you leave. 409 for the last owner |
| GET | `/api/workspaces/{workspace}/invitations` | `workspace.invitation.index` | One page of invitations, newest first, expired ones included for 30 days ([below](#invitations)); 403 for members and viewers |
| POST | `/api/workspaces/{workspace}/invitations` | `workspace.invitation.store`, CSRF, Origin, `invitation:*` limits | 201 with the invitation; the email goes out a moment later. Body `{email, role}`. 403 when the role isn't in your `assignableRoles`; 422 with `field` (`email`: malformed, already a member or already invited; `role`: owner or unknown); 422 for the 51st live invitation or the member quota; 503 with `MAIL_DRIVER=off` or when `invitation:global` is reached; 401, as a dead session, when your account was [suspended](admin.md#suspending-an-account) while the request was on its way |
| DELETE | `/api/workspaces/{workspace}/invitations/{invitation}` | `workspace.invitation.destroy`, CSRF, Origin | 204; its link stops working. 403 for an invitation to a role you can't give, 404 |
| POST | `/api/invitations/check` | Origin, JSON, `auth:per-ip` | 200 `{workspace: {name}, invitedBy: {name, email}, email, role, expiresAt}` for a live `{token}`; 422 with `field: "token"` otherwise. Works signed out |
| POST | `/api/email-blocks/check` | Origin, JSON, `auth:per-ip` | 200 `{email, registered, blocked}` for a live block link's `{token}`; 422 with `field: "token"` otherwise. Works signed out ([below](#email-blocks)) |
| POST | `/api/email-blocks` | Origin, JSON, `auth:per-ip` | 204: the link's address is blocked (already blocked too). Body `{token}`. 409 "This address has an account. Delete your account first."; 422 with `field: "token"` for a dead link |
| DELETE | `/api/email-blocks` | Origin, JSON, `auth:per-ip` | 204: the link's address is unblocked (not blocked too). Body `{token}`. 422 with `field: "token"` for a dead link |
| POST | `/api/client-errors` | Session (outside the write budget), CSRF, Origin, `client-error:per-user`, `client-error:global` | 202, no body (429 or 503 past a limit): the web app's own error, grouped with the server's (`source: client`) and stored with the next flush. Body `{message, stack, route, release}`, at most 4 KiB: `message` required (422 when blank), `route` one of the web app's page patterns or stored as `unknown`; 400 for an unknown field or a larger body. Never a URL ([observability.md](observability.md#errors-from-the-browser)) |
| POST | `/api/invitations/accept` | Session, CSRF, Origin | 200 with the workspace (your `role`, `permissions`). Body `{token}`. 403 when you're signed in as another email; 422 with `field: "token"` for a dead link, or without it when the workspace is full |
| GET | `/api/workspaces/{workspace}/projects` | Member | One page of the workspace's projects ([pagination.md](pagination.md)) |
| POST | `/api/workspaces/{workspace}/projects` | `project.store`, CSRF, Origin, `project:per-user` | 201 with the created project; 403 for viewers; 422 when the workspace has 1,000, or with `field` `name` (1–100 characters) or `description` (up to 500, line breaks and tabs allowed), neither with an invisible formatting character (`input.ValidText`) |
| GET | `/api/workspaces/{workspace}/projects/{project}` | Member | The project, or 404 |
| PATCH | `/api/workspaces/{workspace}/projects/{project}` | `project.update`, CSRF, Origin | 200 with the updated project, or 404; 403 for viewers; 422 with `field` as for creating |
| DELETE | `/api/workspaces/{workspace}/projects/{project}` | `project.destroy`, CSRF, Origin | 204, or 404; 403 for members and viewers |

Every route that needs a session also counts against the user's budget (300 writes per 15 minutes, 1,000 reads per 5 minutes), except logout, `DELETE /api/auth/sessions`, the two password routes, `DELETE /api/auth/mfa`, `POST /api/auth/mfa/recovery-codes` and `POST /api/client-errors`; revoking sessions and client errors have limits of their own ([rate-limits.md](rate-limits.md)). Every response with the signed-in `user` includes `mfaEnabledAt` (when two-factor authentication was turned on, or null) and `emailVerifiedAt` (when the email was verified, or null). With `EMAIL_VERIFICATION_EXISTING=required`, every signed-in route except those marked "unverified allowed" answers an account that never verified its email 403 `{error: "Verify your email to continue.", verificationRequired: true}` ([auth.md](auth.md#email-verification)). "Origin" means the exact `APP_ORIGIN`, no cross-site fetch metadata, and `Content-Type: application/json` on POST, PATCH and any DELETE with a body. "Member" means a session plus a `workspace_user` row for that workspace; a permission such as `project.store` means a member whose role grants it ([auth.md](auth.md#workspaces-and-roles)).

The admin dashboard's routes, `/api/admin/*` (sign-in, signing out your other admin sessions, Users, Workspaces and their projects, Events (filtered by name groups, outcome and window, with its charts' summary on the newest page), the catalogue (with the event groups), the Events CSV, Mail and deliveries, the Monitoring pages, one address's page and one error group's ([monitoring.md](monitoring.md)), Admins, Email blocks and unblocking one, IP blocks, the step-up and the [actions](admin.md#actions)), are in [admin.md](admin.md#api): every one needs the right `X-Admin-Key` from an allowed address (otherwise it answers as an unknown route), and all but the sign-in steps an admin session made under that key.

**Every response** (apart from `net/http`'s own 400 and 431 for a request it can't read) carries `X-Request-ID`, the request's ID, always the server's own, which is on its log lines and on any event it recorded; a **500** answers only `{"error": "Something went wrong. Please try again.", "requestId": "req_…"}` ([observability.md](observability.md#request-ids)).

**Unknown routes** answer `404 {"error": "Not found."}` (JSON, `unknownRoutesAsJSON`), the same as an admin route without its key. A known path with the wrong method stays 405.

## IDs

Every record in a response is identified by its **public ID**: a prefix and 14 base62 characters (`usr_…`, `wsp_…`, `prj_…`), generated by PostgreSQL ([database.md](database.md#public-ids)). Internal numeric IDs never appear in requests or responses. A path ID with the wrong prefix or shape is a 404 without a query.

## Workspaces

```json
{ "id": "wsp_2yEHYLwiwPVhLW", "name": "Acme", "role": "admin",
  "permissions": ["project.destroy", "project.store", "project.update", "workspace.invitation.destroy", "…"],
  "assignableRoles": ["member", "viewer"], "canLeave": true,
  "createdAt": "2026-10-30T12:00:00Z", "updatedAt": "2026-10-30T12:00:00Z" }
```

- **Body for POST/PATCH:** `name` (trimmed, 1–80 characters, no invisible formatting characters: `input.ValidText`). Unknown fields, such as `id` or `role`, are rejected.
- **Membership:** the `{workspace}` routes go through `RequireWorkspace`, which looks the workspace up through your membership. Another user's workspace, a missing one and a malformed ID all get the same 404.
- **`canLeave`**: whether you may leave (`DELETE …/membership`). False only for a workspace's only owner. Read with the role in the same query, so the web app never loads the member list to tell.
- **`permissions`** (sorted) and **`assignableRoles`** (most access first) come from your role, at no query cost, in every workspace response including the list. Empty lists for a viewer, never `null`.
- **The list** takes `page` and `perPage` only (no search or sort). A new account has none; the web app's onboarding creates the first.

## Members

```json
{ "id": "usr_2yEHYLwiwPVhLW", "name": "Ada Lovelace", "email": "ada@example.com",
  "role": "admin", "addedAt": "2026-10-30T12:00:00Z" }
```

- **`{member}` and `id`** are the user's `usr_` public ID; `addedAt` is when they joined the workspace (`workspace_user.created_at`).
- **The list** takes `page`, `perPage` and `q` (name or email) and is always ordered owner, admin, member, viewer, then by name.
- **Scope:** a member is looked up by the workspace from the path and their public ID, so a user in another workspace, or no user, is the same 404.
- **Leaving** is `DELETE` on your own membership, a singleton resource, rather than a verb route. Every member may leave except the last owner ([auth.md](auth.md#workspaces-and-roles)).

## Projects

```json
{ "id": "prj_j292s2S2j7Qx4B", "name": "Website redesign", "description": "…", "status": "In progress",
  "color": "violet", "createdAt": "2026-10-30T12:00:00Z", "updatedAt": "2026-10-30T12:00:00Z" }
```

- **Body for POST/PATCH:** `name` (1–100 characters), `description` (≤ 500), `status` (`Planned`, `In progress`, `Completed`), `color` (`violet`, `blue`, `amber`, `emerald`, `rose`). Unknown fields are rejected.
- **PATCH replaces the whole editable representation.**
- **DELETE** removes it at once; nothing else references a project. Its events stay.
- **Scope:** projects belong to a workspace, taken from the path; a workspace ID is never accepted in the body or returned. A project ID from another workspace, even one of yours, behaves exactly like a missing one (404).

## Invitations

```json
{ "id": "inv_8kQ2mZ1xT4bLwN", "email": "ada@example.com", "role": "member",
  "invitedBy": { "id": "usr_2yEHYLwiwPVhLW", "name": "Harrison" },
  "sentAt": "2026-10-31T12:00:01Z", "createdAt": "2026-10-31T12:00:00Z", "expiresAt": "2026-11-07T12:00:00Z" }
```

- **`sentAt`** is null until the listener has emailed the link; **`invitedBy`** is null once that account is gone (the invitation stays valid).
- **The list** takes `page` and `perPage`. A workspace has at most 50 live invitations; expired ones are listed until they're 30 days past expiry.
- **`check` and `accept`** are named actions on the link: the record is found by the token in the body, never in the URL ([auth.md](auth.md#invitations)).
- **An invitation to a blocked address** is created, listed and gets its `sentAt` like any other; only its email isn't sent ([below](#email-blocks)).

## Email blocks

```json
{ "email": "bob@example.com", "registered": false, "blocked": true }
```

- **Reached only through a block link** (the token in a blockable email, in the body, never the URL): the page's `check`, then `store` to block the address it went to, `destroy` to unblock it. The controller is `account.EmailBlocks`, in the `account` area like the other signed-out links ([naming.md](../naming.md#controllers-internalhttpapi)).
- **`registered`** addresses can't be blocked (409). Neither route says anything an address's owner doesn't already know: only the inbox has the link.
- **No other route says an address is blocked** ([auth.md](auth.md#blocking-an-email-address)).

## Deleting

Deletion is permanent and immediate: one transaction, with `ON DELETE CASCADE` removing what belongs to the row. Plan: [plans/features/implemented/deletion.md](../../plans/features/implemented/deletion.md).

| Deleted | Removed with it | Kept |
| --- | --- | --- |
| project | nothing else | its events (payloads hold `prj_…` only) |
| workspace | memberships, projects | its events, with `workspace_id` set to null |
| account | sessions, memberships, reset link, the events about it (`events.user_id`); **its email (and the name beside it) in every other event**, replaced by `"[deleted]"` ([compliance.md](../compliance.md#erasure)) | events it acted in for others, with `actor_id` set to null; their IPs |

- **Workspace:** the typed `name` must be the current name, checked again under the workspace lock, so a tab opened before a rename can't delete it under the old name. Then the owner's password.
- **Account:** the password, then every workspace you own is locked in `id` order and checked with the same rule as `canLeave`. The only owner of any of them gets:

  ```json
  { "error": "Delete the workspaces you own first, or make someone else an owner.",
    "workspaces": [ { "id": "wsp_2yEHYLwiwPVhLW", "name": "Acme" } ] }
  ```

  At most 100 are listed. The web app loads `GET /api/workspaces?canLeave=false` first, so this is the backstop.
- **Password checks** work as `POST /api/auth/password/verify` does: `LimitAuth` (`auth:per-ip`, a hashing slot) and `auth:per-account`, recording `auth.password_checked` ([auth.md](auth.md#deleting)).

## Status codes

| Code | Meaning |
| --- | --- |
| 400 | Malformed JSON or query parameters |
| 401 | No session, expired session, wrong sign-in credentials (a wrong *current* password while signed in is 422), or a sign-in's second factor whose challenge has ended |
| 403 | Bad Origin or CSRF token, a role without the permission, accepting an invitation sent to another email, a suspended account's right password or code (`suspended: true`), or a request from a blocked address (`Access from your network has been blocked.`, [admin.md](admin.md#ip-blocks)) |
| 404 | Not found, not yours, or a malformed ID |
| 409 | The change would leave a workspace without an owner (for deleting an account, with the `workspaces`) |
| 409 | Email already registered, or blocked, with verification off (generic message); blocking an address an account uses |
| 415 | Mutation without JSON content type (a DELETE needs one only when it has a body) |
| 422 | Body failed validation. Errors about one field add `"field"`, e.g. `{"error": "Current password is incorrect.", "field": "currentPassword"}`. A quota reached ("A workspace can have up to 1,000 projects.") has no `field` |
| 428 | A rate limit's challenge level wants a solved CAPTCHA: `{error, challenge: {provider, siteKey, action}}`; resend the same request with the token in `X-Challenge-Token` (only login, register, a sign-up's new code, a reset request and creating an invitation; [rate-limits.md](rate-limits.md#challenges)) |
| 429 | Rate limited; `Retry-After` is the seconds left in the window ([rate-limits.md](rate-limits.md)) |
| 500 | Something failed on our side: the generic message and `requestId`, never what failed (that's in the log, under the ID) |
| 503 | Database unavailable, sign-in concurrency full, or a limit shared by everyone reached (sign-up, password reset), with `Retry-After`; readiness while shutting down |

## Adding an endpoint

Name the route, its controller, permission, store method and events as in [../naming.md](../naming.md).

1. The controller and its validation in its area, `httpapi/endpoints/<area>/<resources>.go`, one controller per file ([naming](../naming.md#controllers-internalhttpapi)). Its methods are the actions (`workspace.Projects.Index`, `.Show`, `.Store`, `.Update`, `.Destroy`); the query goes in `store` (`h.Repo`), taking the workspace's internal ID (`httpapi.Workspace(r).ID`). What two areas need goes in the core, never imported from another area.
2. Register it in `router/routes.go`: workspace routes go in `workspaceRoutes` (`RequireSession` and `RequireWorkspace`), mutations with `ws.Can(…, auth.<Permission>, …)` ([auth.md](auth.md#protecting-a-new-endpoint)); account-level routes use `RequireSession`.
3. Rate limits: the budget is automatic; a route that creates rows also gets a `<feature>:per-user` limit, and anything that grows without bound a quota ([rate-limits.md](rate-limits.md#limiting-a-new-endpoint)).
4. Integration tests beside the controller (`endpoints/<area>/<resources>_integration_test.go`) for success, validation, 401, a non-member (404), another workspace's data (404 or empty), and a row in the role × route matrix (`workspaceRoutesMatrix`, in `router/permissions_integration_test.go`).
5. Add it to this table.
