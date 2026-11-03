# Admin: alert recovery, persistent threads and incident cycles

Status: **implemented 2026-11-02**. Built as planned: a stable alert per signal with per-cycle recovery evidence, recovered-only resolution, immutable notice snapshots and signal-wide email and webhook caps. Differs: the health fields are mirrored on the alert and its cycle; a data-preserving conversion of existing alerts is not included (development and test databases are rebuilt). Docs updated: [monitoring](../../../../docs/api/monitoring.md), [admin](../../../../docs/api/admin.md), [events](../../../../docs/api/events.md), [mail](../../../../docs/api/mail.md), [deployment](../../../../docs/deployment.md).

Follow-up 2026-11-03: completed the omitted alert detail wiring: thread summaries in the detail response, the shared discussion and recovered-only comment resolution, local evidence expiry, scoped list/sidebar revalidation and browser regression coverage. Updated [monitoring](../../../../docs/api/monitoring.md), [admin](../../../../docs/api/admin.md), [API tests](../../../../docs/api/testing.md) and [UI tests](../../../../docs/ui/testing.md).

Builds on [alerts](alerts.md), [actions](actions.md) and [resolve-messages](resolve-messages.md). Build the discussion/cycle groundwork first, then ship this lifecycle and its comment-based resolution together. [Subscriptions](alert-subscriptions.md) follows this plan and applies to every notification kind defined here. Names follow [naming.md](../../../../docs/naming.md).

## Why

Signal recovery is evidence, not an operator's resolution decision. The evaluator must report recovery without closing an incident automatically. When the same signal fails again, the team needs the existing thread and its previous decisions, not a disconnected replacement alert. Reusing a parent requires explicit per-cycle state, notification bounds and stale-delivery handling; the old open/resolve/new-row assumptions cannot be retained.

## Confirmed product decisions

| Question | Decision confirmed 2026-11-02 |
| --- | --- |
| Who closes an alert? | Only an operator, using a current-cycle discussion comment as the resolution. All admins may discuss. |
| When is resolution permitted? | Only after fresh, confirmed signal recovery. Active, unknown, stale or still-confirming alerts cannot be resolved. |
| What happens on recurrence? | Reuse the same signal's parent `alt_` ID and thread; start a new cycle after a previous resolution. Retain earlier comments and decisions within retention. |
| What should recovery show? | Observed healthy duration, healthy-check count, recovery confirmation and last-check time. |
| What notifies? | Opening/escalation/reminder plus recovery, reopening and manual resolution; bounded lifecycle-only content, never comment text or identities. |

This supersedes active-alert resolution, silent manual closure and creating a fresh alert row for every post-resolution recurrence in earlier drafts.

## Lifecycle

```
no thread + signal bad for For       -> create parent + cycle 1; alert.opened
active + successful healthy reading  -> confirming recovery
healthy observed for recovery window -> recovered; alert.recovered; still unresolved
recovered + bad reading              -> active, same cycle; alert.reopened
recovered + operator selects comment -> resolved cycle; admin.alert.resolved + alert.resolved
resolved + signal bad for For        -> same parent, next cycle; alert.reopened
unavailable/stale reading            -> unknown, cannot resolve; break healthy evidence
```

- `For` controls detecting a new incident after resolution. A relapse of an unresolved recovered incident becomes active immediately; it never creates a second open cycle. Notification cooldown controls delivery, not detection or whether the dashboard reopens.
- No automatic recovered-to-resolved transition exists. A new incident resets acknowledgement, per-cycle peak/severity, recovery evidence and reminder timing; preserve an unexpired signal silence. Within an unresolved cycle, a confirmed-recovery relapse also resets acknowledgement and starts reminder timing again. History records who previously acknowledged it.
- Track **current signal level** separately from the cycle's **highest alert severity**. The current value/level and cycle peak are shown distinctly. A warning after a previous critical cycle starts at warning; do not inherit a prior cycle's critical level.
- A bad reading while an unresolved cycle is confirming recovery resets that run but is not a reopening event (there was no confirmed recovery). A recovered-to-unknown transition is not a failure/reopening claim. If it later reads bad, reactivate the unresolved cycle and record the recovery relapse once.
- Signal identity is the stable rule signal name, not an affected account or sample ID. Distinct causes under one aggregate signal share that signal's thread. Error groups are independently keyed by fingerprint; resolving one never closes its aggregate alert.

## Recovery evidence and freshness

Approved defaults, with all evaluation times supplied by the database clock and stored durably:

