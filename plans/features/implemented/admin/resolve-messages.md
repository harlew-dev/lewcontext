# Admin: incident discussions and resolution messages

Status: **implemented 2026-11-02**. Built as planned: `incident_cycles` and `incident_messages` (migrations 065 and 066), the shared `IncidentThread`, viewer comments, operator resolution with a comment, audited redaction, and timestamp-aware error recurrence with discussion-aware pruning. Docs updated: [monitoring](../../../../docs/api/monitoring.md), [admin](../../../../docs/api/admin.md), [events](../../../../docs/api/events.md), [database](../../../../docs/api/database.md), [rate-limits](../../../../docs/api/rate-limits.md), [compliance](../../../../docs/compliance.md), [forms and actions](../../../../docs/ui/forms-and-actions.md), [design](../../../../docs/ui/design.md).

Builds on [actions](actions.md), [alerts](alerts.md) and [compliance](compliance.md). Names follow [naming.md](../../../../docs/naming.md). This plan owns the shared discussion/cycle storage and error-group recurrence. [Alert recovery](alert-recovery.md) owns alert health, recurrence and lifecycle notifications; [alert subscriptions](alert-subscriptions.md) owns recipient preferences and runtime destinations.

**Build order:** build this plan's storage, discussion UI and error-group behaviour first, then alert recovery, then subscriptions. Ship the alert-resolution action and removal of automatic resolution together with alert recovery. Do not ship the former intermediate design that allowed closing an active alert and silently creating a replacement.

## Why

A single overwritten resolution note cannot explain a team's investigation. Alerts and grouped errors need a persistent discussion, a clear resolution decision, and their earlier decisions visible when the same problem returns. This supersedes the earlier optional `resolved_message` design: comments are separate records, not text copied onto the current alert and into audit payloads.

## Confirmed product decisions

Confirmed on 2026-11-02:

| Question | Decision |
| --- | --- |
| Who participates? | Every signed-in admin, including viewers, may comment. Only operators may resolve. Comments and resolution need no fresh MFA code. |
| What does a comment's **Mark as resolved** do? | Resolve the current incident and designate that comment as the resolution. It does not resolve an individual discussion point. |
| Where are discussions available? | Both alerts and Monitoring error groups. |
| What happens on recurrence? | Reuse the same parent record and URL, append a new incident cycle, and retain earlier comments and resolutions within retention. |
| May an active alert be resolved? | No: only fresh, confirmed recovery permits alert resolution. Error groups have occurrences rather than a health probe; their separate resolution rule is below. |
| Can comments be changed? | Corrections are follow-up comments. Operators may redact a comment with fresh MFA and a reason, leaving a tombstone and audit metadata. |
| Do comments notify? | No. Alert lifecycle transitions notify, including manual resolution; text and identities stay dashboard-only. |

The previous decisions allowing an empty resolution, clearing the previous note on recurrence, operator-only commenting, no redaction and no manual-resolution notification are superseded. Plain-text rendering, validation and preservation of accepted text remain.

## Design and ownership

- An alert's **thread** is its stable `alt_` parent, keyed by signal after alert recovery lands. An error thread is the existing `err_` group, keyed by its existing fingerprint (source, route and scrubbed message). Different signals/fingerprints never share a thread automatically.
- An **incident cycle** is one opening through one resolution. A relapse before resolution remains in that cycle, with a new lifecycle transition; a recurrence after resolution starts the next cycle. Each cycle keeps its own comments and resolution marker.
- A comment belongs to exactly one cycle. Posting never changes lifecycle state. Discussions remain available on resolved cycles; a follow-up there cannot resolve a newer incident.
- An operator may select any non-redacted comment from the **current** cycle, including one another admin wrote. Show the comment author separately from the operator who resolved it. Old-cycle comments cannot be reused as a new resolution; write a new explanation referencing the previous decision in plain text.
- Resolution updates the cycle and parent projection and records its audit event atomically. Concurrent resolutions have one winner; the other receives 409 and must reload. No automatic retry against a newer cycle.

## API and data changes

### Tables (approved design)

Use two shared, narrowly scoped tables rather than two independent comment implementations or an unconstrained polymorphic ID:

