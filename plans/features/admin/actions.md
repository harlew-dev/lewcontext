# Admin: actions

Status: **draft 2026-10-31**, decisions to confirm. Plan A6 of the [admin plans](README.md). Needs [access hardening](../implemented/admin/access-hardening.md) (the `operator` role), [events-and-mail](../implemented/admin/events-and-mail.md) (deliveries), [monitoring](monitoring.md) (errors, IPs) and [alerts](alerts.md) (acknowledging). Docs it will change: [admin.md](../../../docs/api/admin.md), [auth.md](../../../docs/api/auth.md), [endpoints.md](../../../docs/api/endpoints.md), [rate-limits.md](../../../docs/api/rate-limits.md), [events.md](../../../docs/api/events.md), [database.md](../../../docs/api/database.md), [deployment.md](../../../docs/deployment.md), [ui/forms-and-actions.md](../../../docs/ui/forms-and-actions.md) (the admin action dialog). Also AGENTS.md: the security invariant "It stays read-only until a plan adds actions" becomes this plan's rules.

**The first action ships earlier, with [email blocks](email-blocks.md)** (A9, if its decisions are confirmed): unblocking an email address, an account-and-access action. That plan builds this one's step-up (`POST /api/admin/step-up`, `admin_sessions.stepped_up_until`, `RequireAdminStepUp`), required reasons, `admin-action:per-user` and `AdminActionDialog` as described here. This plan reuses them, and adds `requireAdminRole(operator)` to the unblock route along with its own.

## Why

The dashboard shows abuse but can't stop it. Today every response is a command run on the server (`admin reset-mfa`) or SQL against production, which is slower, unrecorded by the dashboard, and riskier. Admin access promised actions "with step-up codes in a later plan" (its decision 4). This is that plan: a short list of actions that support and incident response need, each one deliberate, recorded and reversible where it can be.

## The rules for every action

| | Housekeeping | Account and access actions |
| --- | --- | --- |
| Examples | acknowledge an alert, resolve an error group | everything that affects a person, a workspace, an address or a delivery |
| Role | `operator` | `operator` |
| A fresh code (step-up) | no | **yes**, within the last 5 minutes |
| A written reason | no | **yes**, 10–500 characters, stored in the event |
| Recorded | yes | yes |

(decision 1)

- **Step-up** (decision 2): `POST /api/admin/step-up {code}` checks an authenticator or recovery code like the sign-in's second factor (`mfa:per-account`; `auth.mfa.checked` with step `admin_step_up`) and sets `admin_sessions.stepped_up_until` to 5 minutes ahead. Account and access actions answer 403 `{"error": "Enter a code to continue.", "stepUp": true}` outside it, and the dialog asks for the code. A window rather than a code per action, because a TOTP code works once per 30 seconds and incident response often takes several actions in a row.
- **Every action** checks the key, admin session, CSRF, Origin and JSON as other admin writes do, then `requireAdminRole(operator)`, then step-up where needed, then `admin-action:per-user` (60 an hour, in the database).
- **Never on yourself,** apart from your own sessions. **Never on another admin** apart from signing them out (decision 8): suspending or resetting an admin's MFA stays on the CLI, so one compromised admin account can't lock out the others.
- **Each action and its event in one transaction.** The event is an `admin.*` fact with `admin`, the target, and `reason`; the store events the change already records (`auth.mfa.disabled`, `workspace.invitation.revoked`) are still recorded, with the admin as actor (decision 6).

## The actions

### Accounts

| Action | Route | Does | Event |
| --- | --- | --- | --- |
| **Sign out everywhere** | `DELETE /api/admin/users/{user}/sessions` | Deletes every app session (and admin session) of the account | `admin.user.signed_out` (`sessionsRevoked`) |
| **Reset two-factor authentication** | `DELETE /api/admin/users/{user}/mfa` | What `admin reset-mfa` does: MFA off, recovery codes deleted, sessions signed out. The dialog requires ticking "I've confirmed who is asking, outside the app" | `admin.user.mfa_reset`; `auth.mfa.disabled` (`reason: reset`) |
| **Suspend** | `POST /api/admin/users/{user}/suspension` | See below | `admin.user.suspended` (`sessionsRevoked`, `invitationsRevoked`) |
| **Unsuspend** | `DELETE /api/admin/users/{user}/suspension` | Clears the suspension; the person can sign in again | `admin.user.unsuspended` |

### Suspending an account (decision 4)

```sql
-- 002_create_users (edited)
suspended_at timestamptz,
suspended_reason text NOT NULL DEFAULT '' CHECK (length(suspended_reason) <= 500),
```