- Fields on the current cycle/projection: `healthy_since`, `healthy_checks`, `recovered_at`, `last_checked_at`, `last_successful_at`, `current_level`, `recovery_interrupted` and a monotonic lifecycle `revision`. Keep earlier confirmed recovery/relapse facts in immutable events; current evidence may reset without erasing the discussion or cycle history.
- “Healthy” means `rule.Level(reading.Level) == ok`: **below this alert's trigger**, not necessarily the underlying signal's raw `ok`. Show both raw and effective levels, and label duration **Below alert threshold since …**, not “No issue”, when these differ. A disabled/unavailable data source is unknown, not a healthy zero; metric-backed rules with metrics storage off cannot establish recovery. An intentional valid below-minimum-sample result follows the signal's documented semantics.
- Minute rules require at least 10 minutes and 11 distinct healthy minute evaluations. A successful evaluation counts at most once per scheduled minute/cycle, even if two leaders run. Hourly rules retain the existing behaviour: the **first successful healthy hourly reading** confirms recovery, with one healthy check; do not claim an hour of observed health from one sample.
- An unavailable reading breaks the healthy run: reset its start/count and invalidate current recovery, preserving previous recovery in history. A gap of more than **2 minutes** for minute rules or **65 minutes** for hourly rules has the same effect. A late healthy sample starts a new run; persisted timestamps alone cannot bridge an outage. Two leaders' duplicate/hourly early reads cannot inflate counts; store the last counted evaluation bucket durably.
- A leader change retains valid evidence within those gap bounds. Persist hourly scheduling as well as check counts so a failover cannot manufacture extra hourly evidence. Do not hold write locks during signal queries.
- Unknown is also derived at read/resolve time from freshness, so an evaluator that stopped cannot leave a green, resolvable screen indefinitely. UI displays last successful check, observed-through time, cadence and stale/unknown status. Durations stop at the last successful observation rather than ticking through unobserved downtime.
- Resolve rechecks current cycle, lifecycle revision, effective healthy state and freshness under the write lock, using database time. A fresh page does not bypass a concurrent failure; an unseen new evaluation that changed the submitted revision is 409. A concurrent network/query timeout cannot count as successful evidence.

## Data and transactions

- `alerts` becomes the stable parent per signal with a **unique signal constraint**, replacing the existing open-only unique index. `incident_cycles` and `incident_messages` come from resolve-messages; the current cycle is the highest indexed cycle number. Store opened/resolved times, acknowledgement, severity/peak, recovery fields and notification disposition per cycle; parent fields needed by existing list queries are a transactionally maintained projection.
- Add a parent-level transition sequence and bounded notification state (last per-kind reservation, rolling email reservations, reminder anchor). Do not retain an ever-growing JSON `mail_times` array on a forever-reused row: discard entries outside the maximum budget window while atomically reserving notices. Older events remain the audit evidence.
- Alert changes take `database.LockAlertWrites` **before** parent row locks, then cycle/message locks in a consistent order. Resolve, evaluator transitions, silence, acknowledgement and notification reservations follow that order. Ordinary message/redaction operations lock parent then cycle/message and do not later acquire the alert advisory lock. Recheck cycle/revision/state inside the transaction; a snapshot read before querying signals cannot overwrite a concurrent resolution. Conflicting samples from stale evaluations must not regress timestamps or state.
- Create/advance a cycle, update parent projection, reserve delivery eligibility and record its event/outbox in one transaction. Unique constraints backstop two leaders. Stale mutations return 409; evaluator work derived from a superseded cycle is skipped and reevaluated next turn, never automatically applied to the replacement cycle.
- No extra table for alert health. Update create migrations/factories/models/seeders, `store.DataTables` for the shared tables, and compliance data-map descriptions. Preserve Bun-only queries and bounded evaluation budgets.

### Legacy and rollout

- Fresh development databases use the new schema after the authorised local rebuild, per project migration policy. Do not run that rebuild as part of editing these plans.
- If any deployment has data to preserve, stop using squashed rebuilds: first design an append-only conversion. Group historical alert rows by signal, select the existing open row (otherwise latest row) as canonical parent, retain every old incident as a cycle and preserve old-ID aliases/links. Do not just add `UNIQUE(signal)` over existing duplicates or delete the history. This preservation migration needs its own confirmed implementation detail before such a release.
- Mark converted evaluator resolutions `automatic_legacy`; missing human attribution is `unknown_legacy`, never inferred solely from a null person FK. Old events and listener names remain readable; no historical event is renamed.
- Drain/pause the old evaluator/workers during the lifecycle cutover. Mixed old/new evaluator versions are not supported: an old binary could auto-resolve a recovered incident. Document release ordering and rollback compatibility, and refuse an incompatible runtime/schema combination rather than silently running it.

## Events, payloads and delivery

