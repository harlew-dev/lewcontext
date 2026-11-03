# API keys and the v1 project API

Workspace settings → **API Keys** manages shared integrations. Account & security → **API Keys** manages your personal scripts. Each key belongs to **exactly one workspace**. API-key management requires an app session; an API key cannot manage itself or any other key.

## Ownership and permissions

| Kind      | Authority                                                                                                                         | Management                                                                                                                            |
| --------- | --------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| Workspace | Saved project/action grants, independent of its creator's account or role                                                         | Workspace owners and admins create/edit/revoke; creator departure, suspension and deletion do not end it                              |
| Personal  | Saved grants intersected with the owner's live workspace membership, role, suspension and required email-verification eligibility | Its owner creates/edits/revokes from account settings; workspace owners/admins can inspect metadata and revoke, never edit its grants |

Read expands to `project.index` and `project.show`; Create, Update and Delete are `project.store`, `.update` and `.destroy`. Read is the default. Personal saved scopes cannot exceed the current role at creation/edit. Later demotion removes effective actions immediately; promotion restores only saved actions. Unknown kinds, scopes, roles and access modes fail closed.

**All projects** includes current and future projects in the bound workspace, never another workspace or future API resource. **Selected projects** allows 1–100 distinct accessible projects on creation/edit and cannot include Create. Queries apply the selection before search, counts, sort and pagination, and again to writes. Deleting the final selected project leaves zero accessible projects, never All projects.

## Issuance, expiry and recovery

Creation or broader access requires fresh current password without MFA, or an authenticator/recovery code with MFA. The locked current account chooses the factor. Renaming, narrowing, shortening expiry and revoking require the normal live session/CSRF only. Management checks the initiating session and current membership/permission again inside its transaction. In the UI, MFA proof opens in a nested modal after submitting the filled key form, with authenticator/recovery support and cancellation back to the form. The code is submitted with creation/broadening and checked transactionally, never as a standalone verification. Password proof for accounts without MFA remains in the form.

Expiry is explicit: the UI defaults to 90 days, offers finite durations within 1 hour–365 days of creation and **Never** (`expiresAt: null`). Omitted creation expiry is rejected. Existing finite expiry cannot be extended or changed to Never. Never may be shortened to a finite date. Ended credentials cannot be edited/reactivated.

- Suspension permanently revokes every personal key in its transaction; unsuspension never revives one.
- Leaving/removal permanently revokes personal keys in that workspace; rejoining never revives them.
- Password changes/resets, operator MFA resets, ordinary MFA changes, email changes and browser sign-out preserve keys. API calls never prompt for MFA. Required verification temporarily denies personal-key use with generic 401, without revocation.
- Account deletion cascades personal keys and nulls workspace-key creator references. Workspace deletion cascades every key, selection and replay record.
- Revocation clears the digest immediately. There is no positive credential cache. An admitted read may finish; writes reauthorize inside their mutation transaction.

At most **100 live keys/workspace**, both kinds combined, and **50 live personal keys/user**, across workspaces. Live includes unrevoked Never credentials. Rotation below quota is **create replacement → deploy replacement → revoke old**. At capacity, explicitly free a slot first; revoking the old integration's key can cause downtime. There is no replacement slot, automatic revocation or secret-recovery endpoint. A lost issuance response requires inspecting/revoking that record and creating a new key.

## Credential and transport

Credentials are `lc_wk_<apk_public_id>_<secret>` or `lc_pk_<apk_public_id>_<secret>`: PostgreSQL-generated `apk_` plus 14 base62 characters and 32 random secret bytes encoded as 43 canonical base64url characters. Store only SHA-256 of the **complete credential**, compared in constant time. The public ID/kind identifies a key without revealing it.

Send exactly one `Authorization: Bearer <credential>` header over HTTPS in production. No URL/body/cookie authentication and no `X-API-Key` alias. Browser/admin cookies have no authority on v1. Keys authenticate neither browser/admin endpoints, page gates nor the separate metrics listener. `/api/v1` is selected before the browser security branch, preserving no-store, headers, request IDs, timeout, panic recovery, client-address resolution and IP blocks. Machine mutations require JSON but not Origin/CSRF. Browser mutations retain exact Origin/JSON/CSRF even with a bearer header. CORS remains disabled; machine traffic is never challenged with CAPTCHA.

