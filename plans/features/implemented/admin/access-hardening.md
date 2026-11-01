# Admin: IP allowlist, key rotation, roles and the Admins page

Status: **implemented 2026-11-01** (decisions confirmed as written; see [How the build differs](#how-the-build-differs-from-the-plan)). Plan A2 of the [admin plans](../../admin/README.md). Builds on [admin access](../admin-access.md) and [client IP](../client-ip.md). Docs updated: [admin.md](../../../../docs/api/admin.md) (the previous key, the allowlist, roles, admin sessions, the Admins page), [auth.md](../../../../docs/api/auth.md), [api/README.md](../../../../docs/api/README.md), [deployment.md](../../../../docs/deployment.md) (the variables, rotating the key, the allowlist and Cloudflare Access or a VPN, the checklist), [database.md](../../../../docs/api/database.md), [events.md](../../../../docs/api/events.md), [endpoints.md](../../../../docs/api/endpoints.md), [seeding.md](../../../../docs/api/seeding.md), [api/testing.md](../../../../docs/api/testing.md), [naming.md](../../../../docs/naming.md), [ui/README.md](../../../../docs/ui/README.md), [ui/pages.md](../../../../docs/ui/pages.md), [ui/api.md](../../../../docs/ui/api.md), [ui/design.md](../../../../docs/ui/design.md), [ui/testing.md](../../../../docs/ui/testing.md), [testing.md](../../../../docs/testing.md), [docs/README.md](../../../../docs/README.md), `.env.example`, the `Makefile` (`role=`) and AGENTS.md (the admin invariant, the docs table).

## How the build differs from the plan

- **The allowlist covers `GET /admin-assets/…` too,** checked once the assets pass is valid: from outside the list a valid pass opens nothing (the same 404 as no pass), so a stolen pass works only from where admins work. A valid pass from outside is recorded as `admin.access_refused` like the key; a request without a valid pass is never checked, so it can't write events.
- **Refusals are remembered per address, an IPv6 address by its /64** (as per-IP limits count, `ratelimit.Subject`, now exported), at most once per 15 minutes per server. **The set is capped at 10,000 addresses per server:** when full it drops ended ones (at most once a minute) and otherwise records nothing more rather than grow (`onceEvery`, `httpapi/admin_access.go`, unit-tested). The request only queues the refusal: **a bounded queue of 64 and one worker** per server log it and write the event (after the review; a goroutine per refusal could hold up to 10,000 database connections at once). Past 64 queued, refusals are dropped and counted in a log line at most once a minute; graceful shutdown drains the queue (`StopAdminRefusals`, after the HTTP server stops). The log line names the address, never the path, which holds the key on page requests.
- **The previous key is a wrong key to anyone without a live admin session.** The plan named the sign-in page and login; the build answers every request under the previous key that has no admin cookie (with no query), or a cookie that isn't a live session made under one of the keys, exactly as a wrong key. With a cookie of any shape it costs a database round trip a wrong key doesn't, so timing can tell it from a wrong key; it still opens nothing. **It fails closed** (after the security review): if the session can't be checked (the database down or slow), its pages and API are a wrong key's 404 and no pass is issued, where the current key keeps the outage rule (its inner page is served with a pass, which its sign-in page gives anyone with the key anyway, so the page can show the outage). A session made under the current key, sent with the previous key, is 401 (API), as the plan's table says. Inner pages under the previous key without a matching session are the 404, not a redirect: the sign-in page they'd redirect to doesn't exist under that key. Signing out works under it (204); the sign-in page it then returns to is the 404.
- **Two key middlewares:** `RequireAdminKey` takes the current key only (the sign-in steps and the catch-all); `RequireAdminKeyOrPrevious` (the routes behind a session) takes either and tells `RequireAdminSession` which matched. Both keys are compared every time (`auth.MatchAdminKey`), in constant time with no early exit. The session is looked up (and its idle timer refreshed) **only under the digest of the key the request matched**, so a session sent with the other key is never touched (after the review: an `IN (current, previous)` lookup had refreshed it before rejecting it); a read-only `CheckAdminSession` tells "made under the current key" (401) from "none" (404) under the previous key, and `auth.MatchAdminKeyHash` checks the found session's digest again.
- **`admin_sessions.moved_at`**, a column the plan didn't list, makes `admin.session.moved` once per session across servers (set in the event's transaction, `FlagAdminSessionMoved`). `from` and `to` are CIDR strings (`198.51.100.0/24`), `sessionAge` is in seconds.
- **`grant` without `-role` keeps an existing admin's role** (a new admin is a viewer), so granting again to reopen the setup window never demotes an operator. A new role on an existing admin only changes the role and records `admin.updated`, not `admin.granted`, and leaves the setup window alone. `make admin cmd=grant … role=operator` passes it.
- **Roles in the registry:** mutations in the signed-in admin group are registered with `Role(method, path, role, handler)` (`httpapi.RequireAdminRole`, 403) or `OwnSessions(…)`; the group **panics at startup** on any other mutation (`TestAdminMutationsNeedARole`), beyond the plan's source check. `OwnSessions` takes only `POST /logout` and `DELETE /sessions` (`ownSessionRoutes`) and panics otherwise, and the source check fails on any registration it can't read and on the unchecked `handle` (after the review). `TestAdminRoutesStayInTheAdminGroup` lists the exceptions with their reasons: the three sign-in steps (no session yet), `POST /logout` and `DELETE /sessions` (decision 5: they only take your own access away). No real route needs `operator` yet; a test-only route proves the 403 until the email-blocks plan's Unblock uses it.
- **`GET /api/admin/admins`** answers `{data: [...]}`, at most 500 admins, each with `user`, `role`, `mfaEnabled`, `mfaSetupUntil`, `grantedAt`, `lastSignedInAt` and `sessions` (`createdAt`, `lastSeenAt`, `ip`, `userAgent`, `current`, `previousKey`; only live sessions under the current or previous key). Any query parameter is 400. `lastSignedInAt` reads a **new partial index** in the events create migration (007), `events_admin_logged_in_idx ON events(actor_id, created_at DESC) WHERE name = 'admin.logged_in'`: the actor index alone would walk every read the admin made. The store method is `IndexAdminAdmins` (`store/admin_admins.go`), the controller `admin.Admins`.
- **Admin sign-in needs an IP client address,** which the session and the sign-in's events store (`inet`). Through a unix socket there is none: both sign-in steps answer 503 and log why, before anything is checked or used up.
- **`grant`'s preview and output** say where the admin stands with MFA: a role change opens no setup window, so for an admin without MFA whose window is closed it says to run `grant` again without `-role`.
- **`role`** is in the door's sign-in answer (`POST /api/admin/login/mfa`) as well as `/api/admin/me`, so the two stay alike. Each admin session also stores the sign-in's `User-Agent` as the events table does (valid UTF-8, at most 256 characters).
- **The 90-day warning** counts an admin who never signed in from their grant, so a new admin isn't flagged and one granted long ago who never used it is. Events are pruned after 90 days by default, so an older sign-in reads as none, which is the same answer.
- **The banner** comes from `AdminProvider`, which starts from the layout loader's `/api/admin/me` (its `X-Admin-Key-Status`) and turns on when any later admin answer carries the header (the transport's new `onResponse`), so a rotation mid-visit shows too. It says the address "is being retired", and the Admins page's session badge reads "Retiring address" rather than "previous key", the same words.
- **Viewers see no action buttons:** none exist yet. `role` is in `/api/admin/me` and the session in `AdminProvider`, with `adminCan(role, need)` for the email-blocks plan to hide Unblock; the Vitest checks `adminCan` instead of hidden buttons.
- **The CSV** quotes every cell and starts any a spreadsheet would run as a formula (`=`, `+`, `-`, `@`) with an apostrophe. "Sign out my other sessions" reloads the list afterwards, which is one more recorded read.
- **Left for later plans, with a note where they hook in:** the Configuration page's warnings (monitoring, A4: an allowlist without `CLIENT_IP_HEADER`, sessions still on the previous key) and the alert on a refusal (alerts, A5, in `refuseAdminAddress`). The production startup warning is built.
- **Naming:** `admin.session.moved` has three segments, where [naming.md](../../../../docs/naming.md) said facts about admins and their sessions are one; naming.md now says a fact about one stored admin session names it, as the `auth` area's sub-resources do.

