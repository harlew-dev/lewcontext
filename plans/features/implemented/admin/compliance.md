# Compliance: retention, erasure, export, audit evidence

Status: **implemented 2026-11-01**; decisions confirmed as recommended. See [How the build differs](#how-the-build-differs-from-the-plan). Plan A7 of the [admin plans](../../admin/README.md). Needs [events-and-mail](events-and-mail.md), [observability](observability.md) and [actions](actions.md). Docs it changed: [database.md](../../../../docs/api/database.md) (pruning), [events.md](../../../../docs/api/events.md), [auth.md](../../../../docs/api/auth.md), [endpoints.md](../../../../docs/api/endpoints.md), [rate-limits.md](../../../../docs/api/rate-limits.md), [deployment.md](../../../../docs/deployment.md), [admin.md](../../../../docs/api/admin.md); new [compliance.md](../../../../docs/compliance.md) (the data map and control mapping) and [incident-response.md](../../../../docs/incident-response.md) (the runbook); also [observability.md](../../../../docs/api/observability.md) (audit lines), [testing.md](../../../../docs/api/testing.md), [seeding.md](../../../../docs/api/seeding.md), [naming.md](../../../../docs/naming.md) (the `prune` area), the UI's [pages.md](../../../../docs/ui/pages.md), [api.md](../../../../docs/ui/api.md) and [testing.md](../../../../docs/ui/testing.md), `docs/README.md` and AGENTS.md.


## How the build differs from the plan

Built on the events worktree (branch `admin-run/events`), after merging main (A3 phases 2–3, A4 part 1), with every recommendation in [Decisions](#decisions-confirmed-2026-11-01-as-recommended) taken as accepted.

- **Prune targets are snake_case,** as the existing ones are: `audit_events` (the plan's `audit-events`) and `pseudonymise`. `pseudonymise` deletes nothing: a `Pruner` with `Minimises` set runs `store.PseudonymiseEvents` instead of the delete, and `store.Prunable` gained `Scope` (the rows a target looks after), so `events` and `audit_events` split one table without counting each other's rows as "kept". It runs from cron with `make prune`, not the cleanup loop, so it's scheduled and evidenced like the rest.
- **`prune.completed` is built here** (monitoring decision 10 had placed it in A4 part 2): `prune.Run` records it after every confirmed run that finishes (`target`, `deleted`, `kept`, `cutoff`), a new `prune` event area (label "Retention"). A4's Data page can read it.
- **Audit events** are whole areas, `events.Audit`: every `auth.*`, `admin.*`, `server.*` and `prune.*` name (and `alert.*`, to come). The plan's "`auth.*` sign-in, MFA, password and session events" became all of `auth.*`: sign-ups, verification and account deletion are the account's lifecycle, which an access review asks for too. `email.*` (the block list's history) is 90 days: the list itself is kept as long as it's needed.
- **Minimising** marks each event with a new `events.minimised_at` column (edited into `007_create_events`, with a partial index on what's left to do), so a run is idempotent and reads only the last 30 days. Beyond the payload's `email`, it covers an admin's Events search (`filters.email`, `filters.ip`) and a list search (`q`) holding an `@`. An event with a pending delivery waits (a listener may need the address), and an admin can't retry a minimised event's delivery (409; the bulk retry skips it). The pseudonym is `hmac:` and 128 bits of HMAC-SHA256 in hex, under a key derived with HKDF (`lewcontext email-pseudonyms v1`), as `auth/encrypt.go` derives the others. Minimised IPs show as their network on the Events page, and its IP search also matches the network an address falls in.
- **Erasure** also redacts an admin's Events search and an exact list search for the address, not only `payload.email`; a partial search isn't found (Known gaps in [compliance.md](../../../../docs/compliance.md#known-gaps)). It runs in `DeleteAccount`'s transaction after the existing locks (the user's row, then owned workspaces in `id` order: A6's order). **`admin erase`** deletes the workspaces only the account belongs to (each `workspace.deleted`) rather than refusing them, since the operator can't delete a workspace any other way, and refuses an admin's account (revoke first), as `admin suspend` does. `store.EraseAccount` and `DeleteAccount` share the steps.
- **Download my data** follows the plan's password-only check (no code with MFA on, as deleting the account), counts `export:per-user` in the handler once the password is right (so typos don't use the day's five), and caps the events at 10,000 (`eventsTruncated`). Two choices the plan left open: the invitations exported are only those **the account sent** (a co-owner's are someone else's typing), and the events are those about it plus those it caused **outside the admin dashboard** (an admin's reads and actions hold other people's data and belong to the admin trail); the IP and user agent appear only on events the account caused. A suspended account has no session, so it can't export; `admin export` can, for an operator handling a verified request (the user should confirm, below).
- **Audit lines** are written as the row is inserted, in its transaction (the `RETURNING` reads the actor's, user's and workspace's public IDs), so a line can outlive a rolled-back event; the docs say how to tell. The server warns at startup when `LOG_LEVEL` would hide them.
- **The Events CSV** is `GET /api/admin/events/export` (`admin.EventExport.Show`), the list's filters, generated on the server (a page holds 100 rows at most), behind one of the two summary slots, recorded before sending; the admin client gained `Http.text` for an answer that isn't JSON.
- **Inactive accounts** (decision 9) shipped with A4's Usage page, which lists accounts not seen for two years from `users.last_seen_at`; nothing of it is built here.
- **Docs:** `docs/deployment.md` gained [Retention and erasure](../../../../docs/deployment.md#retention-and-erasure) (a cron schedule) and [Database roles](../../../../docs/deployment.md#database-roles) (the SQL for the split); the data map is enforced by `TestEveryTableIsInTheDataMap`.
- **After the security and privacy review** (each its own `fix:` commit):
  - **An admin's own export left out only `admin.*` names,** but dashboard actions also record `workspace.invitation.revoked`, `email.unblocked`, `auth.suspended`, `auth.unsuspended` and `auth.mfa.disabled` with the admin as actor, so an admin's export held strangers' invitation addresses, an unblocked address and other workspaces' IDs. Events now carry `admin_request` (edited into `007_create_events`), set from the context (`events.WithAdminRequest`) by the admin key's middleware for every `/api/admin/` route, and the export leaves those out. Chosen over matching an `admin.*` event by `request_id` (no index or invariant that every dashboard request records one needed): it marks a future action without anyone remembering to.
  - **Audit lines held the full IP** while the docs said to keep logs 30 days, though the lines are the evidence for audit events kept a year. The line's `ip` is now cut to its /24 or /48 (`minimiseIP`), and the docs give audit lines their own retention, a year, the rest of the log 30 days.
  - **Erasure read every admin read** in the deletion's transaction to find a search for the address, with no index on `q` or `filters.email`: a long admin trail could have pushed self-deletion past its timeout. Two partial expression indexes (`events_search_email_idx`, `events_search_q_idx`, in `007_create_events`) and the matching `?` predicates make each update an index lookup; `TestErasureUsesItsIndexes` checks the plans with a new `dbtest.Explain`.
  - **The export held operator notes:** `auth.suspended` and `auth.unsuspended` from the command line carry the operator's reason. The export now leaves `reason` out of those two (keeping `by`); the rows are unchanged.
  - **The `LOG_LEVEL` warning was itself hidden at `LOG_LEVEL=error`** (it was a warning). It's logged at `error` now.
  - **The export's scope against erasure's** is written down in [compliance.md](../../../../docs/compliance.md#download-my-data): the export is by account ID (`user_id`, `actor_id`), erasure by address, so reset and sign-up requests, invitations from others and admins' searches for the address are erased but not exported, each with its reason. The export wasn't widened.
  - **A retry could race pseudonymisation:** `RequeueDelivery` locked only the delivery, so a run could minimise the event between the retry's check and its commit, and the listener would then send to a pseudonym. Both retries now lock the event's row (`FOR UPDATE`, the event before the delivery), which a run skips (`SKIP LOCKED`) or the retry waits for.
  - **The Events CSV held its summary slot for the whole request,** with no deadline. Its query now has the summaries' deadline (503 past it) and lets the slot go before the file is written.
- **Also done here (A6's merge-time tasks):** `ip_block_refused_total` and the `ip_blocks_loaded_timestamp_seconds` gauge on `/metrics` (no labels), and `admin.access_refused` keeping the browser when recorded by a page or an IP block's check.

**For the user to confirm:**
- Whether `admin export` should refuse a suspended account (built: allowed, since the right of access doesn't end with a suspension).
- Whether Download my data should ask for a code too with MFA on (built: the password only, as the plan and account deletion do).
- Whether all of `auth.*` and `email.*`'s retention classes are right (above).

## Why, and what this can't do

No feature set makes an app "pass" GDPR, SOC 2 or ISO 27001. Those cover the organisation as much as the software: contracts with processors, policies, training, risk assessments, an auditor's judgement. What software can do is **provide the technical controls and produce the evidence** those reviews ask for, and avoid the gaps that fail them. Today the app is in good shape on access (Argon2id, MFA, short admin sessions, every admin read recorded) and weak on four things auditors and data-protection law look at:

1. **Retention is one rule** (events 90 days) and it isn't written down per kind of data.
2. **Erasure leaves traces:** deleting an account deletes the events *about* it, but its email stays in other events (login attempts, reset requests, invitations to it) until pruned.
3. **There's no way for a person to get their data** (GDPR articles 15 and 20).
4. **The audit trail lives only in a table the app can change**, with nothing off the server to show it wasn't altered.

Health data (HIPAA) and card data (PCI DSS) are out of scope: the app holds neither.

## 1. The data map (`docs/compliance.md`)

One table per database table: each personal-data field, why it's kept (purpose and lawful basis), how long, who can see it, and where it goes (processors: the hosting provider, the SMTP provider, Cloudflare when used). Plus: the cookies (all strictly necessary: session, CSRF, admin, MFA challenge; so no consent banner), the absence of third-party scripts and analytics, and the logs' contents.

`TestEveryTableIsInTheDataMap` (decision 8) reads the table names from `docs/compliance.md` and fails when a migration adds a table the map doesn't list, as `TestEveryTableHasAFactory` does for factories.

## 2. Retention per kind of data (decisions 1, 2)

| Data | Kept | How |
| --- | --- | --- |
| **Audit events**: `auth.*` sign-in, MFA, password and session events, `admin.*`, `alert.*`, `server.*`, `prune.completed` | **1 year** | a new prune target, `audit-events` |
| Other events (workspaces, projects, invitations, limits) | 90 days (as today) | `events`, which now keeps audit names for `audit-events` |
| **IPs in events** | full for 30 days, then cut to the IPv4 /24 or IPv6 /48 | a daily prune target, `pseudonymise` |
| **Emails in events** (login attempts, reset requests, invitations, admin door) | readable for 30 days, then replaced by a keyed hash (`HMAC-SHA256` with a key derived from `APP_KEY`) | `pseudonymise` |
| `app_errors` | 30 days after last seen | [observability](observability.md) |
| `request_minutes`, `metric_minutes` / their hourly sums | 48 hours / 90 days | observability |
| `alerts` | 1 year | [alerts](../../admin/alerts.md) |
| `table_stats_daily` | 400 days | [monitoring](../../admin/monitoring.md) |
| Sessions, limits, links, challenges, IP blocks | until they end | the server's cleanup |
| Accounts, workspaces, projects | until deleted by their owner | self-service deletion |
| Server logs | **30 days recommended** | the operator's log store; [deployment.md](../../../../docs/deployment.md) |
| Backups | the provider's rotation (30 days recommended); erased data leaves backups as they expire | the operator; documented |

- **Pseudonymised, still useful:** a hashed email still matches itself, so "how many attempts against this address" works on old events, and the Events page's `email` filter hashes what's typed to match older rows too. An IP cut to its /24 still shows a network.
- **Every age is one registered prune target,** so `make prune` lists the whole schedule and `prune.completed` proves it ran. Monitoring warns when it hasn't.
- **Inactive accounts** (decision 9): the Usage page lists accounts not seen for two years. Deleting them is a business policy, not a default.

## 3. Erasure: deleting an account deletes its traces (decision 3)

`DeleteAccount` already deletes the account, its memberships and the events about it (`user_id` cascades), and nulls it as actor. In the same transaction it will also:

- replace `payload.email` with `"[deleted]"` (and `payload.name`, where present) in every event whose `lower(payload->>'email')` is the account's address, or whose hash is its hash (A1's expression index keeps this fast);
- leave IPs alone: they're pseudonymised at 30 days anyway, and a security record of *an* address's behaviour is a legitimate interest that outlives the account.

**Erasure handled by staff,** for someone who can't sign in: `admin erase -email … -confirm` runs the same deletion. It refuses when the account is the only owner of a workspace with other members, listing those workspaces, as self-service deletion does; the operator resolves ownership first. It records `admin.user.erased` (`user`, no actor) besides `auth.account_deleted`.

## 4. "Download my data" (decision 4)

| | |
| --- | --- |
| Route | `POST /api/auth/export {password}` |
| Requires | session, CSRF, `limitAuth` (the password is checked like deletion's), `export:per-user` 5 a day |
| Answers | `200 application/json` with `Content-Disposition: attachment; filename="lewcontext-export-<date>.json"`, streamed |
| Records | `auth.data_exported` |

The file holds the profile (name, email, created), MFA on or off (never its secret or codes), sessions (created, last seen, user agent), memberships with roles, the workspaces the account owns with their projects and pending invitations, and the events about or by the account (audit and other, within their retention) with their payloads as stored. Never: hashes, tokens, secrets, other people's private data beyond what the account itself typed (the addresses it invited).

- **Synchronous:** quotas bound the size (at most 20 owned workspaces of at most 1,000 projects), so it fits the request timeout, and nothing is stored or emailed. No export files to secure or expire.
- **In Account & security:** a "Download your data" settings action (an `outline` button in its own section, [forms-and-actions.md](../../../../docs/ui/forms-and-actions.md)), asking for the password in a dialog.
- **For someone who can't sign in:** `admin export -email … > file.json` on the CLI, after the operator has verified who's asking. Records `admin.user.exported` (no actor).

## 5. Audit evidence off the server (decisions 5, 6)

- **Audit log lines:** every audit event (the 1-year class above) is also written as a JSON log line, `msg: "audit"`: `event` (`evt_…`), `name`, `actor` and `user` (`usr_…`), `workspace`, `ip`, `requestId`, `createdAt`. **No email or name:** the `evt_` leads back to the row. The operator ships logs off the host to storage that can't be changed (S3 Object Lock, a SIEM, a provider's immutable log archive). A row altered or deleted in the database then disagrees with a record nobody on the server can touch.
- **Database roles:** [deployment.md](../../../../docs/deployment.md) will recommend two roles. A **migration role** owns the tables and runs `migrate`; the **runtime role** the server uses gets `SELECT, INSERT, UPDATE, DELETE` only (no DDL, no `TRUNCATE`). A compromised server can't drop the audit table or change its schema. Making `events` insert-only isn't possible while erasure and pseudonymisation update it; the shipped log lines are the tamper evidence instead.
- **Events CSV for auditors and incidents** (decision 7): the Events page gets "Export CSV" for its current filters, at most 10,000 rows with payloads as the page shows them, recorded as `admin.event.exported` with the filters and row count.

## 6. Incident runbook (`docs/incident-response.md`)

Written for the person on call, using what A1–A6 built:

1. **Detect:** an alert, the external uptime check, or a report.
2. **Triage:** Monitoring's Overview and Security; Events filtered by IP, account and time; the error group's request ID in the logs.
3. **Contain:** suspend accounts, block addresses, revoke a workspace's invitations ([actions](actions.md)); `admin sessions revoke-all` if sessions may have leaked; rotate `ADMIN_ROUTE_KEY`, the SMTP password and `METRICS_TOKEN` if exposed. (`APP_KEY` can't be rotated yet; see Known gaps.)
4. **Scope:** who was affected, from Events exports for the window and addresses involved.
5. **Notify:** under GDPR, the supervisory authority within 72 hours of becoming aware of a personal-data breach that's a risk to people (article 33), and the people affected when the risk is high (article 34). Wording templates in the runbook.
6. **Record:** every breach goes in a breach register (article 33(5)) kept by the organisation, with facts, effects and remedial action, even when not notified.
7. **Review:** what signal or alert would have caught it sooner.

## Control mapping (in `docs/compliance.md`)

Guidance for an auditor's questions, not a certification; an auditor decides what satisfies a control.

| Requirement | Control in the app |
| --- | --- |
| GDPR art. 5(1)(c) data minimisation, 25 privacy by design | Counts over names where a count answers the question; pseudonymisation at 30 days; no third-party scripts |
| GDPR art. 5(1)(e) storage limitation | The retention schedule, enforced by prune targets, evidenced by `prune.completed` |
| GDPR art. 15, 20 access and portability | "Download my data"; `admin export` |
| GDPR art. 17 erasure | Account deletion with email redaction; `admin erase` |
| GDPR art. 30 records of processing | The data map (the organisation's record builds on it) |
| GDPR art. 32 security of processing | TLS required, Argon2id, MFA, sealed secrets, rate limits, CSP, monitoring and alerts |
| GDPR art. 33, 34 breach notification | The runbook; Events scoping and export |
| SOC 2 CC6.1, ISO 27001 A.8.5 secure authentication | Passwords, MFA (required for admins), session expiry |
| SOC 2 CC6.2, CC6.3, ISO A.5.18, A.8.2 provisioning, privileged access, review | CLI-only admin grants with roles; the Admins page and its CSV for access reviews; stale-admin warnings |
| SOC 2 CC7.2, ISO A.8.15, A.8.16 logging and monitoring | The audit trail, admin reads recorded, structured logs with request IDs, Monitoring, audit lines shipped off the host |
| SOC 2 CC7.3, CC7.4, ISO A.5.24–A.5.26 incident handling | Alerts, the runbook, the actions |
| SOC 2 CC8.1 change management | `server.started` with version and settings summary; mixed-version warnings |
| ISO A.8.10 information deletion, A.8.11 data masking | Erasure, retention, pseudonymisation |

## Known gaps, written down

Stated in `docs/compliance.md` so nobody claims otherwise:

- **`APP_KEY` can't be rotated** (changing it locks out every MFA account). Needs its own plan before a key-rotation policy can be claimed.
- **No email change** (rectification of the address means deleting and re-registering). Email verification exists since this plan was drafted.
- **No SSO (SAML / OIDC) or SCIM,** which enterprise customers ask for.
- **No audit log for workspace owners yet: planned for later.** Owners will read their own workspace's history (enterprise customers commonly ask for it) once we've decided, event by event, which data is available and safe for them to read. Other members' IPs and emails, and the admins' own reads, are examples that may not be. Until then, events are admin-only. It gets its own plan.
- **Backups, disaster recovery, data residency and encryption at rest** depend on the hosting provider and are the operator's to document.
- **Contracts and policies** (DPAs with processors, a sub-processor list, a privacy notice, security policies) are the organisation's.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| An export used to steal an account's data with a stolen session | The password is required (with `auth:*` limits), 5 a day, recorded |
| An export leaking other users' data | Only what the account owns or typed; tests check the file against another member's private fields |
| Pseudonymised emails reversed by guessing | A keyed hash (HMAC), not a plain hash; the key derives from `APP_KEY`, which never leaves the server |
| Erasure missing copies | Redaction in the deletion transaction; logs and backups covered by their documented retention |
| A tampered audit trail going unnoticed | Audit lines shipped to immutable storage; the runtime role can't change the schema |
| Retention silently not running | `prune.completed` (built here); the Data page (A4) and an alert (A5) to come |

## Tests

- **Retention:** `audit-events` keeps audit names a year while `events` removes others at 90 days; neither touches the other's rows; both record `prune.completed`.
- **Pseudonymisation:** after 30 days IPs are cut to /24 and /48 and emails hashed; running it twice changes nothing; the Events email filter finds hashed rows; deletion's redaction finds hashed rows too.
- **Erasure:** deleting an account redacts its email in login attempts, reset requests and invitations to it, in the same transaction; `admin erase` refuses a sole owner with members and lists the workspaces.
- **Export:** the JSON holds every listed section and no field from the sensitive list (`decodeAdmin`'s walk); a wrong password 422; the sixth export in a day 429; another member's private fields never appear.
- **Audit lines:** every audit event writes one, with no email or name; other events don't.
- **CSV:** bounded at 10,000 rows, matches the page's filters, recorded.
- `TestEveryTableIsInTheDataMap`.

## Decisions (confirmed 2026-11-01, as recommended)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Retention classes | Audit events a year; other events 90 days; the rest as the table above |
| 2 | Minimising old events | IPs cut to /24 or /48 and emails replaced by a keyed hash after 30 days |
| 3 | Erasure | Redact the account's email and name from other events in the deletion transaction; keep IPs; `admin erase` for staff-handled requests, refusing sole owners with members |
| 4 | Data export | Self-service, synchronous JSON after the password, 5 a day; `admin export` on the CLI for people who can't sign in |
| 5 | Tamper evidence | Audit events also written as JSON log lines without emails, for the operator to ship to immutable storage. No hash chain (it fights pruning and erasure) |
| 6 | Database roles | Documented split: migration role owns the schema, runtime role has DML only |
| 7 | Events CSV | For admins, at most 10,000 rows, recorded |
| 8 | The data map | `docs/compliance.md`, with a test that every table is listed |
| 9 | Inactive accounts | Listed on Usage after two years; never deleted automatically |

## Not included

- **Certification work**: policies, contracts, risk assessments, audits.
- **Email verification and email change**, **SSO / SCIM**, **`APP_KEY` rotation**: each its own plan (Known gaps).
- **Workspace audit logs for owners:** a later plan, once we've decided which event data is safe for owners to read (Known gaps).
- **Automatic deletion of inactive accounts** (decision 9).
- **Legal hold** (suspending deletion for a dispute).
- **Consent management**: nothing needs consent today; a plan that adds non-essential cookies or analytics must add it.
