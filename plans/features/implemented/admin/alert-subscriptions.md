# Admin: alert catalog, email subscriptions and delivery settings

Status: **implemented 2026-11-02**. Built as planned: the rule catalog, personal per-signal preferences narrowing the global setting, and one shared webhook and heartbeat with sealed, versioned runtime settings, 15-minute dual-signature key rotation and cancellation of unsent old-destination notices. Docs updated: [monitoring](../../../../docs/api/monitoring.md), [admin](../../../../docs/api/admin.md), [events](../../../../docs/api/events.md), [database](../../../../docs/api/database.md), [compliance](../../../../docs/compliance.md), [deployment](../../../../docs/deployment.md), [AGENTS.md](../../../../AGENTS.md).

Builds on [alerts](alerts.md), [monitoring](monitoring.md), [resolve-messages](resolve-messages.md) and [alert-recovery](alert-recovery.md). Build the thread/cycle groundwork and recovery lifecycle first, then this plan. Names follow [naming.md](../../../../docs/naming.md); new names use at most three event segments and ordinary resource controllers.

## Why

Admins need to know what the evaluator watches and narrow which signals reach their own inbox. Operators also need to change the shared delivery destinations without a redeploy. These are different powers: an inbox preference changes one admin's mail; a webhook or heartbeat changes infrastructure for the whole team.

The previous draft left schema, routes, event names and secret handling contradictory. This revision chooses a single contract, integrates recovery/reopening/manual resolution, and specifies runtime activation and queued-delivery behaviour.

## Confirmed product decisions

Confirmed on 2026-11-02: include a catalog, per-admin email preferences, **one shared webhook and one shared heartbeat**, explicit inherit/enabled/disabled states and runtime changes across servers. Lifecycle notices include recovery, reopening and manual resolution, never discussion content or identities. Additional webhook destinations are deferred.

The follow-up review also confirmed the technical design below, including narrowing-only preferences, cancellation of old-destination queued work, runtime snapshots, the webhook cap and signing-key overlap. These are approved decisions rather than alternatives for an implementer to choose.

## Catalog and personal email preferences

### Surface and catalog contract

- Add **Catalog** as a query-state view of the existing `/admin/:key/alerts` page (`?view=catalog`); existing Open/Resolved/All state views remain. No web `alerts/catalog` page or route-order workaround is needed. Use Back/Forward-compatible URL state and existing tabs/highlight primitives.
- API catalog is its own resource: `GET /api/admin/alert-rules`, controller `AlertRules.Index`. It returns each `monitor.AlertRules` entry exactly once, joined to the code-defined signal: name, title, meaning, page, unit, signal warn/critical thresholds and minimum sample/window, rule `From`/`Cap`, effective alerting levels, `For`, cadence and recovery semantics. A raw critical signal capped to warning must not be advertised as a critical alert.
- Include the signed-in admin's explicit preference (`inherit`, `warn`, `critical`, `off`) and effective levels after the global setting and rule cap. Warn when an explicit selection is currently ineffective, e.g. critical-only on a warning-capped rule. No thresholds are editable.
- The catalog has personal preference data, so it is a recorded read: `admin.alert_rule.listed`, with actor only. This is not the unrecorded names-only `/api/admin/catalogue` endpoint.

### Storage and preference semantics

- New `admin_alert_subscriptions`: internal key; `admin_user_id` FK to `admins(user_id)` CASCADE; signal string; `min_level` CHECK `warn|critical|off`; unique `(admin_user_id, signal)`. No row means **inherit**. Store only explicit overrides, not a JSON blob. One create migration pair, own factory, `store.DataTables` entry and compliance data-map row.
- Keep `admins.alert_email` (`all|critical|none`) as the default and maximum scope. Per-signal choices only narrow it: `none` sends nothing; `critical` never sends warning mail even if an override is `warn`; `off` always excludes; missing override follows global. Evaluate against the event/cycle alert severity, not the underlying raw signal severity.
- New rules inherit by default. Removed-rule overrides remain inert and are returned as retired entries in the preference response so they can be reset; do not silently turn them into subscriptions to a different rule. Limit explicit rows per admin to 500, independent of the current rule count, under the admin row lock.
- `PATCH /api/admin/me` accepts existing `alertEmail` and/or `alertSubscriptions: [{signal, minLevel}]`; `minLevel: inherit` deletes that override. This is a bounded **patch**, not a whole-list replacement: omitted signals are unchanged. At most 100 entries/request; unique names, signal length at most 100, known current rules for new overrides, or an existing retired name only for deletion. Unknown/duplicate/invalid fields are 422. Reject an empty mutation; no-op changes record no event.
- Apply both global/pre-signal changes in one transaction under the admin row lock, recording one existing `admin.updated` with `changes.alertEmail` and/or `changes.alertSubscriptions` (changed signal/level pairs only, bounded by the request). Never allow changing someone else's subscriptions or email through this body. Admin revocation removes overrides; a later regrant gets the normal critical-only default.
- `GET /api/admin/me` keeps its session-probe role and does not return another admin's overrides. The catalog is the read surface for detailed subscriptions. Current own inbox address is displayed read-only using the locked-field primitive, not an arbitrary destination input; an account email change follows the normal verified email-change flow.