## Why

Admin access left four gaps that matter once the dashboard can see abuse data and, later, act on it:

1. **Anyone on the internet can reach the door** once they have the key. An allowlist of office, VPN or bastion addresses makes a leaked key useless from anywhere else.
2. **Rotating the key signs every admin out at once.** A routine rotation (every 90 days, or when someone leaves) shouldn't interrupt people mid-investigation.
3. **Every admin can do everything.** Once [actions](../../admin/actions.md) exist, most people who need to *look* (support, auditors) shouldn't be able to *act*. Least privilege is also what SOC 2 and ISO 27001 reviewers ask for first.
4. **Nobody can see who holds admin** without the CLI. An access review (who has it, when they last used it, with MFA) should be one page.

## IP allowlist: `ADMIN_ALLOWED_IPS`

| Variable | Value | Notes |
| --- | --- | --- |
| `ADMIN_ALLOWED_IPS` | comma-separated addresses or CIDR ranges, e.g. `203.0.113.10,198.51.100.0/24,2001:db8:1::/48` | **Unset: any address** (today's behaviour). Parsed like `TRUSTED_PROXIES`; `0.0.0.0/0` and `::/0` are refused, as are ranges that don't parse |

- **Checked after the key matches,** in `requireAdminKey` and the admin page gate, against `clientIP(r)`.
- **A right key from an address not on the list gets the same 404** as a wrong key (decision 1), for pages and API. Outside the list, nobody can learn the key is right.
- **But the operator learns it:** a `slog.Warn` and an `admin.access_refused` event (`reason: "ip_not_allowed"`, the IP in `events.ip`), **at most once per IP per 15 minutes** per server (an in-memory set), so a scanner holding the key can't fill the table. A refusal means the key has leaked: [alerts](../../admin/alerts.md) raises one on the first.
- **Behind a proxy it depends on the real client IP.** Without `CLIENT_IP_HEADER` and `TRUSTED_PROXIES`, every request comes from the proxy, and an allowlist naming the proxy lets everyone in. Production startup **warns** when `ADMIN_ALLOWED_IPS` is set without `CLIENT_IP_HEADER`; it can't refuse, since a server facing the internet directly is valid. The Configuration page ([monitoring](../../admin/monitoring.md)) shows the same warning.
- **Stronger, outside the app:** Cloudflare Access, a VPN or Tailscale in front of `/admin/` authenticate people before they reach the server. [deployment.md](../../../../docs/deployment.md) will recommend one where available; the allowlist is the layer that works everywhere.

## Rotating without signing out: `ADMIN_ROUTE_KEY_PREVIOUS`

| Variable | Value | Notes |
| --- | --- | --- |
| `ADMIN_ROUTE_KEY_PREVIOUS` | the key being retired, same format as `ADMIN_ROUTE_KEY` | Optional; must differ from the current key; refused without a current key |

**The previous key keeps live sessions working; it doesn't open the door** (decision 2):

| Request | Under the current key | Under the previous key |
| --- | --- | --- |
| `GET /admin/<key>` (sign-in page) | 200 | 404, as a wrong key |
| `POST /api/admin/login…` | as today | 404 |
| `/admin/<key>/<page>`, other `/api/admin/*` with a session made under that key | 200 | 200, with `X-Admin-Key-Status: previous` |
| A session made under the other key | 401 | 401 |

- `admin_sessions.key_hash` already records which key a session was made under; a session is valid while its `key_hash` is the current or the previous key's digest.
- **Both digests are always compared** in constant time, with no early exit, so timing doesn't show which one matched.
- **The web app shows a banner** on `X-Admin-Key-Status: previous`: "This admin address is being retired. Ask your operator for the new one before this session ends." The header goes only to someone who already holds that key and a session made with it.
- **Rotation, step by step:** generate a new key; set it as `ADMIN_ROUTE_KEY` and the old one as `ADMIN_ROUTE_KEY_PREVIOUS`; restart; hand out the new URL; after 8 hours at most (the session maximum) remove the previous key and restart again.
- **If a key leaked, don't set it as previous.** Replace it outright, as today, and every session under it ends.
- The startup log says the previous key is set (never its value), and the Configuration page shows how many sessions still use it.

## Admin roles (decision 3)

```sql
-- 014_create_admins (edited)
role text NOT NULL DEFAULT 'viewer' CHECK (role IN ('viewer', 'operator')),
```

| Role | Can |
| --- | --- |
| `viewer` | Every admin page and read |
| `operator` | That, plus the [actions](../../admin/actions.md), each still behind a fresh code and a reason |

- **Granted from the CLI only,** as admin itself: `admin grant -email … -role operator -confirm` (default `viewer`). Running `grant` on an existing admin with another role changes it, recording `admin.updated` (`user`, `changes.role`). There is no API route for it.
- **`requireAdminRole(operator, …)`** wraps each action route in `adminRoutes`, as `ws.Can` names a permission. `TestAdminRoutesStayInTheAdminGroup` grows a check that every non-GET admin route other than sign-in and sign-out names a role.
- `/api/admin/me` returns `role`, so the web app hides action buttons from viewers; the API refuses them regardless (403).

## Admin sessions remember where they started (decision 4)

```sql
-- 015_create_admin_sessions (edited)
ip inet NOT NULL,             -- clientIP at sign-in
user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 256),
```

Sessions aren't bound to an address (mobile networks change it), but `requireAdminSession` notices when a request comes from a different network than the sign-in (another IPv4 /24 or IPv6 /48) and records `admin.session.moved` (`from`, `to` network, the session's age), once per session. A stolen admin cookie used from elsewhere shows up in the trail and as an alert.