The plaintext appears only in the successful creation response: `201 {key: <metadata>, secret: <credential>}`, `Cache-Control: no-store`. The web app holds it in the creation dialog's component state only, behind Reveal with Copy that works while masked. Done/navigation/workspace change/sign-out discard it. Closing early warns about loss but does not revoke the key. No usable secret belongs in screenshots, toasts, SWR, browser storage, events, errors or exports.

```sh
curl 'https://app.example.com/api/v1/workspaces/wsp_00000000000000/projects' \
  -H "Authorization: Bearer $LEWCONTEXT_API_KEY"
```

## Public routes

| Method | Path suffix after `/api/v1/workspaces/{workspace}` | Scope                         | Success                      |
| ------ | -------------------------------------------------- | ----------------------------- | ---------------------------- |
| GET    | `/projects`                                        | `project.index`               | 200 numbered `Page<Project>` |
| POST   | `/projects`                                        | `project.store`, All projects | 201 project                  |
| GET    | `/projects/{project}`                              | `project.show`                | 200 project                  |
| PATCH  | `/projects/{project}`                              | `project.update`              | 200 project                  |
| DELETE | `/projects/{project}`                              | `project.destroy`             | 204                          |

Project JSON: `id`, `name`, `description`, `status`, `color`, `createdAt`, `updatedAt`. POST/PATCH accept only the four editable fields; PATCH replaces their entire editable representation. Names are trimmed, 1–100 characters; descriptions trimmed, at most 500, allowing ordinary line breaks/tabs; both use `input.ValidText`. Status: `Planned`, `In progress`, `Completed`; color: `violet`, `blue`, `amber`, `emerald`, `rose`. The existing 1,000-project quota applies.

Lists take `page`, `perPage`, `q`, `status`, `sort` with the bounds, literal-wildcard search and deterministic orders in [pagination.md](pagination.md). Unknown/repeated query parameters are 400. Separate explicit v1 DTOs prevent accidental browser-response changes. Additive optional fields are compatible; removed/renamed fields, required new inputs or changed semantics need a new major version or an explicit deprecation/migration policy.

401 is generic invalid/ended/ineligible credential, with `WWW-Authenticate: Bearer`, no redirect/cookie changes. Wrong workspace and missing/malformed/unselected projects are identical 404. In-scope credentials lacking an action receive 403. Existing validation/JSON/quota/rate-limit/generic 500 conventions apply, including server-generated `X-Request-ID` and `requestId` in 500s. Unknown versioned API paths remain JSON 404s.

## Optional duplicate-safe project creation

POST may supply one `Idempotency-Key`: 16–128 ASCII letters/digits or `.`, `_`, `~`, `-`, generated randomly by the caller. Missing means ordinary creation with **no duplicate-safe retry guarantee**. Malformed/repeated supplied headers are 400. Never log the header or put it in a URL.

Within **24 hours**, the same credential/workspace/operation/value with the same canonical validated input returns the original 201 snapshot without another project, quota slot, create allowance or event. Different input, or a deleted original project, yields 409. A rollback leaves no reservation. Every retry reauthenticates and rechecks current credential/owner authority and spends ordinary request budgets; it cannot retrieve formerly accessible data after revocation/demotion. After expiry, reuse may create another project.

`api_idempotency_requests` holds digests of the value/input, project public ID and a bounded explicit response snapshot, not the raw header. Project reference is a public-ID snapshot, not an FK: deletion leaves replay state so it can return conflict. The reservation, allowance, project and event share a transaction/connection. At **5,000 retained rows/workspace**, new idempotent creates get 503 + `Retry-After: 60`; valid replays still work. Cleanup is bounded; expiry prevents replay even before cleanup. Waiting is bounded by the ordinary request deadline; retry with the **same** header after uncertainty.

## Browser management routes

Both collections have Index/Store, and PATCH/DELETE on `/{key}`. No per-record GET or secret-recovery read exists.