### Delivery-time recipient filtering

- All `mail.alert*` kinds use one shared predicate: opening, escalation, reminder, digest **per item**, recovery, reopening, manual resolution and pending legacy automatic resolution. Recheck current admin membership, current account email and current global/override settings when the listener runs. All actual mail still goes through `mailer.Send` and its block-list check.
- Do not snapshot recipient addresses in alert events, replay history on subscribing, notify about comments, or imply “only people who received the opening get the recovery”. These are self-contained lifecycle summaries; a changed preference can legitimately receive a recovery without having received the opening. Preserve the existing documented partial-email-failure behaviour, with no new exactly-once claim.
- Webhooks are independent of personal email settings. They receive every **eligible** lifecycle kind, not literally every recorded event: silence, quiet/cooldown, the recovery plan's webhook cap, stale checks and disabled destinations still apply. No per-signal webhook subscription controls in this build.

## Shared delivery settings

### Explicit configuration modes

Each kind (`webhook`, `heartbeat`) has one setting with these modes:

| Mode | Effective behaviour |
| --- | --- |
| `inherit` | Use the environment destination if configured; otherwise off. The API shows its source, never the URL. |
| `enabled` | Use the sealed dashboard URL (and webhook format/signing secret), ignoring the environment destination. |
| `disabled` | Explicitly off, even if an environment URL exists. Clearing a URL must not silently select inherit. |

The UI exposes these as explicit choices. Entering enabled requires a URL; an update may keep an existing enabled URL with a separate `keepExisting: true` flag, never a masked value roundtrip. Setting disabled or inherit removes unused dashboard secret material once no in-flight use needs it. Global settings belong to the installation and survive the creating admin's revocation; only the optional changed-by FK is SET NULL.

Retain startup validation for configured env values even while overridden; show an overridden-env warning on Configuration. `ALERT_ALLOW_PRIVATE_TARGETS` remains configuration-only. `ALERT_WEBHOOK_SECRET` applies only to inherited env delivery, never to a dashboard URL. Dashboard webhook format is explicit `json|slack`; env format stays the inherited default.

### Schema and effective destination identity

Approved tables:

| Table | Fields / retention |
| --- | --- |
| `alert_delivery_settings` | One row per kind (CHECKed unique kind); mode; monotonic settings revision; nullable active destination-version FK; updated time and changed-by FK to users SET NULL. Settings persist until changed. |
| `alert_destination_versions` | Internal key; generated `dst_` public ID; kind/source (`env|dashboard`); sealed URL; webhook format and sealed current/overlap signing secrets where relevant; created time. Routing/configuration snapshots are immutable; erasing an expired overlap key is the sole secret-retirement exception. Retain active versions and retired versions only while pending/in-flight deliveries reference them, then cleanup promptly; no year-long secret history. |

Version rows distinguish destinations even with a single webhook. Event payloads contain only the version's public ID, never a URL, host, secret or digest. A version change never retargets a pending delivery to a different receiver. Both tables need migration pairs, own factories, `store.DataTables` entries, FK/indexes and compliance rows. Do not cascade shared infrastructure away with an admin account.

Seal URL/secrets with `APP_KEY` using domain-separated keys and associated data binding kind and destination identity; do not repurpose the MFA owner's encryption context. A database dump without the key must not reveal a receiver token. `APP_KEY` rotation is still unsupported; changing/losing it makes these settings unusable and must fail closed, not silently fall back to env. Backups now contain encrypted destination data; document restore/key requirements.

### Runtime activation across servers

