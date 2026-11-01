# Compliance: data, retention, erasure, export and audit evidence

Code: `internal/events/retention.go` (`events.Audit`, the audit classes), `internal/store/prune_events.go` (the `events` and `audit_events` targets), `pseudonymise.go` (minimising old events), `erasure.go` (`DeleteAccount`'s erasure, `EraseAccount`), `account_export.go` (`FindAccountExport`), `events.go` (`insertAudited`, the audit lines), `internal/auth/encrypt.go` (`EmailPseudonym`), `internal/prune` (the targets and `prune.completed`), `internal/httpapi/endpoints/account/data_export.go` and `endpoints/admin/event_export.go`, `cmd/admin/compliance.go` (`admin erase`, `admin export`). Web: Account & security's **Your data** (`pages/app/settings.tsx`) and the Events page's **Download CSV** (`pages/admin/[key]/events.tsx`). Plan: [compliance](../plans/features/implemented/admin/compliance.md) (A7). The incident runbook is [incident-response.md](incident-response.md).

No software makes an organisation pass GDPR, SOC 2 or ISO 27001: those cover contracts, policies, training, risk assessments and an auditor's judgement as much as code. This page lists the **technical controls** the app has and the **evidence** they leave, so the organisation's own records can build on them, and says plainly what isn't there ([Known gaps](#known-gaps)). Health data (HIPAA) and card data (PCI DSS) are out of scope: the app holds neither.

## Data map

Every table, what personal data it holds, why, for how long and who can see it. `TestEveryTableIsInTheDataMap` (`internal/database`) fails when a migration adds a table this list doesn't name, or this list names one that's gone. "Admins" means the admin dashboard, behind its key, an admin session and MFA, every read recorded ([api/admin.md](api/admin.md)). The lawful basis is the organisation's to confirm; the column gives the one the app was built around.

| Table | Personal data | Why (basis) | Kept | Who sees it |
| --- | --- | --- | --- | --- |
| `users` | name, email, Argon2id password hash, when created, email verified and last seen (to the hour) | the account (contract) | until the account is deleted | the person; members of their workspaces (name, email); admins (never the hash) |
| `sessions` | the session's token digest and CSRF token, when created, last used, ends | staying signed in (contract) | 30 minutes idle, 24 hours at most; the cleanup deletes ended ones | nobody: the person's own browser holds the token |
| `workspaces` | its name | the service (contract) | until its owner deletes it | its members; admins |
| `workspace_user` | who belongs to which workspace, with what role, since when | the service (contract) | until they leave, are removed, or the workspace or account goes | the workspace's owners and admins; admins |
| `projects` | names, descriptions (free text people type) | the service (contract) | until deleted, or their workspace is | the workspace's members; admins |
| `workspace_invitations` | the invited address (typed by the inviter), role, who invited, the link's digest | inviting someone (the inviter's legitimate interest) | until accepted, revoked, or 30 days after it expires (7 days) | the workspace's owners and admins; admins |
| `events` | the audit trail: who did what, the client IP, user agent and request ID; emails typed at sign-in, sign-up, reset and invitation; admins' searches and reasons ([api/events.md](api/events.md)) | security and accountability (legitimate interest; the audit trail an auditor asks for) | audit events 1 year, others 90 days; IP cut and emails pseudonymised at 30 days ([Retention](#retention)) | admins only |
| `event_deliveries` | which listener ran for which event, a scrubbed error | sending the emails events cause | with their event | admins (the Mail page) |
| `rate_limits` | counters keyed by a SHA-256 of an IP, email or account ID, never the value | abuse prevention (legitimate interest) | until the window ends; the cleanup deletes them | nobody |
| `password_resets` | a reset link's digest for an account | resetting a password (contract) | 30 minutes; the cleanup deletes it | nobody |
| `mfa_totp` | the authenticator secret, sealed with `APP_KEY` | two-factor authentication (contract) | while MFA is on | nobody (the server opens it only to check a code) |
| `mfa_recovery_codes` | HMAC digests of recovery codes | two-factor authentication (contract) | until used or replaced | nobody |
| `mfa_challenges` | a sign-in waiting for a code: its token digest and tries | signing in (contract) | 5 minutes; the cleanup deletes it | nobody |
| `pending_registrations` | a sign-up waiting for its code: email, name, password hash, the code's digest | signing up (steps before a contract) | 15 minutes; the cleanup deletes it | nobody |
| `email_verifications` | a verification code's digest for an account | verifying an email (contract) | 15 minutes; the cleanup deletes it | nobody |
| `email_block_links` | an address and its block link's digest | honouring "don't email me" (legal obligation, legitimate interest) | a year; the cleanup deletes it | nobody |
| `email_blocks` | an address that asked for no email, when | a suppression list (legal obligation): it must outlive everything else | until its owner unblocks it | admins (the Email blocks page) |
| `admins` | who is an admin, their role, MFA setup window | privileged access (legitimate interest) | until revoked from the command line | admins (the Admins page) |
| `admin_sessions` | an admin session's digests, CSRF token, IP, user agent | admin sign-in (legitimate interest) | 15 minutes idle, 8 hours at most | admins (their own, the Admins page) |
| `user_suspensions` | a suspended account, the admin's reason, who suspended | stopping abuse (legitimate interest) | until unsuspended or the account is deleted | admins |
| `ip_blocks` | a blocked address or range, the reason, who blocked it | stopping abuse (legitimate interest) | at most 30 days; the cleanup deletes expired ones | admins |
| `app_errors` | error groups, scrubbed of emails, IPs, IDs and tokens; the last request ID | running the service (legitimate interest) | 30 days after last seen (`prune app_errors`) | admins (SQL until the Errors page) |
| `request_minutes` | requests per route and minute, never a path, IP or account | running the service | 48 hours, by the hourly rollup | admins |
| `request_hours` | the same, per hour | running the service | 90 days (`prune request_hours`) | admins |
| `metric_minutes` | other counters per minute (limit, listener, event names) | running the service | 48 hours | admins |
| `metric_hours` | the same, per hour | running the service | 90 days (`prune metric_hours`) | admins |
| `server_instances` | each server's host name, version and settings summary (never a secret) | running the service | a day after it stops | admins |