| Table | Fields and constraints | Indexes / deletion |
| --- | --- | --- |
| `incident_cycles` | Internal bigint key; PostgreSQL-generated `inc_` public ID; exactly one nullable FK of `alert_id` / `app_error_id` (XOR CHECK); positive cycle number; `opened_at`, `resolved_at`, `resolution_source` (`admin`, `automatic_legacy`, `unknown_legacy`, null while open); `resolved_by` FK to `users` SET NULL; nullable `resolution_message_id` | Unique cycle number per parent and unique **open** cycle per parent, using partial indexes. Parent FKs CASCADE. Indexed parent/cycle order for history. |
| `incident_messages` | Internal bigint key; PostgreSQL-generated `msg_` public ID; cycle FK CASCADE; author FK to `users` SET NULL; body; `created_at`; nullable `redacted_at` / `redacted_by` (users SET NULL). Body is 1–2,000 Unicode characters after trim; body is null exactly when redacted | `(cycle_id, created_at DESC, id DESC)` for cursor reads. A composite FK from the cycle's resolution-message reference must ensure the selected message belongs to that cycle; add it after both tables exist. |

Do not add `resolved_message` columns or copy message bodies into cycles/events. Parent `resolved_at` remains the current-state projection for existing filters. Derive the current cycle by the highest cycle number through its parent index; expose public cycle IDs, not database keys. Preserve cycle resolver/source separately from the nullable author/resolver FKs, so account deletion is never mislabelled as automatic resolution.

Alert health/notification fields are owned by alert recovery, not duplicated here. Each new table needs a create migration pair, Bun model, its own factory, `store.DataTables` entry and compliance data-map row. Edit existing create migrations while squashed; allocate migration versions after inspecting the repository. Build queries with Bun only.

### HTTP contract

All routes are in `adminRoutes`, behind the key (current or previous as appropriate), admin session and CSRF for writes. Parent IDs are public IDs. Controllers are focused resource controllers in the existing `admin` area; no new package or dependency is needed.

| Route | Behaviour | Role / budget |
| --- | --- | --- |
| `GET /api/admin/alerts/{alert}` / `GET /api/admin/errors/{error}` | Existing detail responses add current cycle, current resolution reference/person/source, and the newest 20 cycles; no message bodies in parent lists | Any admin; existing read budget and one existing parent `.viewed` audit event |
| `GET /api/admin/alerts/{alert}/cycles` / `GET /api/admin/errors/{error}/cycles` | Older cycle summaries, cursor-paged | Any admin; read budget; `admin.alert.viewed` / `admin.error.viewed` with `section: cycles` and cursor |
| `GET /api/admin/alerts/{alert}/messages` / `GET /api/admin/errors/{error}/messages` | Required `cycle=inc_…`, `before=msg_…`, `perPage` 1–100 (default 20); newest-first cursor page, rendered chronologically within a page | Any admin; read budget; parent `.viewed` with `section: messages`, cycle and cursor |
| `POST` on either messages collection | `{cycle, body}`; 201 with saved message; stale but valid cycle is allowed for discussion, never silently reassigned | `Role(viewer)` with `LimitAdmin` on new `incident-comment:per-user` (60/hour, shared across both resources); no step-up |
| `PATCH /api/admin/alerts/{alert}` | Exactly one action: existing `{acknowledged: true}` or `{resolved: true, cycle, message: msg_…, revision}`; 200 with parent | Housekeeping: operator, existing `admin-action:per-user`, no code; alert recovery validates fresh health and revision |
| `PATCH /api/admin/errors/{error}` | `{resolved: true, cycle, message: msg_…, lastSeenAt}`; 204 | Housekeeping: operator, existing action budget; refuse a stale occurrence snapshot |
| `POST /api/admin/alerts/{alert}/messages/{message}/redaction` / error equivalent | Singleton redaction resource, `{reason}`; 201 with tombstone | `Action`: operator, step-up, reason 10–500, existing action budget |

Resolve `message` as a message public ID, not free text. Unknown/foreign parent, cycle or message is 404; well-formed stale resolution, already resolved, redacted selected message or occurrence conflict is 409; invalid body/action combination is 422; unknown/repeated query fields are 400. Scope all lookups through the path's parent and selected cycle. A cursor from a different cycle is invalid. Mutations never accept author, resolver or internal IDs from the caller.