- Register alert webhook listeners on every server **regardless of whether a URL was set at startup**. Their handlers resolve version references from the database; disabled events do not acquire webhook outbox work. Register and retain legacy listener names too. `MAIL_DRIVER=off` still disables email, independently of webhooks.
- Each dashboard setting mutation validates, creates its sealed effective snapshot, updates the active version/revision and records the audit event in one transaction under the settings row lock. Concurrent editors submit expected revision; stale changes are 409. No process-local “last known good URL” fallback.
- All new events bind the current effective webhook version in their transition/outbox transaction. Workers read the active setting/version again immediately before network I/O; retired/disabled versions are skipped, **not** sent to the replacement. Skip is a completed no-send outcome with a fixed reason counter, not an SMTP/HTTP failure needing endless retries. Already in-flight requests cannot be recalled; document that boundary.
- Heartbeat leader resolves the active heartbeat version from the database after every successful evaluation, before each ping. A change is effective on the next turn without a restart. Settings read/decrypt/validation failures stop outbound delivery and report a scrubbed fixed-context error; no cached/env bypass. A skipped/failed evaluation still sends no heartbeat.
- In inherited mode the evaluator leader materialises env destinations as sealed versions before producing notifications, under the same settings lock. On startup/leadership change it compares the effective env configuration with the stored inherited snapshot and activates a new version if changed. Other workers consume that shared snapshot rather than each using their own env URL. Until initial materialisation, no destination is active. Require matching env configuration across servers; document that a misconfigured leader takeover can change inherited destinations, and show source/version activation time on Configuration without exposing values. Dashboard enabled/disabled always takes precedence under the lock.
- Mode/destination/format/signing-key changes activate a **new version** and invalidate pending work bound to the old one. Existing event bodies stay immutable; a new destination starts with future transitions, no implicit history replay. On cutover, drain legacy unversioned webhook deliveries under the old configuration before switching to runtime settings; do not guess a target for them. Retired snapshots remain only until outstanding worker leases finish or deliveries are completed/skipped.

### Outbound validation and confidentiality

Moving URLs from configuration into data intentionally changes the config-only invariant. Update AGENTS.md and monitoring/deployment documentation **with implementation**, not ahead of the code. A viewer never sets a network target.

- Share one lower-level URL validation helper between config and runtime settings without an import cycle (`config` must not import an alerts package that imports `httpapi`/`store`). Keep structural validation separate from the dial-time address guard; avoid duplicating or weakening either implementation.
- Validate on write and again before use: absolute host URL, at most 2,048 characters, production HTTPS, no userinfo/fragment/whitespace/controls, bounded ports and the existing host/address rules. DNS resolution at validation is not proof of a safe connection. Preserve connection-time public-unicast checks, DNS-rebinding defence, verified TLS, no environment proxy, no redirects, fixed timeouts and bounded ignored responses.
- API reads return **set/not-set, mode, source, revision, format and signed/overlap state only**. Do not return even a host: a receiver can embed secrets in subdomains. Operators replace a destination by entering it again; no reveal URL endpoint. Errors, audit events, config summaries and metrics never carry URL/host/secret/digest.
- Read or validation errors never quote input values. Do not send test network requests from validation handlers. Future “Send test” would need its own listener/outbox design, audit and bounds; it is not included here.

## Webhook signing and rotation

- For a dashboard webhook, generate a random 32-byte signing key, encoded for the receiver, and seal it. Return plaintext **only in the successful create/replace/rotate mutation response** to the stepped-up operator (`no-store`); normal reads never return it. UI keeps it only in component memory behind `Reveal` with shared Copy controls. No local/session storage, SWR response cache, event, log, screenshot or error-report state contains it. A lost response requires rotation, not a recovery endpoint.
- Header remains `X-Lewcontext-Signature: t=<unix>,v1=<HMAC-SHA256(timestamp.body)>`; `X-Lewcontext-Delivery: evt_…` is stable across retries. Versioned JSON body includes alert/cycle/sequence/event-time, as defined by alert recovery. Slack text carries event-time wording; neither format contains discussion text or people.
- Use the approved **15-minute rotation overlap** for dashboard keys: new-version posts carry **two `v1=` signatures** over the identical timestamp/raw body, one with the new key and one with the previous key until expiry. Receivers accept any matching signature using constant-time comparison and a recommended five-minute timestamp tolerance. Merely retaining an old key without sending its signature is not overlap. No third rotation during overlap (409); allow explicit immediate revocation of the old key through a stepped-up settings change.
- Overlap is only for rotation on the same destination/format; replacing the URL generates a new independent key with no overlap to the old receiver. Retired-version pending work is skipped as above. Worker signing uses its bound snapshot and expiry; retries keep the body/delivery ID, not an old signature timestamp. Document multi-signature receiver compatibility before enabling overlap; do not claim Slack verifies the application's HMAC.
- After overlap expiry, the cleanup erases the old sealed key from the active snapshot without changing its routing/version identity or cancelling its pending work. Even before cleanup, signing must refuse to use an expired overlap key. Explicit early revocation instead creates a new active version under the ordinary settings-change/cancellation contract.