- **Ends access at once:** every session is deleted in the same transaction, and `FindSession` only finds sessions of accounts not suspended, as a backstop.
- **Sign-in is refused honestly, but only after the password and code:** `403 "This account has been suspended. Contact support."` Someone who doesn't know the password learns nothing; the real owner learns why. `auth.login_attempted` gains `result: "suspended"`.
- **No email from a suspended account's requests:** password reset requests for it are answered as usual, but nothing is sent (the reset wouldn't let them in anyway, and it keeps a suspended spammer from using it as a mail channel).
- **Invitations they sent:** the dialog's "Also revoke invitations they've sent" is ticked by default.
- **Their workspaces keep working** for everyone else. A workspace whose only owner is suspended has no active owner until they're unsuspended; Integrity lists these (a warn check).
- The Users list gets a Suspended badge and a `suspended` filter. The reason is shown to admins only.

### Workspaces

| Action | Route | Does | Event |
| --- | --- | --- | --- |
| **Revoke pending invitations** | `DELETE /api/admin/workspaces/{workspace}/invitations` | Revokes every live invitation in the workspace: stops an invitation-spam run without touching the workspace's data | `admin.workspace.invitations_revoked` (`count`); `workspace.invitation.revoked` for each |

### Deliveries

| Action | Route | Does | Event |
| --- | --- | --- | --- |
| **Retry a delivery** | `POST /api/admin/deliveries/{event}/{listener}/retry` | A failed delivery is queued again: `failed_at` cleared, `attempts` reset, due now. Reset and invitation listeners issue a fresh link when they run, so an old link is never resent | `admin.delivery.retried` |
| **Retry failed deliveries** | `POST /api/admin/deliveries/retry {listener, failedSince}` | The same for every failed delivery of one listener since a time, at most 1,000: for after an SMTP outage | `admin.delivery.retried` (`count`, `listener`, `failedSince`) |

### IP addresses (decision 5)

```sql
-- create_ip_blocks (a new migration)
CREATE TABLE ip_blocks (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 public_id text NOT NULL UNIQUE DEFAULT public_id('ipb') CHECK (public_id ~ '^ipb_[0-9A-Za-z]{14}$'),
 network cidr NOT NULL UNIQUE,
 reason text NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
 created_by bigint REFERENCES admins(user_id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
```

| Action | Route | Event |
| --- | --- | --- |
| **Block an address or range** | `POST /api/admin/ip-blocks {network, expiresIn, reason}` | `admin.ip.blocked` |
| **Unblock** | `DELETE /api/admin/ip-blocks/{block}` | `admin.ip.unblocked` |
| List blocks | `GET /api/admin/ip-blocks` (recorded as `admin.ip_block.listed`) | |

- **Every block expires**, after at most 30 days. Addresses are reassigned and shared (offices, mobile carriers), so a permanent block eventually hits the wrong people. The cleanup loop deletes expired rows.
- **Bounded width:** at most a /16 (IPv4) or /32 (IPv6). **Refused:** a range containing the acting admin's own address, or any of `TRUSTED_PROXIES` (that would block everyone behind the proxy).
- **Enforced in `clientAddress`** from an in-memory list each server reloads every 30 seconds (one small query), so the check costs a prefix lookup. Blocked requests get `403 {"error": "Access from your network has been blocked."}` (a plain 403 page for pages), and count in `ip_block_refused_total`.
- **Not blocked:** `/api/health*` and the admin door (`/admin/…`, `/api/admin/…`), so a mistaken block can't lock admins out of undoing it. The admin door has its own allowlist ([access hardening](../implemented/admin/access-hardening.md)).
- **The edge is better for floods:** a blocked request still reaches the server. [deployment.md](../../../docs/deployment.md) will say to use a Cloudflare or proxy rule for volume; this is for a few abusive addresses.

### Email addresses

| Action | Route | Event |
| --- | --- | --- |
| **Unblock an address** | `DELETE /api/admin/email-blocks {email, reason}` | `admin.email.unblocked`; `email.unblocked` (`by: "admin"`) |

Built by [email blocks](email-blocks.md) (A9), on the Email blocks page. There is no admin block: blocking comes only from the inbox owner's link.

### Admins

| Action | Route | Event |
| --- | --- | --- |
| **Sign another admin out** | `DELETE /api/admin/admins/{user}/sessions` | `admin.admin.signed_out` |

For a lost laptop or a session flagged by `admin.session.moved`. Revoking admin itself stays on the CLI.

### Housekeeping

| Action | Route | Event |
| --- | --- | --- |
| **Acknowledge an alert** (stops its repeat emails) | `PATCH /api/admin/alerts/{alert} {acknowledged: true}` | `admin.alert.acknowledged` |
| **Resolve an error group** (a new occurrence reopens it) | `PATCH /api/admin/errors/{error} {resolved: true}` | `admin.error.resolved` |
| **Silence a signal** for up to 24 hours (planned maintenance, a known incident) | `PATCH /api/admin/alerts/{alert} {silencedUntil}` | `admin.alert.silenced` |

Silencing hides alerts that might be security alerts, so it's an account-and-access action: step-up and a reason, despite living here.

### On the command line

| Command | Does |
| --- | --- |
| `admin sessions revoke-all -confirm` | **Signs every account out**, app and admin sessions alike, recording `auth.sessions_revoked_all` (`count`). For a suspected session-table leak; part of the [incident runbook](compliance.md) |
| `admin suspend -email … -reason … -confirm`, `admin unsuspend …` | Suspending from the CLI, including admins (after `revoke`) |