Trim body, count Unicode characters (client and database agree), validate with `input.ValidText(body, true)` and enforce a bounded JSON body. Keep accepted text unsanitised; no `observe.Scrub`, HTML, Markdown, linkification or mention parsing. An empty comment is 422. Comments allow **2,000 characters**, superseding the former 500-character single-note limit. Cap at **1,000 comments per cycle**, counted under the cycle lock; redacted tombstones count. Quota refusal is 422 and does not prevent resolution using an existing comment.

### Error-group recurrence and races

- On a group's first persisted occurrence, create its initial cycle; on a genuinely newer occurrence after resolution, close-state becomes open and create exactly one new cycle. Retain all old messages and markers.
- Error collection is buffered. Compare the incoming group's **latest occurrence time** with the locked resolution time: only `incoming.last_seen_at > resolved_at` reopens it. An older/equal batch may increase counts/update aggregates but must not clear resolution. This uses the existing trusted server timestamps; clock synchronisation across servers remains an operational requirement.
- Resolve checks the submitted cycle and `lastSeenAt` against the locked group. A new occurrence since the page was read yields 409. A flush racing resolution serialises on the parent row; tests must cover both orderings and old buffered batches arriving after resolution.
- Error resolution means “reviewed/handled”, not proven signal recovery. It is allowed on any current open error group with an eligible comment. A later occurrence reopens it. Resolving a group never resolves its aggregate alert signal (`errors.new_panics`, etc.), and resolving that alert never resolves constituent error groups.
- Keep persistence and occurrence aggregation in `internal/store`; do not make the observer import application/store/events packages. Bulk upserts process parent locks in deterministic fingerprint order, maintain cycle creation in the same transaction, and preserve count/budget bounds.

## Audit, privacy and retention

- Record `admin.alert.commented` / `admin.error.commented` with parent, cycle and message IDs, actor and time; **no body**. Resolution is `admin.alert.resolved` / existing `admin.error.resolved`, with cycle/message IDs and actor, again no text. Alert recovery queues a separate sanitised `alert.resolved` lifecycle notice. Error discussions/resolutions send no external notices.
- Redaction records `admin.alert.redacted` / `admin.error.redacted` with parent/cycle/message, actor and reason, never the removed body. The reason must not repeat the sensitive material. Null the body in place; no hidden revision copy, body digest or quoted excerpt in events/logs. Redacting a resolution comment leaves the historical resolution marker and state intact, displaying **Resolution comment redacted**.
- Author/resolver revocation preserves attribution while their user exists; account deletion SET NULL displays **Deleted account**. Do not store snapshots of names/emails in comment rows. Discussion content is admin-only and excluded from ordinary account exports like other admin investigations.
- **Approved retention:** alert threads/cycles/comments last one year after the latest resolution or comment, only while resolved; unresolved alert threads stay. Error groups **without discussion** keep the existing 30-day last-occurrence retention. Once a group has a comment, keep its thread/cycles/comments while unresolved and for one year after the latest occurrence, resolution or comment once resolved. Redaction itself does not extend retention. Prune rechecks the parent under its row lock so it cannot delete a concurrently reopened/commented thread; cascade removes its cycles/messages. Document that recurrence after pruning creates a new thread and cannot restore deleted context.
- Free text is not automatically found by structured email erasure/minimisation. Update compliance's known gaps explicitly: operators handle requests concerning discussion text via audited redaction; backups expire under existing rotation. Do not claim automatic full-text erasure. Audit events contain references, not a second unredactable copy of the discussion.

## UI

Use one shared `IncidentThread` family in `components/app/admin/incident-thread.tsx`, reused by alert and error detail pages, using existing Button, Textarea, Badge, skeleton, error and dialog primitives. Keep page-specific health and occurrence content in the page files.