## The Admins page

| URL | File | Export |
| --- | --- | --- |
| `/admin/:key/admins` | `pages/admin/[key]/admins.tsx` | `AdminAdminIndex` |

`GET /api/admin/admins` (recorded as `admin.admin.listed`): every admin with `user` (`{id, name, email}`), `role`, `mfaEnabled`, `mfaSetupUntil`, `grantedAt`, `lastSignedInAt` (newest `admin.logged_in`), and their live admin sessions (`createdAt`, `lastSeenAt`, `ip`, `userAgent`, `current`, `previousKey`).

- **Access review:** a "Download CSV" button builds the file in the browser from the same response (email, role, MFA, granted, last sign-in), for the quarterly review an auditor asks to see. It records nothing extra; the read already did.
- **Sign out my other admin sessions:** `DELETE /api/admin/sessions` (CSRF, no code: it only removes access), recording `admin.sessions_revoked` (`count`). Signing out *another* admin is an [action](../../admin/actions.md).
- Badges for role, MFA and "setup window open"; a warning on admins who haven't signed in for 90 days ("consider revoking").

## Events

| Event | Payload | Scope |
| --- | --- | --- |
| `admin.access_refused` | `reason`: `ip_not_allowed` | none; `ip` set |
| `admin.updated` | `user`, `changes.role` (`from`, `to`) | `user_id`, no actor (the CLI) |
| `admin.granted` | gains `role` | as today |
| `admin.session.moved` | `admin`, `from`, `to` (networks, not full addresses), `sessionAge` | actor |
| `admin.sessions_revoked` | `admin`, `count` | actor |
| `admin.admin.listed` | `admin` | actor |

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A leaked key used from elsewhere | The allowlist answers 404 and the operator gets an event and an alert |
| The allowlist silently does nothing behind a proxy | A startup warning and a Configuration page warning when `CLIENT_IP_HEADER` isn't set; docs |
| A refused address floods the events table | One event per IP per 15 minutes per server |
| The previous key used to sign in after a leak | It never opens the sign-in; only live sessions continue, and a leaked key is replaced outright |
| Timing showing which key matched | Both digests compared every time |
| A viewer performing an action | `requireAdminRole` on the API (403), a route-registry test, buttons hidden in the UI |
| Role escalation through the dashboard | Roles change only from the CLI, recorded as `admin.updated` |
| A stolen admin cookie | Strict cookie, 15 minute idle, now also `admin.session.moved` and an alert |