**Processors** (the organisation lists them in its records and contracts): the hosting provider (the servers, the database and its backups, the logs), the SMTP provider (every email's recipient and contents) and Cloudflare when it's in front ([deployment.md](deployment.md#cloudflare)). Nothing else receives data: there are **no third-party scripts, analytics or trackers** in either web build, and the CSP allows only the app's own scripts.

**Cookies,** all strictly necessary, so no consent banner: the session (`lewcontext_session`; its CSRF token travels in a header, never a cookie), a sign-in's MFA challenge (`lewcontext_mfa`), a sign-up's registration cookie, the admin session (`lewcontext_admin`), the admin door's MFA challenge and the admin build's assets pass (`__Host-` or `__Secure-` in production). None tracks anyone across sites.

**Logs** hold, per request, the route pattern (never the path or query), status, timing, the client IP and the signed-in `usr_` ID, and the [audit lines](#audit-lines); error text is scrubbed ([api/observability.md](api/observability.md)). Keep them **30 days**, apart from the audit lines, which are kept **a year** with the audit events they stand for and hold only a cut IP ([deployment.md](deployment.md#logs-and-health-checks)).

**Backups** are the hosting provider's; 30 days' rotation is recommended. Erased data leaves the backups as they expire: say so in the privacy notice.

## Retention

Every age is a registered prune target, so `make prune` (no target) lists the whole schedule, and every confirmed run records `prune.completed` (`target`, `deleted`, `kept`, `cutoff`): the evidence that retention runs ([api/database.md](api/database.md#pruning)). Schedule each target daily from cron ([deployment.md](deployment.md#retention-and-erasure)).

| Data | Kept | How |
| --- | --- | --- |
| **Audit events** (`events.Audit`: every `auth.*`, `admin.*`, `server.*`, `prune.*` name, and `alert.*` to come): sign-ins and attempts, MFA, passwords, sessions, the account's lifecycle, everything admins read and did, servers, retention itself | **1 year** | `prune audit_events` |
| Other events (`workspace.*`, `project.*`, `rate_limit.*`, `email.*`) | 90 days | `prune events` (it no longer touches audit events) |
| **IPs in events** | full for 30 days, then cut to the IPv4 /24 or IPv6 /48 | `prune pseudonymise` |
| **Emails in events** (sign-in and sign-up attempts, reset requests, invitations, the admin door, admins' searches) | readable for 30 days, then replaced by a keyed pseudonym | `prune pseudonymise` |
| `app_errors` | 30 days after last seen | `prune app_errors` |
| `request_minutes`, `metric_minutes` / `request_hours`, `metric_hours` | 48 hours / 90 days | the server's rollup / `prune request_hours`, `metric_hours` |
| Sessions, limits, links, codes, challenges, sign-ups, IP blocks | until they end | the server's 15-minute cleanup |
| Accounts, workspaces, projects | until deleted by their owner ([Erasure](#erasure)) | self-service, or `admin erase` |
| Server logs | **30 days** recommended | the operator's log store |
| Audit lines in the log (`msg: "audit"`) | **1 year**, as the audit events; the IP already cut to its /24 or /48 | the operator's immutable log store |
| Backups | the provider's rotation, 30 days recommended | the operator |

- **Minimising** (`pseudonymise`, decision 2): an event older than 30 days has its `ip` cut to its network (`198.51.100.0/24`, `2001:db8:1::/48`), and its payload's `email`, an Events search's `filters.email` and a list search (`q`) holding an `@` replaced by `hmac:` and the first 128 bits of an HMAC-SHA256 of the address, trimmed and lower-cased, in hex (`auth.Keys.EmailPseudonym`), under a key derived from `APP_KEY` with HKDF and its own label (`lewcontext email-pseudonyms v1`), as the other derived keys are. An Events search's `filters.ip` is cut the same way. The row's `minimised_at` is set, so a run never looks at it again (idempotent), in transactions of 1,000 rows (`FOR UPDATE SKIP LOCKED`), oldest first, through a partial index on what's left to do. An event whose listener hasn't run yet waits (a listener may need the address to send its email), and a delivery whose event is minimised can't be retried by an admin (409).
- **Still useful once minimised:** a pseudonym matches itself, so "how many attempts against this address" still works across old events, and the admin Events page's email search hashes what's typed and finds old rows too; its IP search finds a minimised event by the network an address falls in. The dashboard shows a minimised IP as its network.
- **Not reversible without `APP_KEY`:** the pseudonym is keyed, so someone with a database dump but not the key can't check guessed addresses against it, as they could with a plain hash. With the key, an address can be confirmed (that is the point of the search), never read back. `APP_KEY` can't be rotated yet ([Known gaps](#known-gaps)); a new key would make old pseudonyms unmatchable, not readable.
- **Inactive accounts** (decision 9): the Monitoring Usage page lists accounts not seen for two years (`users.last_seen_at`), built with A4's Usage page ([api/monitoring.md](api/monitoring.md)). Deleting them is a business decision, never automatic.

## Erasure

**Deleting an account** (Account & security, `DELETE /api/auth/me`) deletes the account, its sessions, memberships, MFA, reset link, admin row and the events *about* it (`events.user_id` cascades), and nulls it as the actor of events it caused for others. In the same transaction it erases the account's email from the events that remain (decision 3): wherever an event holds the address as typed (in any case) or as its pseudonym (already minimised), it becomes `"[deleted]"`, with the `name` beside it: a sign-in or sign-up attempt, a reset request, an invitation to it, the admin door, an admin's Events search (`filters.email`), and an admin's list search (`q`) when it is exactly the address. A partial search ("ada@") isn't found. IPs stay: they're minimised at 30 days anyway, and a record of an address's behaviour is a security interest that outlives the account. The lock order is unchanged: the user's row first, then the workspaces it owns in `id` order. Each of the three updates finds its rows through a partial expression index (the payload's `email`, `filters.email`, `q`; `TestErasureUsesItsIndexes` checks the plans), so a long admin trail can't push a deletion past the request's timeout.

**Erasure handled by staff,** for someone who can't sign in: after confirming who is asking outside the app, the operator runs

```sh
docker run --rm --env-file prod.env lewcontext admin erase -email ada@example.com            # preview
docker run --rm --env-file prod.env lewcontext admin erase -email ada@example.com -confirm
```

It does the same deletion and erasure, and deletes the workspaces only the account belonged to (each `workspace.deleted`). It refuses an admin's account (revoke admin first) and, naming them, the only owner of a workspace with other members: make someone else an owner first. It records `admin.user.erased` (`user`, `workspaces`) beside `auth.account_deleted`, with no actor. A suspended account can be erased.

What erasure can't reach: **backups** and **shipped logs** keep the data until they expire (30 days recommended); the **audit lines** (kept a year) never held the email, and hold the account's `usr_` ID and a cut IP; an **invitation to the address** sent by someone else stays with that workspace until it ends; the **block list** keeps an address only for an address without an account.

## Download my data

Account & security → **Your data** → **Download your data**: the password in a dialog, then a JSON file (decision 4, GDPR articles 15 and 20). `POST /api/auth/export {password}`: a session, CSRF, `LimitAuth` and `auth:per-account` (the password is checked as deleting the account checks it, `auth.password_checked` with `step: export_data`), then `export:per-user` (5 a day, counted once the password is right), recorded as `auth.data_exported`. The file:

- `account`: `id`, `name`, `email`, `createdAt`, `emailVerifiedAt`, `lastSeenAt`, `mfaEnabledAt` (on or off; never the secret or codes);
- `sessions`: the live ones, `createdAt`, `lastSeenAt`, `expiresAt` (never a token);
- `memberships`: each workspace, its name, the role and when they joined;
- `ownedWorkspaces`: each workspace the account owns, with its projects and the live invitations **the account sent** (an address it typed);
- `events`: the events about the account (`user_id`) and those it caused (`actor_id`), apart from the admin dashboard's: every event recorded in an admin request (`events.admin_request`, marked by the admin key's middleware whatever the event's name, so a dashboard action added later is left out too) and every `admin.*` name. An admin's reads and actions hold other people's data (a stranger's invitations it revoked, an address it unblocked, an account it suspended) and belong to the admin trail. Newest first, within their retention, at most 10,000 (`eventsTruncated` says when there were more), payloads as stored apart from an operator's note: `auth.suspended` and `auth.unsuspended` from the command line hold the reason the operator typed, which the export leaves out (keeping `by: cli`); from the dashboard the reason is in the `admin.*` event, never exported. The IP and user agent only on events it caused: an event someone else caused about it (an owner changing its role, a sign-in attempt on its address) carries theirs.

**Never:** a password hash, an MFA secret or recovery code, a token or digest, a CSRF token, an internal ID, or anyone else's data beyond what the account itself typed (another member's name or email, an invitation a co-owner sent, someone else's IP). Each section is a struct of its own listing its fields, so a new column is never exported by accident; `TestDownloadMyData` walks the JSON for those keys and searches it for the actual secrets and the other people's values.

**Synchronous:** the quotas bound the size (20 owned workspaces of at most 1,000 projects, 10,000 events), so it fits the request; nothing is stored or emailed, so there are no export files to secure or expire. A suspended account has no session, so it can't export; for someone who can't sign in, after confirming who is asking:

```sh
docker run --rm --env-file prod.env lewcontext admin export -email ada@example.com > ada.json
```

which records `admin.user.exported` (no actor). Send the file only by a channel you've checked is theirs.

## Audit lines

Every audit event (the 1-year class above) is also written as a log line as its row is inserted (decision 5): `msg: "audit"`, `event` (`evt_…`), `name`, `actor`, `user` and `workspace` (public IDs, empty when none), `ip` cut to its IPv4 /24 or IPv6 /48 (as minimising cuts an old event's), `createdAt` and the request's `requestId`. **Never the payload**, so never an email or a name, and never a full IP: the `evt_` leads back to the row, which holds the full address for its first 30 days. **Keep them a year**, as long as the audit events they stand for (the rest of the log 30 days): route `msg: "audit"` lines to their own store. Ship the server's log off the host to storage nobody on the server can change (S3 Object Lock, a SIEM, a provider's immutable archive): a row altered or deleted in the database then disagrees with a record that can't be. The line is written in the event's transaction, so a line whose event isn't in the table was either rolled back (its request answered an error: its access line, by `requestId`, says so) or deleted since. The lines are at `info`: with `LOG_LEVEL=warn` or `error` they aren't written, and the server warns at startup. The command line's commands write theirs to stderr.

There's no hash chain (it fights pruning, minimising and erasure, which rewrite rows on purpose); the shipped lines are the tamper evidence.

## Database roles

[deployment.md](deployment.md#database-roles) recommends two (decision 6): a **migration role** that owns the tables and runs `migrate`, and a **runtime role** the servers, `prune` and `admin` use, with `SELECT, INSERT, UPDATE, DELETE` only: no DDL, no `TRUNCATE`, not the owner. A compromised server then can't drop the audit table or change its schema. `events` can't be insert-only while erasure and minimising update it; the shipped audit lines are the evidence instead.

## Events CSV

The admin Events page's **Download CSV** (decision 7): the page's filters, newest first, at most 10,000 rows (`X-Export-Truncated: true` when there were more; the page says to narrow the filters), with the time, `evt_`, name, failed, actor, user and workspace (with emails and names as the page shows them), IP and payload. Any admin may (it's a read), it takes one of the dashboard's two summary slots (busy: 503), and it's recorded as `admin.event.exported` with the filters and the row count before a byte is sent. Every cell a spreadsheet would read as a formula (starting, after spaces, with `=`, `+`, `-` or `@`, or with a tab or carriage return) starts with an apostrophe, as the Admins page's CSV does ([api/admin.md](api/admin.md#events)).

## Control mapping

Guidance for an auditor's questions, not a certification: an auditor decides what satisfies a control.

| Requirement | Control in the app |
| --- | --- |
| GDPR art. 5(1)(c) data minimisation, 25 privacy by design | Counts over names where a count answers the question; IPs and emails minimised at 30 days; no third-party scripts; error text scrubbed; access lines without paths |
| GDPR art. 5(1)(e) storage limitation | The [retention schedule](#retention), enforced by prune targets, evidenced by `prune.completed` |
| GDPR art. 15, 20 access and portability | [Download my data](#download-my-data); `admin export` |
| GDPR art. 16 rectification | Name changes in Account & security; the email can't change yet ([Known gaps](#known-gaps)) |
| GDPR art. 17 erasure | [Account deletion with email redaction](#erasure); `admin erase` |
| GDPR art. 21 objection (email) | The block link in every invitation; the block list ([api/auth.md](api/auth.md#blocking-an-email-address)) |
| GDPR art. 30 records of processing | The [data map](#data-map) (the organisation's record builds on it) |
| GDPR art. 32 security of processing | TLS required, Argon2id, MFA (required for admins), secrets sealed with `APP_KEY`, rate limits, CSP, Monitoring |
| GDPR art. 33, 34 breach notification | [incident-response.md](incident-response.md); Events filters and the CSV for scoping |
| SOC 2 CC6.1, ISO 27001 A.8.5 secure authentication | Passwords, MFA, session expiry, step-up for admin actions |
| SOC 2 CC6.2, CC6.3, ISO A.5.18, A.8.2 provisioning, privileged access, review | Admins granted only from the command line, with roles; the Admins page and its CSV for access reviews; stale-admin warnings |
| SOC 2 CC7.2, ISO A.8.15, A.8.16 logging and monitoring | The audit trail, every admin read recorded, structured logs with request IDs, [audit lines](#audit-lines) shipped off the host, Monitoring |
| SOC 2 CC7.3, CC7.4, ISO A.5.24–A.5.26 incident handling | The [runbook](incident-response.md); suspension, IP blocks, revoking invitations and sessions |
| SOC 2 CC8.1 change management | `server.started` with the version and a settings summary; mixed versions on Overview |
| ISO A.8.10 information deletion, A.8.11 data masking | Erasure, retention, minimising |

## Known gaps

Written down so nobody claims otherwise:

- **`APP_KEY` can't be rotated:** changing it locks out every account with MFA on, and makes old pseudonyms unmatchable. Needs its own plan before a key-rotation policy can be claimed.
- **No email change:** rectifying an address means deleting the account and signing up again. Email verification exists ([api/auth.md](api/auth.md#email-verification)).
- **No SSO (SAML, OIDC) or SCIM.**
- **No audit log for workspace owners yet:** planned, once each event's data has been judged safe for owners to read (other members' IPs and emails, admins' reads). Until then events are admin-only.
- **Erasure misses partial searches** an admin typed (`q` that's part of an address) and free-text admin reasons that name someone; both leave with the 1-year audit retention.
- **Audit lines for rolled-back events:** a line is written as its row is inserted, so the rare event whose transaction then rolls back leaves a line without a row.
- **Backups, disaster recovery, data residency and encryption at rest** depend on the hosting provider, and are the operator's to document.
- **Contracts and policies** (DPAs with processors, a sub-processor list, a privacy notice, security policies, a breach register) are the organisation's.

## Tests

- **Retention:** `TestRetentionClasses` (`audit_events` keeps audit names a year while `events` deletes the others at 90 days, neither touching the other's rows, both recording `prune.completed`), `TestConfirmDeletesOnlyOldEventsWithoutPendingDeliveries` (`prune.completed`'s payload; a preview and a cancelled run record none) (`internal/prune`); `TestAuditEvents` (`internal/events`).
- **Minimising:** `TestPseudonymise` (IPs cut to /24 and /48; the payload's email, an Events search's email and IP and a list search with an `@` minimised, numbers kept; a second run changes nothing; recent events and those a listener still needs untouched; no address left; refused without the key; another key's pseudonym differs), `TestEmailPseudonym` (stable in any case, keyed, not a plain hash, its own key, never an address) (`internal/auth`); `TestAdminEventsFindMinimisedEvents` (the email search finds the pseudonym, the IP search the network), `TestAdminRetryRefusesMinimisedEvents`.
- **Erasure:** `TestErasureUsesItsIndexes` (`internal/store`: each update's plan uses its index), `TestDeletingAnAccountErasesItsEmail` (`endpoints/workspace`: an attempt in upper case, an invitation, a minimised event, the name beside the email, an Events search, an exact list search; IPs kept; a partial search and another address untouched; a refused deletion erases nothing; no event holds the address after), `TestEraseAccount` (`cmd/admin`: the preview changes nothing; an admin refused; the only owner of a workspace with members refused, naming it and not the solo one; the solo workspace and its projects deleted; `admin.user.erased` and `auth.account_deleted` without an actor).
- **Export:** `TestDownloadMyData` (`endpoints/account`: CSRF, sign-in, a wrong password 422 and unrecorded; the file's headers and sections; no secret key anywhere in the JSON; none of the password hash, TOTP secret, recovery codes and digests, session and invitation tokens and digests, CSRF token, the co-owner's email, name and ID, their invitation, their IPs, the account's admin searches, or what its dashboard actions touched (a stranger's revoked invitation, an unblocked address, a suspended account, its reason); IPs only on its own events; `auth.data_exported`; the sixth export 429 with `rate_limit.reached`), `TestSuspendedAccountsCantExport`, `TestExportLeavesOutOperatorNotes` (a command-line suspension's and unsuspension's reason left out, `by` kept, the rows unchanged), `TestExportAccount` (`cmd/admin`).
- **Audit lines:** `TestAuditEventsAreLogged` (`internal/store`: one line per audit event with its IDs, the IP cut to /24 or /48 (never the full address), time and request ID, naming a row that exists; none for other events; no email or name logged), `TestEventsCarryTheRequestID` (a sign-in attempt's line, with no email).
- **CSV:** `TestAdminEventExport` (the filters, newest first, formula cells defused, recorded with the row count, 10,000 at most with `X-Export-Truncated`, refused parameters).
- **The data map:** `TestEveryTableIsInTheDataMap` (`internal/database`).
- Vitest: `pages/app/settings.test.tsx` (Download your data: the password, a refusal on its field, the file saved, a 429 kept in the dialog), `pages/admin/events.test.tsx` (Download CSV: the page's filters, a refusal as a toast, the file saved).