| Collection                             | Reads / writes                                                                                                                           |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `/api/workspaces/{workspace}/api-keys` | `workspace.api_key.index/store/update/destroy`, owner/admin. Store/update workspace kind only; list/revoke either kind in this workspace |
| `/api/auth/api-keys`                   | Session, own personal keys only. Create also takes `workspace`, checked against live membership                                          |

Create: `{name, projectAccess, projects, scopes, expiresAt, password? or code?}` plus workspace for personal creation. Update replaces editable configuration without accepting ownership/kind/workspace changes. All requires empty projects; Selected requires 1–100 unique project IDs. Broadening includes adding scopes/projects or Selected → All. A wrong factor is 422 on `password`/`code`, not 401. Lists take numbered `page/perPage`, `q`, and optional `kind` only on workspace lists, newest first. Repeated accessible revoke is 204 with no second event.

Metadata includes ID, kind/name, workspace ID/name, owner/creator ID/name or null, project mode/selections, saved/effective scopes and creation/expiry/last-use/revocation times. Never emails, digests or internal IDs. `lastUsedAt` indicates valid credential use, at most once/hour, not a successful operation. Automation never updates human `users.last_seen_at`.

## Budgets, storage and locks

| Budget                            | Ceiling                                                      |
| --------------------------------- | ------------------------------------------------------------ |
| Ingress/IP, before lookup         | 10,000 / 5 min                                               |
| Failed authentication/IP          | 300 / 5 min; successful authentication does not increment it |
| Reads/key and personal owner      | 1,000 / 5 min each                                           |
| Writes/key and personal owner     | 300 / 15 min each                                            |
| Reads/workspace                   | 5,000 / 5 min                                                |
| Writes/workspace                  | 1,500 / 15 min                                               |
| Key issuance/user, both kinds     | 20 / hour                                                    |
| New projects/workspace through v1 | 100 / hour; replay excluded                                  |
| Management metadata reads/session | 1,000 / 5 min, outside ordinary browser budgets              |
| Management revocations/session    | 60 / hour, outside ordinary browser budgets                  |

Names and integration details are in [rate-limits.md](rate-limits.md). Write counters are shared in PostgreSQL; reads and anonymous ceilings are per process, so scaled production needs an edge limiter. IPv6 subjects use /64 and trusted client-address resolution. The anonymous counters share a **100,000-entry/process cap**, sweeping at most 256 entries/access. At saturation new untracked subjects receive generic 503 + Retry-After, existing live windows stay enforced, and no unbounded fallback/live eviction occurs. A blocked failed-auth window can refuse valid keys behind the same egress until expiry; this deliberate abuse boundary is separate from authenticated throughput.

Workspace graph mutations take `database.LockWorkspaceGraph` shared **before row locks**; account deletion/erasure takes it exclusively before account/workspace/cascade work, draining writers and preventing event-FK/creator-cascade reversal. Ordinary shared holders run concurrently. Row order: personal owner/manager non-key user lock → initiating session (management) → workspace → credentials by ID → projects; proof factors are consumed atomically. Suspension uses a non-key account lock then credentials, never a later workspace lock. Membership changes recheck under workspace lock and revoke removed members' keys there. Browser and v1 project mutations use the same transaction-local business helpers and recheck current authority. A revocation/demotion that commits first prevents a waiting write; admitted reads may finish.

Three create migration pairs, factories and both table registries cover `api_keys`, `api_key_projects`, `api_idempotency_requests`; composite selection FKs prevent cross-workspace joins. The 15-minute cleanup removes ended key metadata after 90 days (earlier of expiry/revocation), preserving unrevoked Never credentials, and expired replay state, in batches of at most 5,000. Seeders never issue usable keys. Personal metadata is included in the owner's data export without creator details or secrets; cascades handle erasure.

Lifecycle events are `workspace.api_key.created/updated/revoked` and `auth.api_key.created/updated/revoked`, security audit events kept **one year**. Project events retain their names/retention and add `api_key_public_id`, a non-secret snapshot that survives key deletion. Personal operations name the owner as actor; workspace services have no human actor. Admin event detail/CSV exposes that ID, never the secret. Whole credentials are scrubbed before generic token/ID rules, including all-lowercase/digit secrets.
