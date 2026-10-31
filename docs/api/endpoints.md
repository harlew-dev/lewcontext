# Endpoints

All routes are registered in `apps/api/internal/httpapi/routes.go`. The browser reaches them through the Next proxy at `/api/*`.

| Method | Route | Requires | Success |
| --- | --- | --- | --- |
| GET | `/api/health` | Public | `{"status":"ok"}`, or 503 if the database is down |
| POST | `/api/auth/register` | Origin, JSON, rate limits | 201 `{user, csrfToken, expiresAt, signedInAt}` and a new session cookie |
| POST | `/api/auth/login` | Origin, JSON, rate limit | 200 `{user, csrfToken, expiresAt, signedInAt}` and a rotated session cookie |
| GET | `/api/auth/me` | Session | `{user, csrfToken, expiresAt, signedInAt}` |
| PATCH | `/api/auth/me` | Session, CSRF, Origin | 200 with the updated user. Body `{name}` (trimmed, 1–80 characters, no control characters); the email address can't change, so other fields are rejected (400) |
| DELETE | `/api/auth/me` | Session, CSRF, Origin, rate limit | 204; the account, its sessions, memberships and the events about it are deleted, and the cookie is cleared. Body `{password}`. 422 with `field: "password"`; 409 for the only owner of a workspace, listing them ([below](#deleting)) |
| POST | `/api/auth/logout` | Session, CSRF, Origin | 204; session revoked, cookie cleared |
| DELETE | `/api/auth/sessions` | Session, CSRF, Origin | 204; every other session revoked |
| POST | `/api/auth/password/verify` | Session, CSRF, Origin, rate limit | 204 if `{password}` is the current password; 422 with `field: "password"` if not |
| PATCH | `/api/auth/password` | Session, CSRF, Origin, rate limit | 200 `{user, csrfToken, expiresAt, signedInAt}` and a new session cookie; every earlier session revoked. 422 with `field` (`currentPassword` or `newPassword`) when refused |
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
| POST | `/api/workspaces/{workspace}/invitations` | `workspace.invitation.store`, CSRF, Origin, `invitation:*` limits | 201 with the invitation; the email goes out a moment later. Body `{email, role}`. 403 when the role isn't in your `assignableRoles`; 422 with `field` (`email`: malformed, already a member or already invited; `role`: owner or unknown); 422 for the 51st live invitation or the member quota; 503 with `MAIL_DRIVER=off` or when `invitation:global` is reached |
| DELETE | `/api/workspaces/{workspace}/invitations/{invitation}` | `workspace.invitation.destroy`, CSRF, Origin | 204; its link stops working. 403 for an invitation to a role you can't give, 404 |
| POST | `/api/invitations/check` | Origin, JSON, `auth:per-ip` | 200 `{workspace: {name}, invitedBy: {name, email}, email, role, expiresAt}` for a live `{token}`; 422 with `field: "token"` otherwise. Works signed out |
| POST | `/api/invitations/accept` | Session, CSRF, Origin | 200 with the workspace (your `role`, `permissions`). Body `{token}`. 403 when you're signed in as another email; 422 with `field: "token"` for a dead link, or without it when the workspace is full |
| GET | `/api/workspaces/{workspace}/projects` | Member | One page of the workspace's projects ([pagination.md](pagination.md)) |
| POST | `/api/workspaces/{workspace}/projects` | `project.store`, CSRF, Origin, `project:per-user` | 201 with the created project; 403 for viewers; 422 when the workspace has 1,000 |
| GET | `/api/workspaces/{workspace}/projects/{project}` | Member | The project, or 404 |
| PATCH | `/api/workspaces/{workspace}/projects/{project}` | `project.update`, CSRF, Origin | 200 with the updated project, or 404; 403 for viewers |
| DELETE | `/api/workspaces/{workspace}/projects/{project}` | `project.destroy`, CSRF, Origin | 204, or 404; 403 for members and viewers |

Every route that needs a session also counts against the user's budget (300 writes per 15 minutes, 1,000 reads per 5 minutes), except logout, `DELETE /api/auth/sessions` and the two password routes ([rate-limits.md](rate-limits.md)). "Origin" means the exact `APP_ORIGIN`, no cross-site fetch metadata, and `Content-Type: application/json` on POST, PATCH and any DELETE with a body. "Member" means a session plus a `workspace_user` row for that workspace; a permission such as `project.store` means a member whose role grants it ([auth.md](auth.md#workspaces-and-roles)).

## IDs

Every record in a response is identified by its **public ID**: a prefix and 14 base62 characters (`usr_…`, `wsp_…`, `prj_…`), generated by PostgreSQL ([database.md](database.md#public-ids)). Internal numeric IDs never appear in requests or responses. A path ID with the wrong prefix or shape is a 404 without a query.

## Workspaces

```json
{ "id": "wsp_2yEHYLwiwPVhLW", "name": "Acme", "role": "admin",
  "permissions": ["project.destroy", "project.store", "project.update", "workspace.invitation.destroy", "…"],
  "assignableRoles": ["member", "viewer"], "canLeave": true,
  "createdAt": "2026-10-30T12:00:00Z", "updatedAt": "2026-10-30T12:00:00Z" }
```

- **Body for POST/PATCH:** `name` (trimmed, 1–80 characters, no control characters). Unknown fields, such as `id` or `role`, are rejected.
- **Membership:** the `{workspace}` routes go through `requireWorkspace`, which looks the workspace up through your membership. Another user's workspace, a missing one and a malformed ID all get the same 404.
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

## Deleting

Deletion is permanent and immediate: one transaction, with `ON DELETE CASCADE` removing what belongs to the row. Plan: [plans/features/implemented/deletion.md](../../plans/features/implemented/deletion.md).

| Deleted | Removed with it | Kept |
| --- | --- | --- |
| project | nothing else | its events (payloads hold `prj_…` only) |
| workspace | memberships, projects | its events, with `workspace_id` set to null |
| account | sessions, memberships, reset link, the events about it (`events.user_id`) | events it acted in for others, with `actor_id` set to null |

- **Workspace:** the typed `name` must be the current name, checked again under the workspace lock, so a tab opened before a rename can't delete it under the old name. Then the owner's password.
- **Account:** the password, then every workspace you own is locked in `id` order and checked with the same rule as `canLeave`. The only owner of any of them gets:

  ```json
  { "error": "Delete the workspaces you own first, or make someone else an owner.",
    "workspaces": [ { "id": "wsp_2yEHYLwiwPVhLW", "name": "Acme" } ] }
  ```

  At most 100 are listed. The web app loads `GET /api/workspaces?canLeave=false` first, so this is the backstop.
- **Password checks** work as `POST /api/auth/password/verify` does: `limitAuth` (`auth:per-ip`, a hashing slot) and `auth:per-account`, recording `auth.password_checked` ([auth.md](auth.md#deleting)).

## Status codes

| Code | Meaning |
| --- | --- |
| 400 | Malformed JSON or query parameters |
| 401 | No session, expired session, or wrong sign-in credentials (a wrong *current* password while signed in is 422) |
| 403 | Bad Origin or CSRF token, a role without the permission, or accepting an invitation sent to another email |
| 404 | Not found, not yours, or a malformed ID |
| 409 | The change would leave a workspace without an owner (for deleting an account, with the `workspaces`) |
| 409 | Email already registered (generic message) |
| 415 | Mutation without JSON content type (a DELETE needs one only when it has a body) |
| 422 | Body failed validation. Errors about one field add `"field"`, e.g. `{"error": "Current password is incorrect.", "field": "currentPassword"}`. A quota reached ("A workspace can have up to 1,000 projects.") has no `field` |
| 429 | Rate limited; `Retry-After` is the seconds left in the window ([rate-limits.md](rate-limits.md)) |
| 503 | Database unavailable, sign-in concurrency full, or a limit shared by everyone reached (sign-up, password reset), with `Retry-After` |

## Adding an endpoint

Name the route, its handler, permission, store method and events as in [../naming.md](../naming.md).

1. Handler and validation in `httpapi` (one file per resource). Handlers are methods on the resource's handler type, named after the action (`projectHandlers.index`, `.show`, `.store`, `.update`, `.destroy`); the query goes in `store`, taking the workspace's internal ID (`currentWorkspace(r).ID`).
2. Register it in `routes.go`: workspace routes go in `workspaceRoutes` (`requireSession` and `requireWorkspace`), mutations with `ws.Can(…, auth.<Permission>, …)` ([auth.md](auth.md#protecting-a-new-endpoint)); account-level routes use `requireSession`.
3. Rate limits: the budget is automatic; a route that creates rows also gets a `<feature>:per-user` limit, and anything that grows without bound a quota ([rate-limits.md](rate-limits.md#limiting-a-new-endpoint)).
4. Integration tests for success, validation, 401, a non-member (404), another workspace's data (404 or empty), and a row in the role × route matrix (`workspaceRoutesMatrix`).
5. Add it to this table.