## Routes, events and tiers

All routes use the admin key and separate admin session; mutations retain exact Origin, JSON and CSRF checks. Resource controller actions follow naming.md, in the existing admin area.

| Route | Controller / response | Guards and audit |
| --- | --- | --- |
| `GET /api/admin/alert-rules` | `AlertRules.Index`, 200 catalog/preferences | Any admin; read budget; `admin.alert_rule.listed` |
| `PATCH /api/admin/me` | `Me.Update`, 204 | `Role(viewer)`; new `alert-subscription:per-user`, 60/hour, applies to global/pre-signal preference writes; one `admin.updated` when changed |
| `GET /api/admin/alert-delivery-settings` | `AlertDeliverySettings.Index`, 200 both redacted summaries | Any admin; read budget; `admin.alert_delivery.viewed` |
| `PATCH /api/admin/alert-delivery-settings/{kind}` | `AlertDeliverySettings.Update`, 200 safe summary plus one-time new key only if generated | `Action`: operator, step-up, reason, `admin-action:per-user`; `admin.alert_delivery.updated` |
| `POST /api/admin/alert-delivery-settings/webhook/signing-keys` | `AlertSigningKeys.Store`, 201 safe summary and one-time key | `Action`; expected settings revision; `admin.alert_delivery.updated` with `operation: rotate_key` |

Mutation bodies include `revision` and `reason`. Kind is `webhook|heartbeat` (404 otherwise); validate allowed fields per mode/kind, URL on field `url`, format/key options on their own field; unknown fields 400, invalid values 422, stale revision/overlapping rotation 409. There is no secret in an API path or query.

`admin.alert_delivery.updated` contains actor, kind, operation, revision and safe changed-field names/modes, plus the required reason. It never holds before/after URLs, keys, hosts or ciphertext. Personal changes use existing `admin.updated`, never four-segment event names. New `alert_rule`/`alert_delivery` read resources and the settings action resource must be agreed and reflected in naming/docs/checks under the existing extension process; do not loosen checks to accept arbitrary names.

## UI

- **Subscribe** in the Alerts header opens a shared Dialog with type choices Email/Webhook/Heartbeat. Explain that email is personal and the other two are shared. Any admin can use Email; viewers may read the shared safe summaries but cannot edit them. Subscriptions and delivery data are separate API resources, each read audited once when opened, not prefetched behind the scenes.
- Email shows the locked own address, global level, per-signal choices and effective state; Save updates only changed fields. Catalog entries display rule-specific meaning/cadence/severity. Represent unavailable reads with a skeleton/error+Retry and preserve available data on refresh failure.
- Webhook/heartbeat editing is a dedicated `AdminActionDialog` after choosing the type, rather than nesting two independently submitting forms. Fields include mode and, for enabled, a replacement URL (never prefilled masked text); webhook adds format. Reason/step-up follow the existing flow. Show confirmation that changing/disabling discards pending old-destination notifications.
- **Example payload** / heartbeat ping explanation is collapsed initially, uses fixed non-secret examples, and keyboard-accessible disclosure behaviour through existing approved primitives. Webhook examples include recovery, reopening, manual resolution, source, sequence, signature verification and duplicates/out-of-order expectations. Never use the real URL or key in examples.
- Rotation is an operator-only action with clear overlap/old-key-revocation explanation and a one-time success reveal. Pending disables duplicate saves/rotations, failures stay in the dialog, 409 reloads safe state without auto-overwriting, and success announces activation without implying delivery was verified. No automatic polling or module-level user cache.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Viewer redirects global notices | Own-inbox Role write is separate from global Action routes; fresh MFA/reason required for every destination/key change. |
| SSRF or URL-token disclosure | Sealed storage, no URL/host reads, shared structural validation plus connect-time public-address guard, bounded outbound client and scrubbed errors. |
| Disabled webhook unexpectedly resumes via env | Explicit inherit/enabled/disabled; decrypt/read failure is not inheritance. |
| Several servers use different targets or settings never activate | Durable active snapshots read at use; static listener registration independent of startup URL; leader-owned inherited snapshot. |
| Old pending incident sent to replacement receiver | Immutable destination-version binding, retired-version cancellation, no implicit replay and coordinated legacy cutover. |
| Preference changes or recurrence bypass rate limits | All mail kinds/digest items use one predicate; signal-wide lifecycle reservations/caps remain owned by recovery. |
| Key overlap is claimed but ineffective | Dual raw-body HMAC signatures, explicit expiry/rotation conflict and receiver documentation/tests. |