- Show cycle boundaries, timestamps, authors, resolution operator/time and the selected comment's **Resolved incident** badge. On a reopened thread show previous cycles without overwriting those badges. Load comments for the selected cycle on demand; show empty, pending, success, failure and older-page states.
- Composer ends with right-aligned **Post comment**. Enter inserts a newline. Disable duplicates while submitting; preserve the draft on failure, clear on success, toast and reload only affected resources. No localStorage drafts or live polling; every read remains deliberate/audited.
- Each eligible current-cycle comment has an outline **Mark as resolved** button at its bottom right for operators. Use `ConfirmationDialog` to confirm the chosen resolution; alert health/staleness restrictions have a visible explanation. Viewer comments cannot smuggle a resolve flag.
- Redact is an operator-only destructive menu item using `AdminActionDialog` with reason/step-up. Keep focus, keyboard access, announcements, mobile wrapping and dark/reduced-motion styles. A 409 preserves the draft/dialog, explains the conflict and reloads current state; never retries a resolution against a new cycle automatically.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A viewer gains incident-control powers | New viewer writes are only comment creation, with an explicit role and limit. Resolve/acknowledge remain operator housekeeping; redaction remains step-up Action. Update role documentation and AGENTS.md to describe this intentional expansion. |
| Cross-thread message selection or stale resolution | Parent-scoped lookups, cycle membership FK, row locks and submitted cycle/revision or occurrence preconditions. |
| Stored XSS, accidental secrets or unbounded text | Plain escaped text, validation/length limit, audited redaction, no body in events/notifications/logs, per-admin rate limit and per-cycle quota. |
| Account deletion makes an admin resolution look automatic | Explicit resolution source survives nullable person FKs. Legacy error resolutions with unknown attribution are labelled unknown, never guessed. |
| Pruning destroys the discussion immediately | Change pruning and its indexes/tests with this feature; use the explicit retention policy above. |

## Tests and checks

- Real PostgreSQL: multiple admins post to both resources, reload and paginate tied timestamps; quotas and limits; cycle/message/parent isolation; empty/invalid/overlong bodies; Unicode counts; read audit events once; no body in audit payloads or logs.
- Viewers post but cannot resolve/redact; operators resolve without step-up; redaction requires step-up/reason. Wrong key/app session/CSRF/Origin and revoked admin remain refused. Register the approved housekeeping routes explicitly, without loosening route guards.
- Resolve using another admin's eligible comment; reject another cycle's, redacted, unknown or already-used-in-a-closed-cycle selection. Resolve/resolve, resolve/redact, comment/prune and recurrence/prune races have deterministic results and atomic events.
- Buffered error batches older/equal/newer than resolution, both concurrent orderings, multiple servers flushing the same fingerprint, stale `lastSeenAt`, unchanged aggregate counts and preserved old discussions.
- Attribution survives revocation, deletion is not legacy auto-resolution, redaction leaves no body copy, exports stay isolated, retention preserves active discussed groups and removes expired resolved history.
- Vitest/Playwright: team discussion, resolution badge, recurrence with older decisions, role controls, reload persistence, conflict/error/draft handling, keyboard and narrow-screen layout. No trivial styling tests.
- Implementation checks: `make test`, `make check`, `make test-e2e E2E_PORT=auto`, `make build`; audit auth/role changes as required by the project guide.

## Decision record

The user approved the revised technical design on 2026-11-02, including the shared tables/`inc_` and `msg_` IDs, required existing comment for resolution, **2,000-character/1,000-comment** bounds, 60-comment/hour per-admin budget, discussed-error one-year retention extension and the routes/events specified above. No product decisions remain open. Follow [checks.md](../../../../docs/checks.md) when adding the approved stored resources; any additional unlisted named store verb or convention exception still requires agreement. Use CRUD controllers and existing naming rules, never loosen checks.

## Docs to update when implemented

- `docs/api/admin.md`, `docs/api/endpoints.md`, `docs/api/monitoring.md`: routes, viewer comments, cycles, actions, health vs occurrence resolution.
- `docs/api/database.md`, `docs/compliance.md`: both tables, personal data, references-only audit payloads, redaction, attribution, pruning and export/erasure limitations.
- `docs/api/events.md`, `docs/api/observability.md`, `docs/api/rate-limits.md`: events, timestamp-aware error upserts, limits/quotas and retained discussions.
- `docs/naming.md`, `docs/checks.md`, `AGENTS.md`: the approved viewer-write capability and new stored resources, while preserving every existing guard.
- `docs/ui/design.md`, `docs/ui/forms-and-actions.md`, `docs/ui/api.md`, `docs/api/testing.md`, `docs/ui/testing.md`: shared thread, comment actions, conflicts and tests.
- Plan indexes and the two companion plans; move this plan only when implemented.

## Not included

- Replies nested under comments, individual discussion-point resolution, attachments, Markdown, links, mentions, comment notifications or realtime chat.
- Editing bodies/history, silently deleting comments, changing rule thresholds, merging different error fingerprints or guaranteeing history beyond retention.
- A manual error reopen endpoint, an active-alert resolve override, or comments resolving related resources automatically.