## Tests

**Go unit:** allowlist parsing (refusals as `TRUSTED_PROXIES`); matching IPv4, IPv6 and ranges; previous key format, equal-to-current refused, refused without a current key.

**Go integration:**
- With an allowlist, a right key from outside gets responses byte-identical to a wrong key's (pages and API); one `admin.access_refused` per IP per window; from inside, as today.
- Previous key: its sign-in page and login are 404; a session made under it works with `X-Admin-Key-Status: previous`; removing it ends that session; a session under the current key sent with the previous key is 401.
- Roles: a viewer gets 403 on an action route (once A6 exists, a test route meanwhile); `grant -role` changes the role and records `admin.updated`.
- A request from another /24 records `admin.session.moved` once.
- The Admins list; `DELETE /api/admin/sessions` keeps the current session.

**Vitest:** the previous-key banner; actions hidden for viewers; the CSV's columns.

## Decisions (confirmed 2026-11-01, as written)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | A right key from an address outside the allowlist | The same 404 as a wrong key, plus a warning, an event (once per IP per 15 minutes) and an alert |
| 2 | What the previous key allows | Live sessions made under it only; never a new sign-in. Two slots (current and previous) rather than a list |
| 3 | Admin roles | Two: `viewer` (default) and `operator`, set from the CLI. No finer permissions until actions need them |
| 4 | Admin sessions and addresses | Store the sign-in IP and user agent; don't bind the session to them; record `admin.session.moved` once when the network changes |
| 5 | Signing out your own other admin sessions | Allowed without a code (it only removes access) |
| 6 | Stale admins | A warning on the Admins page after 90 days without an admin sign-in; no automatic revocation |

## Not included

- **A key per admin** (each admin's own URL, so a leaked one names who leaked it). It moves the key from config to the database, so a wrong key costs a lookup; worth revisiting if the team grows.
- **A separate admin origin or subdomain.**
- **Passkeys for admins** (WebAuthn): phishing-resistant, and the natural next step after TOTP.
- **Configuring Cloudflare Access or a VPN**: documented, not built.
- **Automatic revocation** of stale admins.