## Web app

- **User page:** an **Actions** danger zone (operators only) with a `DangerAction` row each for Sign out everywhere, Reset two-factor authentication, and Suspend account (or Unsuspend, as an `outline` button since it isn't destructive). A suspended account shows a Notice with the reason, who suspended it, and when.
- **Workspace page:** Revoke pending invitations, in its own danger zone.
- **IP page:** Block this address (and "Block the /64" for IPv6). Monitoring › IP addresses gets a Blocked tab: network, reason, by, expires, a ⋯ menu with Unblock.
- **Mail:** a ⋯ menu per failed row with Retry delivery; Retry failed deliveries in the page header.
- **Alerts and Errors:** Acknowledge and Mark resolved as `outline` buttons in the detail page header; Silence in its ⋯ menu.
- **`AdminActionDialog`** (`components/app/admin/admin-action-dialog.tsx`): a `ConfirmationDialog` with a required Reason textarea and, when the API answers `stepUp`, a code field that steps up and retries. Danger variant for destructive actions. Pending, error and success states as every dialog ([forms-and-actions.md](../../../docs/ui/forms-and-actions.md)).
- **Viewers** don't see action controls; `/api/admin/me`'s `role` decides.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A stolen admin session used to act | Operator role, a fresh code within 5 minutes, CSRF and a Strict cookie, 60 actions an hour, everything recorded with a reason |
| A compromised admin locking out the others | No action on another admin except signing them out; suspension and MFA resets of admins only via CLI |
| Locking yourself out | No actions on yourself; IP blocks can't cover your own address and never apply to the admin door |
| Blocking everyone | IP blocks can't include `TRUSTED_PROXIES`, have a width limit and expire |
| Social engineering an MFA reset | The out-of-band confirmation tick, a required reason, and the event naming who did it |
| Re-sending a stale link | Retried deliveries make fresh links |
| Actions without accountability | One `admin.*` event per action with the admin, target and reason, in the same transaction; the reason is required |
| Impersonation used to snoop or act as a user | Not built, by decision |

## Tests

- **Every action:** a viewer gets 403; an operator without step-up gets 403 with `stepUp`; with step-up and a reason it works and records exactly its events in the same transaction; without a reason 422; on yourself or another admin (where refused) 403; `admin-action:per-user` gives 429.
- **Step-up:** a wrong code 422 and counted against `mfa:per-account`; the window expires after 5 minutes; a recovery code works and is used up.
- **Suspension:** sessions deleted; a session created before is refused; correct password and code give 403 with the message, a wrong password the usual 401; no reset email; invitations revoked when asked; unsuspend restores sign-in.
- **IP blocks:** an address inside a block gets 403 on API and pages within 30 seconds on every server; health and admin routes aren't blocked; width, own-address and trusted-proxy refusals; expiry.
- **Deliveries:** a retried reset delivery issues a new link; the bulk retry caps at 1,000.
- **CLI:** `sessions revoke-all` previews without `-confirm`, then deletes every session and records one event.
- **Vitest:** the dialog's reason and step-up flow; controls hidden from viewers.
- **Playwright:** an operator suspends a seeded user, that user's open session is signed out and their sign-in shows the message; the operator unsuspends them.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Tiers | Housekeeping (operator role only) and account-and-access actions (operator, step-up and a reason) |
| 2 | Step-up | A 5-minute window opened by `POST /api/admin/step-up` with a code |
| 3 | Reasons | Required for account-and-access actions, 10–500 characters, stored in the event payload |
| 4 | Suspension | Sessions ended; an honest 403 only after password and code; no reset email; their invitations revoked by default; their workspaces untouched |
| 5 | IP blocks | Expire within 30 days; at most /16 or /32; never the admin door, health, your own address or trusted proxies; enforced from a list reloaded every 30 s |
| 6 | Events | One `admin.*` event per action with the reason; the store's own events still recorded with the admin as actor |
| 7 | Telling the affected person | No email in v1; a suspended account sees the message at sign-in. Security mail ("your two-factor authentication was reset") can follow once the app sends security mail at all |
| 8 | Acting on other admins | Only signing them out; everything else via the CLI |
| 9 | Signing everyone out | CLI only (`admin sessions revoke-all`) |
| 10 | Impersonation | Not built. If support ever needs it, a read-only "view as" in its own plan |

## Not included

- **Impersonation** (decision 10).
- **Editing** anyone's name, email or workspace roles; **transferring ownership**; **deleting** accounts or workspaces from the dashboard. Account deletion stays self-service; an erasure request handled by staff is in [compliance](compliance.md).
- **Four-eyes approval** (a second admin confirming an action). Worth adding for destructive actions if the team grows.
- **Bulk suspension**, and suspending workspaces.
- **Granting or revoking admin** in the dashboard: CLI only, permanently.