| Event | Meaning |
| --- | --- |
| `alert.opened` | First cycle of a new thread |
| `alert.reopened` | Reactivation of an unresolved recovered cycle (`cause: relapse`) or a new cycle after resolution (`cause: recurrence`) |
| `alert.recovered` | A new confirmed healthy run; awaiting operator review |
| `admin.alert.resolved` | Operator decision; actor plus parent/cycle/message references, **no comment body** |
| `alert.resolved` | Sanitised manual-resolution lifecycle event (`source: admin`); old queued payloads without source retain their legacy automatic meaning |

Keep escalation, reminder and digest facts. System lifecycle events carry parent/cycle public IDs, parent transition sequence, transition time, effective/current and cycle severity as needed, value/unit, recovery evidence and immutable display timestamps, plus reserved mail/webhook eligibility. Their external projection contains fixed descriptions, signal names and numbers only. `alert.resolved` says **Resolved after recovery**, not that the operator changed the signal. No actor, resolution-message ID/body, account name/email, IP, sample ID, URL or admin key leaves in notices.

### Flood bounds and recipient semantics

- Apply limits to **every** notification kind across **all cycles of the signal**, not just new parent inserts. Quiet/cooldown rules cannot be bypassed by reopening the same row. Keep 3 email reservations/signal/hour, 5 system-wide/10 minutes and 4-hour critical reminders; openings/reopenings beyond the global burst may join the existing digest. Recovery/resolution beyond caps are not emailed and are visibly recorded as suppressed, not promised as guaranteed delivery.
- For webhooks, retain opening cooldown and silence and enforce the approved cap of **12 transition posts per signal/hour**, including recoveries/reopenings/resolutions/escalations/reminders. Dropped notices remain audit events. An eligible posted notice includes the current state/sequence so a receiver can reconcile even if intermediate transitions were capped. No unbounded backlog of suppressed transitions.
- One recovery event per confirmed healthy run, one resolution per cycle, one relapse event per recovery becoming bad. A quiet cycle stays quiet; a manual resolution is eligible only when that cycle's opening/reopening was externally eligible, subject to caps and silence. Do not introduce guaranteed paired delivery or per-recipient “received opening” tracking in this build.
- Recovery (and unknown after recovery) stops reminders until another confirmed bad reading reactivates the cycle. Acknowledgement resets on reactivation as above; remind no earlier than four hours from reactivation or the most recent mail reservation, whichever is later. Silence remains effective until its expiry, including across new cycles, and is rechecked immediately before sending.
- Subscriptions, when installed, evaluate current admin membership, email and preferences **at delivery time**, across every mail kind and each digest item. Recovery/resolution need not target exactly the previous recipients; they are self-contained lifecycle summaries, not receipts. Preference changes do not replay history.

### Queued, delayed and out-of-order notices

- Never build a recovery body from the row's current value or vice versa. Use the immutable event snapshot; retries of the same delivery retain the same body (signature timestamp may change). Include `occurredAt`, cycle and sequence in JSON and timestamped event wording in Slack/email.
- Freeze titles, meaning, units and all external rendering fields at event creation too, not by looking up possibly changed rule definitions on a retry. Persist the sanitised JSON and Slack body strings in the event snapshot, bounded and validated by the alert-content safety tests. A code/template update must not change a webhook retry's bytes under the same delivery ID. Never persist a body containing discussion or recipient data.
- Before a send, validate the parent/cycle/sequence against current durable state. A state-changing notice superseded by a later lifecycle transition is skipped as stale, even within the same cycle; a resolved row does **not** blanket-suppress its final resolution notice. Stale digest entries are removed individually. Legacy queued `alert.resolved` events retain their old interpretation and are subject to the same supersession check where mappable.
- The worker is at-least-once and unordered. Receivers must deduplicate by `X-Lewcontext-Delivery` and ignore lower parent sequences. The pre-send check cannot retract an HTTP/SMTP request already in flight; event-time wording and receiver sequence handling are essential, not an ordering guarantee. Counter fixed skip reasons and retain existing retry/error reporting for real send failures.
- Update existing mail templates, strict subject allowlist, JSON/Slack formatters and listeners for every new kind. Keep old listener registrations to drain old payloads. Webhook wire shape is versioned/documented; coordinate consumers before cutover, including the changed meaning/source of new resolution notices.

## API and UI