## Tests and checks

- Catalog exactly matches rules, including raw thresholds, effective From/Cap levels, hourly cadence, recovery semantics and personal effective preferences. One recorded read; no static/dynamic route ambiguity.
- No override preserves existing behaviour. Test global none/critical/all × inherit/warn/critical/off × all lifecycle/legacy kinds; digest item filtering, removed/new rules, current recipient address, revocation/regrant, block-list handling and partial mail failure.
- Own preferences only; atomic bounded patches, duplicate/unknown/retired signal validation, no-op audit behaviour, quota and 429/Retry-After with the new limit. Viewer cannot mutate global settings/keys even if stepped up; all guards, rate limits and required reasons hold.
- All mode/env combinations, explicit disable with an env URL, source/format reporting, initial no-env activation without restarting, two-server updates, stale revision races, leader takeover, unreadable/sealed-data failure and no fallback. In-flight requests are documented as non-retractable.
- Queue before/after destination changes, disabled/retired version skip, no retargeting/replay, legacy unversioned drain, retained snapshot cleanup after worker leases, stable retry body/delivery ID and source lifecycle filtering from recovery.
- URL validation/write/use parity, DNS rebinding, private addresses and allow-private config, HTTP/HTTPS, credentials/fragments, proxies/redirects/timeouts/body bounds. Responses, summaries, events and logs contain none of the entered URL, subdomain token or keys. Handler validation sends no outbound request.
- One-time key appears only in the authorised mutation response, never normal reads/cache/events; bound encryption contexts resist substitution; correct HMAC, dual-signature overlap expiry, rotation conflict, immediate old-key revocation, lost-response rotation and no-overlap URL replacement.
- Factories/data map/known tables and retention; Playwright catalog/subscription/runtime-setting flows, viewer/operator controls, pending/error/conflict states, one-time reveal and accessible dialogs/disclosures. Use real PostgreSQL and local HTTP receivers for meaningful persistence/delivery tests.
- Implementation checks: `make test`, `make check`, `make test-e2e E2E_PORT=auto`, `make build`, plus auth/security dependency audits as required.

## Decision record

The user approved the revised plan on 2026-11-02: one-webhook/one-heartbeat dashboard scope; global preference as narrowing ceiling; inherit-by-default table/patch semantics and bounds; route/event names; shared version snapshots with **cancellation** of unsent retired-destination work and no retargeting; no host disclosure; one-time key response; 15-minute dual-signature overlap; and leader-materialised env fallback. No product decisions remain open. Changes to these decisions require updating and reconfirming the plan, not silently selecting an old draft's alternative.

## Docs to update when implemented

- `docs/api/admin.md`, `docs/api/endpoints.md`, `docs/api/monitoring.md`: catalog, personal/global controls, routes, modes, runtime activation and safe Configuration summaries.
- `docs/deployment.md`, `.env.example`: inheritance/migration, consistent server configuration, legacy delivery drain, receiver signature/order contract, key loss and runtime errors. Never commit a real destination.
- `docs/api/events.md`, `docs/api/mail.md`, `docs/api/rate-limits.md`: three-segment events, all-kind filtering, limits and skipped deliveries.
- `docs/api/database.md`, `docs/compliance.md`: three tables, factories/registries, role/author FKs, sealed destination/secret retention, backups and processors.
- `docs/ui/forms-and-actions.md`, `docs/ui/design.md`, `docs/ui/api.md`, `docs/api/testing.md`, `docs/ui/testing.md`: dialogs, one-time reveal, state/conflict handling and tests.
- `docs/naming.md`, `docs/checks.md`, `AGENTS.md`: approved resources and the deliberate replacement of the config-only URL invariant with validated sealed runtime settings. Preserve existing checks/guards and update plan indexes/companions.

## Not included

- Multiple webhook destinations, customer/workspace subscriptions, per-webhook signal filtering, editable rules, PagerDuty/SMS integrations, arbitrary email destinations, comment notifications or replay/test-send endpoints.
- URL/secret reveal reads, generic settings framework, APP_KEY rotation, exactly-once delivery or guaranteed network cancellation after a request has started.