- Existing list/detail responses expose current cycle, `state` (`active`, `confirming`, `recovered`, `unknown`, `resolved`), raw/effective current level, cycle severity/peak, health evidence, lifecycle revision and resolution reference/person/source. List payloads do not contain discussion bodies.
- `state=open` continues to mean unresolved, including recovered/unknown, and the sidebar counts those parents once. Sort active/unknown before recovered and then by severity/current-cycle opening, so old recovered critical incidents do not bury new active warnings. Add state filters only if separately confirmed; existing resolved/all lists use stable current-cycle opening plus parent ID cursor order.
- The page shows current health, last-check/cadence, cycle peak/current value, acknowledgement/silence and the shared thread. Previous cycles keep their resolution comments/markers. **Mark as resolved** is available only for an eligible current-cycle comment while recovered and fresh; otherwise explain why. The server enforces the condition independently.
- Page/list/sidebar mutation revalidation is explicit and scoped. No module cache of user data, key in SWR keys, background polling or auto-retry of a conflicted resolution.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| False recovery during outages or disabled metric storage | Unknown breaks evidence; cadence bounds, source availability, persisted evaluation buckets and resolve-time freshness checks. |
| Old acknowledgement/severity hiding a new failure | Per-cycle state and explicit reset on reactivation, preserving history rather than inherited control flags. |
| Notification storms through one reused row | Signal-wide budgets include every transition; quiet/silence/digest decisions are durable and transactional. |
| Old all-clear arriving after a new failure | Immutable snapshots, durable sequence checks, receiver ordering contract and explicit in-flight limitation. |
| Operator hides an active incident | Backend refuses active/unknown/stale resolution; discussion alone changes no lifecycle state. |
| Thread retention grows without bounds | Resolved-parent retention from resolve-messages; bounded mail state, indexed/paged cycles, comments quota/rate limit. Unresolved history intentionally remains retained. |

## Tests and checks

- Below/at 10 minutes with distinct checks; duplicate leader ticks; hourly first-healthy semantics; failover within/beyond freshness; unavailable/gapped/metrics-disabled readings; raw warn mapped below trigger; observed-through duration never advances through downtime.
- Initial open, unresolved recovery/relapse, resolution using a comment, resolved recurrence with same parent/new cycle, reset acknowledgement and severity, preserved unexpired silence and earlier comments. A later first occurrence after pruning is explicitly a new thread.
- Resolve refuses active/confirming/unknown/stale, wrong cycle/revision, redacted comment and concurrent bad reading; two operators resolve once; evaluator/resolve/acknowledge/silence races obey lock order and preserve atomic events.
- Flood tests spanning many cycles and recovery runs: email/webhook caps, bounded mail-state storage, cooldown, digest filtering, quiet cycles, silence and four-hour reminder reset. All new notice kinds remain subject to subscriptions.
- Queued recovery delivered after relapse, resolution after newer recurrence, opening digest after recovery, retries/out-of-order sends, legacy payloads/listeners and stable retry bodies. In-flight limitations are documented rather than asserted away.
- Content tests cover all kinds, sources and both webhook formats: no message body/author/resolver/key/link/address/IP/sample ID; strict subjects/templates allow the new words. External IDs are alert/cycle/event identity only.
- List/order/cursor, sidebar unresolved count, stale evidence and historical resolution markers tested through API and UI; real PostgreSQL, fake clocks and receivers; Playwright full recovery/discussion/recurrence flow.
- Implementation validation: `make test`, `make check`, `make test-e2e E2E_PORT=auto`, `make build`.

## Decision record

The user approved this revised design and defaults on 2026-11-02: 2-minute/65-minute freshness gaps, 11 distinct checks spanning 10 minutes for minute recovery, first-healthy scheduled hourly recovery, relapse acknowledgement reset, per-cycle/highest vs current severity, unique parent per signal, signal-wide notification reservations including the 12/hour webhook cap, best-effort lifecycle summaries and coordinated cutover. No product decisions remain open. A data-preserving migration, if needed rather than the current local squashed development schema, still requires the separately confirmed conversion detail described above.

## Docs to update when implemented

- `docs/api/monitoring.md`, `docs/api/admin.md`, `docs/api/endpoints.md`: lifecycle, freshness, actions, list sorting, counts and cycle history.
- `docs/api/events.md`, `docs/api/mail.md`, `docs/deployment.md`: payload/source/version, all notice kinds, caps, stale skips, consumer contract and cutover.
- `docs/api/database.md`, `docs/compliance.md`, `docs/api/observability.md`: stable parent/cycles, bounded mail state, retention, source availability and fixed skip counters.
- `docs/ui/forms-and-actions.md`, `docs/api/testing.md`, `docs/ui/testing.md`, `AGENTS.md`: recovered-only comment resolution and tests. Update plan indexes/companions without rewriting implemented historical plans.

## Not included

- Automatic resolution, active-alert override, suppressing a signal by resolving it, editable thresholds or a promise that recovery proves a security incident was remediated.
- Creating separate threads for each sample/account within an aggregate signal, merging distinct signals, guaranteed external delivery/order or indefinite history after pruning.
