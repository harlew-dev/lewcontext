# Workspace and personal API keys

Status: **proposed 2026-11-01; decisions awaiting confirmation**. Investigation and implementation plan only. Builds on [workspace permissions](implemented/workspace-permissions.md), [rate limits](implemented/rate-limits.md) and the existing session-authenticated SPA. No API-key functionality exists yet.

## Why

Support server-side integrations and personal scripts without sharing a browser session or someone's password. Workspace settings needs an **API Keys** tab with **All projects** and **Selected projects** restrictions. A user also needs personal keys, restricted to resources and actions they can currently access.

Ownership, project selection and action permissions are three different things. A key that can see all projects need not be able to edit or delete them. A personal key must never become a way around a demotion, removal, suspension or future project-access rule.

## Investigation

### What this application does today

| Area | Finding and consequence |
| --- | --- |
| Routing | `apps/api/internal/httpapi/router/routes.go` dispatches `/api` and `/api/*` to JSON handlers; everything else goes to pages. `/api/v1` already falls inside that dispatch boundary; `/v1` would need a new one. Unknown versioned URLs must remain JSON 404s. |
| Browser authentication | `internal/httpapi/middleware.go` applies exact Origin checks to mutations before routing, then session cookies and CSRF. Machine clients usually send no Origin or CSRF token. Simply adding bearer checks to the existing group would either fail machine writes or weaken browser protection. |
| Authorization | `internal/auth/permissions.go` grants workspace roles named mutation permissions. Every member, including a viewer, can read every project. There is **no per-project membership or private-project ACL**. |
| Persistence | `internal/store/projects.go` scopes queries by workspace, but has no credential/project-selection constraint. Filtering results in middleware after loading would leak pagination counts and leave writes unsafe. |
| Settings | Workspace settings already has a layout and General / Members tabs. `settings-tabs.tsx` hides the entire tab strip without `workspace.member.index`; it needs per-tab visibility. Account & security is currently one page at `/app/settings`. |
| Identity | Controllers read `Session(r)` and `Workspace(r)`. A workspace key is not a session or a human member; representing one as its creator would give misleading audit attribution and accidental user privileges. |
| Recovery | Password changes/resets, MFA recovery, suspension, account deletion and membership removal already have explicit transactional rules. API-key lifetime must join those rules deliberately. |
| Observability | Events identify a human actor and workspace but not the credential used. Error scrubbing recognises existing public-ID prefixes and token shapes; the new key format needs explicit coverage. |

The local page-folders proposal (`plans/features/page-folders.md`, currently untracked) is still unconfirmed. This plan only moves account settings into a folder when it actually gains a child page; recheck that proposal and the source tree before implementation.

One existing documentation discrepancy: `docs/api/endpoints.md` says “Next proxy”, while the application and `docs/ui/api.md` use Vite in development and the embedded Go server in production. Correct that sentence when documenting these routes; do not base this design on Next.js.

### Production URL conventions

There is **no mandatory standard** choosing `/v1` over `/api/v1`, or requiring a separate URL just because authentication uses a key. Versioning describes the compatibility contract; authentication describes the caller.

- On a dedicated API host, `https://api.example.com/v1/...` is a common arrangement. [Stripe](https://docs.stripe.com/api/authentication) uses `/v1` and API keys; [OpenAI](https://developers.openai.com/api/reference/overview) uses `/v1` and bearer credentials.
- On the same host as a web app, `https://example.com/api/v1/...` clearly reserves the API namespace. This fits the current single-binary, single-origin deployment and Vite's `/api` proxy.
- Path versioning is not universal: [GitHub](https://docs.github.com/en/rest/authentication/authenticating-to-the-rest-api) uses an API host and a version header. Its personal tokens are still bounded by selected permissions and the user's identity.
- A private SPA backend can remain unversioned when frontend and backend deploy together. A supported integration API needs an explicit compatibility policy. They can share business rules and persistence without publishing browser login, account recovery, admin or key-management endpoints.

**Recommendation:** retain the SPA's `/api/...` routes and add a deliberately small `/api/v1/...` integration API on the same origin. Both key kinds use that same versioned API. No `/v1` alias, redirects, host split or frontend API migration is needed.

## Design

### Ownership

Every key is bound to **one workspace**, including a personal key. Personal keys are managed together in account settings, but the creation form chooses one of the user's workspaces. This is a recommendation to confirm, not an assumption that personal means cross-workspace.

| Kind | Who creates/manages it | Effective authority | Staff changes |
| --- | --- | --- | --- |
| Workspace | Workspace owners and admins | Explicit saved project/action grants, capped to the public API's supported actions; not a human role | Remains valid if its creator leaves, is demoted, suspended or deleted. The workspace owns the integration. Other owners/admins can revoke it. |
| Personal | Any member, including a viewer, for themselves | Saved project/action grants **intersected with the owner's current membership, role and account eligibility on every request** | Removal/leaving permanently revokes that workspace's personal keys; rejoining never revives them. Suspension and credential-recovery rules below also revoke them. |

Workspace owners/admins can list metadata and revoke **either kind** issued for their workspace. They cannot recover secrets, take ownership of personal keys, edit another person's personal grants, or see that person's keys for other workspaces. A user cannot list anyone else's personal keys from account settings.

Add `workspace.api_key.index`, `.store`, `.update`, `.destroy` (`WorkspaceApiKeyIndex`, etc.) for owners/admins. Personal self-management uses the account session and ownership checks, not those permissions; viewer continues to have no workspace mutation grants. Personal creation also validates workspace membership and the proposed actions in the store transaction.

### Project restriction and action permissions

The form has separate fields:

1. **Project access:** `all` or `selected`.
2. **Permissions:** explicit actions from the following fixed catalogue. Defaults to Read projects only.

| UI choice | Saved scopes | Available to a personal key's owner |
| --- | --- | --- |
| Read projects | `project.index`, `project.show` | Any current workspace member |
| Create projects | `project.store` | Owner, admin, member; only with All projects |
| Update projects | `project.update` | Owner, admin, member |
| Delete projects | `project.destroy` | Owner, admin |

These are API-key scopes, not additional role grants. Reuse the existing role check for mutation scopes; membership supplies read authority. Do not grant viewers new permissions or add a second role matrix. No wildcard scopes: new resources/actions do not silently become available to old keys. Require at least one action; the UI expands Read into its two scopes.

- **All projects** means existing and future projects **in that workspace**, subject to the personal owner's live access. Say this explicitly in the field description. It never means all workspaces or all future API resources.
- **Selected projects** means an explicit set of 1–100 distinct project IDs at creation. Each must be accessible and belong to that workspace. A mixed valid/invalid selection fails atomically with a generic field error; do not reveal which inaccessible ID exists.
- A selected-project key **cannot create projects**, because a new project is not in its saved selection. Reject that combination in the API as well as the form. Do not silently add created projects to a selection.
- A deleted project removes its selection row. If the final selected project is deleted, the key stays selected and grants access to **no projects**, never all projects. The UI says “No selected projects remain”; editing requires selecting at least one again.
- A personal key's saved scopes cannot exceed the owner's role at creation or edit. Later promotion never adds unsaved scopes. A later demotion immediately removes effective actions; if promoted back, previously saved actions become effective again. Removal/leaving instead revokes permanently.
- This feature restricts **credentials**, not the person's SPA access. If per-project membership is introduced later, its shared access predicate must also narrow personal keys and their project picker; this plan does not introduce that separate authorization system.

### Credential format and lifecycle

- Generate 32 random secret bytes with `crypto/rand`. Suggested wire format: `lc_wk_<apk_public_id>_<43-character base64url secret>` or `lc_pk_<apk_public_id>_<secret>`. `apk_` plus 14 base62 is the key record's PostgreSQL-generated public ID; it is not the secret. The parser checks the exact fixed structure and kind. No JWT, password hashing or new crypto dependency.
- Look up by public ID and compare SHA-256 digests of the full canonical credential in constant time. Only the digest is stored. An unknown ID uses a dummy comparison; malformed/unknown/wrong-kind/revoked/expired credentials share a generic 401. Never include the credential in an error.
- Return the secret in the **201 creation response only**, with `Cache-Control: no-store`. Lists, edits, exports, events, logs and admin views never return a digest or secret. Display kind + public ID as the non-secret identifier; no need to retain secret suffix characters.
- Require a name (trimmed, 1–80 characters, `input.ValidText`) and expiration: default 90 days, minimum 1 hour, maximum 365 days from creation. No never-expiring choice in the initial release. PATCH may rename, replace grants/selections or shorten expiry; it cannot change ownership/workspace/kind, replace the secret or extend expiry. Expired/revoked keys cannot be edited or reactivated.
- Revocation sets `revoked_at` and clears the digest in the same transaction as its event. No positive authentication cache: later requests see revocation directly in PostgreSQL. A read already admitted may finish; a write must recheck under the locks below.
- Rotation is **create replacement → deploy replacement → revoke old key**. Both may work during the explicit overlap. Do not automatically revoke the old key before its replacement reaches the integration. A lost creation response means revoke that record and create another; no recover-secret endpoint or automatic POST retry.
- Update `last_used_at` approximately, at most once per hour with a conditional update after valid authentication and workspace matching. It indicates credential use, not a successful business operation, and must not turn into a write/event per request. Do not update `users.last_seen_at` from automation: that remains human app activity.

### Recovery and revocation rules

| Change | Personal keys | Workspace keys created by that person |
| --- | --- | --- |
| Browser logout / sign out other sessions | Stay valid; those controls explicitly concern browser sessions | Stay valid |
| Password change, completed password reset, operator MFA reset (CLI or dashboard), account suspension | Permanently revoke all of that user's personal keys in the same transaction; unsuspension never revives them | Stay valid; independent workspace credentials, visible to workspace managers for review |
| Ordinary MFA enable/disable or new recovery codes | Stay valid; API keys intentionally authenticate unattended clients | Stay valid |
| Leave/remove membership | Permanently revoke that user's keys for that workspace, even if they later rejoin | Stay valid |
| Delete account | Delete personal keys; clear creator references on workspace keys | Stay valid |
| Delete workspace | Delete every key and selection in it | Delete every key and selection in it |

The UI explains that API keys are separate long-lived credentials and do not prompt for MFA on API calls. Credential issuance/broadening requires fresh proof: the current password when MFA is off, an authenticator/recovery code when MFA is on, checked in the same transaction as issuance/grant change. Reuse existing password concurrency and auth/MFA limits; never trust a prior `/password/verify` call or introduce an admin session. Renaming, reducing access, shortening expiry and revoking need only the normal session + CSRF so recovery remains available.

### Public API contract

Initial supported resource: **projects only**. Do not publish the whole SPA API by prefixing every route with `v1`.

| Method | Path | Required scope | Result |
| --- | --- | --- | --- |
| GET | `/api/v1/workspaces/{workspace}/projects` | `project.index` | Scoped `Page<Project>` |
| POST | `/api/v1/workspaces/{workspace}/projects` | `project.store`, All projects | 201 project |
| GET | `/api/v1/workspaces/{workspace}/projects/{project}` | `project.show` | 200 project |
| PATCH | `/api/v1/workspaces/{workspace}/projects/{project}` | `project.update` | 200 project |
| DELETE | `/api/v1/workspaces/{workspace}/projects/{project}` | `project.destroy` | 204 |

Use the current project JSON, input validation, status values, deterministic pagination/search/sort and full-editable-representation PATCH semantics, documented explicitly for v1. Key scopes include neither account nor workspace administration. No workspace-list endpoint is needed for a single-workspace credential; the creation screen supplies its workspace ID and base URL.

Authenticate only with `Authorization: Bearer <key>` over HTTPS in production. Never accept keys in a URL, JSON auth field, cookie or `X-API-Key` alias. Require exactly one bounded Authorization header with one credential; no comma-joined or multiple credentials. Cookie-only requests to v1 get 401; a valid browser/admin cookie never rescues an invalid bearer key. Cookies accompanying a valid key have no authority and are ignored. Keys never authenticate the SPA API, admin API, page gate or metrics listener.

- **401:** missing/invalid/revoked/expired key or ineligible personal owner, with `WWW-Authenticate: Bearer`; no login redirect or cookie changes.
- **404:** wrong workspace, malformed/out-of-scope/missing project, using the same response for inaccessible and absent resources. Check resource scope before returning action-specific detail.
- **403:** authenticated and in-scope but lacking the required action (including a demoted personal owner).
- **400/415/422/429/500/503:** retain the application's validation, JSON content type, quota, rate-limit and generic error conventions. No CAPTCHA challenges for machine traffic. Include the server-generated `X-Request-ID`; 500s retain `requestId`.

Lists apply the credential's project predicate **before** search, counts, sorting and pagination. A selected key sees only selected rows and scoped totals; searching or paging never reveals another project's existence. Detail, update and delete queries carry both workspace and project-selection predicates; middleware-only filtering is insufficient. Future project sub-resources must inherit this same check.

Version policy: keep v1's documented fields, validation and semantics stable. Additive optional fields/endpoints are normally compatible; removing/renaming fields, adding required inputs or changing semantics requires a new major version or an explicit deprecation/migration policy. Internal SPA changes must not silently change v1's DTOs or behavior. Authentication is a reusable layer; `v1` is not an authentication mode.

### Browser management API

These remain session-only under `/api`, with the existing exact Origin, JSON and CSRF rules. An API key cannot create, inspect, edit or revoke keys, including itself.

| Method | Path | Controller | Access |
| --- | --- | --- | --- |
| GET | `/api/workspaces/{workspace}/api-keys` | `workspace.ApiKeys.Index` | `workspace.api_key.index`; workspace and personal metadata in this workspace |
| POST | `/api/workspaces/{workspace}/api-keys` | `workspace.ApiKeys.Store` | `.store`; creates workspace kind only, fresh proof |
| PATCH | `/api/workspaces/{workspace}/api-keys/{key}` | `workspace.ApiKeys.Update` | `.update`; workspace kind only, fresh proof if broadening |
| DELETE | `/api/workspaces/{workspace}/api-keys/{key}` | `workspace.ApiKeys.Destroy` | `.destroy`; revoke either kind in this workspace |
| GET | `/api/auth/api-keys` | `account.ApiKeys.Index` | Session; own personal metadata only, across current memberships |
| POST | `/api/auth/api-keys` | `account.ApiKeys.Store` | Session; `{workspace, ...}` checked through own membership, fresh proof |
| PATCH | `/api/auth/api-keys/{key}` | `account.ApiKeys.Update` | Session + ownership + live membership; fresh proof if broadening |
| DELETE | `/api/auth/api-keys/{key}` | `account.ApiKeys.Destroy` | Session + ownership; still available to revoke a key that has lost effective access |

Creation body: `{name, projectAccess: "all"|"selected", projects: ["prj_..."], scopes: [...], expiresAt, password? or code?}`, plus `workspace` for personal creation. All projects requires an empty `projects` array. Update replaces the editable grant configuration explicitly, without accepting a kind or owner field. Compare old and new effective grants under lock to decide if fresh proof is needed; switching selected → all or adding a project/scope is broadening. Wrong proof is 422 on its field, not session-expiry 401.

List metadata: `{id, kind, name, workspace: {id, name}, owner: {id, name}|null, createdBy: {id, name}|null, projectAccess, projects: [{id, name}], scopes, effectiveScopes, createdAt, expiresAt, lastUsedAt, revokedAt}`. No emails, internal IDs or computed role pretending a service key is a member. Creator/owner names are shown only within the authorized management view. Personal lists compute effective scopes for the current owner; workspace managers may inspect that same metadata for their workspace. Omit inaccessible project names if future ACLs narrow the manager's access.

Lists use the existing numbered page contract (`page`, `perPage`, `q`, optional `kind` on the workspace list), newest first with ID tie-break. No per-record GET endpoint or detail page is necessary initially. Repeated revoke on an accessible retained record is 204 with no duplicate event; a missing or differently owned/scoped record is 404.

## Backend structure and concurrency

Keep all route declarations in `router/routes.go`. Add an explicit public-v1 group and separate transport/security entry for `/api/v1` and `/api/v1/*`, selected **before** the general `/api` branch. Preserve headers, no-store, timeout, panic recovery, client-address resolution and IP blocks. Share those transport protections, but keep browser Origin/CSRF validation in the browser branch and bearer authentication in v1. A caller-supplied Authorization header must never switch the browser branch into a weaker mode. V1 does not require Origin; CORS remains disabled.

- Crypto/parser and fixed scope catalogue: `internal/auth/api_keys.go` and focused adjacent files as needed.
- Authentication context and middleware: core `internal/httpapi/api_keys.go`; use a typed credential principal, not a fabricated `Session` or membership.
- Browser controllers: `endpoints/account/api_keys.go`, `endpoints/workspace/api_keys.go` (`ApiKeys` matches the existing PascalCase naming checker; do not invent a casing exception).
- V1 controller: `endpoints/workspace/api_projects.go`, `workspace.ApiProjects`, the same domain area as projects. Document the optional `v1` transport prefix in the area naming rule; avoid a new `v1` area that imports the workspace area.
- Shared project input parsing/validation belongs in `httpapi/input`; keep separate v1 DTOs at the boundary so browser DTO changes do not break v1. Both controllers call shared store operations/rules; neither area imports another, and business logic is not copied.
- Persistence: focused `store/api_keys.go`, `api_key_projects.go`, `api_projects.go` and, if useful, `api_key_access.go`. Use an explicit typed project-access value. Do not add an optional filter whose missing/zero value accidentally grants all projects. Existing SPA callers explicitly use their member access; public callers must supply a validated credential access value.
- Fail closed on unknown kind, scope, role or project-access mode. Register **every public route**, reads included, with an explicit required key scope and require a complete credential-kind × role × route test matrix. Preserve the existing SPA permission startup guard.

**Transaction guarantee:** a v1 write rechecks key validity, expiry, grants and (personal) current membership/account state inside its mutation transaction. Do not let a middleware check followed by an unguarded store write race revocation or demotion. Extend project write methods with transaction-aware shared helpers so authorization and mutation share the same transaction.

Use this lock order wherever applicable: personal owner's user row (or the human manager's row for management), workspace rows in ID order, credential rows in ID order, project rows. Personal API writes lock the owner before the workspace; workspace API writes start at the workspace. Membership changes already hold the workspace and revoke affected keys after it; they must not acquire a new user lock after the workspace. Password reset/suspension holds the user then revokes personal keys, never acquires a workspace lock after locking a credential. Conditional last-used writes acquire no later locks. Creation/quota checks, project-selection edits, revocation and deletion follow the same ordering. Explicitly test manager-vs-owner operations and account-deletion cascades for deadlocks.

A revocation/demotion/removal that commits first must prevent a waiting write; a write that obtains the relevant locks first can commit before the revocation completes. Already-admitted reads may complete under their statement's snapshot. This is the documented boundary, not a promise to cancel network responses already in flight.

## Data changes

### `api_keys`

One create migration pair, with:

- Internal bigint primary key; unique `public_id DEFAULT public_id('apk')` with the normal format CHECK.
- `workspace_id NOT NULL` (`CASCADE`); `kind` (`workspace`, `personal`); `owner_user_id` (personal only, `CASCADE`); `created_by` (nullable user FK, `SET NULL`). CHECK personal requires an owner and workspace kind has none. Creator is attribution only, never authority.
- `name`, `token_hash` (unique while present, exact digest shape; null only when revoked), `project_access` (`all`, `selected`), nonempty `scopes text[]` constrained to the fixed catalogue; reject duplicates in validation. CHECK prohibits `project.store` in selected mode.
- `created_at`, `updated_at`, mandatory `expires_at`, nullable `last_used_at`, `revoked_at`; CHECK creation-relative expiry bounds and a revoked key having no digest. API validation uses the same durations.
- Indexes beginning with `workspace_id` and personal `owner_user_id` for management lists/quotas; expiry/revocation cleanup indexes. Unique `(id, workspace_id)` supports selection FKs. No uniqueness on a user-chosen name: multiple integrations may legitimately share a label.

### `api_key_projects`

One create migration pair: bigint primary key for the existing factory/table tooling, `workspace_id`, `api_key_id`, `project_id`, unique `(api_key_id, project_id)`. Composite FKs `(api_key_id, workspace_id)` → keys and `(project_id, workspace_id)` → projects prevent cross-workspace selections at the database boundary; add the corresponding unique project key to its squashed create migration. Both cascade. Index starting with `workspace_id`, plus a project-leading index for project deletion. No public ID: selection rows are never addressed individually.

Store transactions enforce selected mode's nonempty selection on creation/edit and all mode's empty selection; project deletion may reduce selected mode to zero. Never infer `all` from an empty join table.

### Credential attribution on existing events

Add `events.api_key_public_id text NOT NULL DEFAULT ''`, CHECK empty or a valid `apk_` ID; it is a non-secret snapshot, not a foreign key. It survives key deletion so retained project events can identify the credential used. Set it from trusted credential context on every key-authenticated event; do not put the creator in `actor_id` for workspace-key requests. Personal requests use the owner as actor; service requests have no human actor. Browser key-management events use their actual human manager as actor, and name the target key in the payload.

Update the event model, factory, safe admin event detail/CSV representation and typed UI fields. No new admin action/endpoint family is needed. Treat `apk_` as an ID in scrubbing, while specifically scrubbing **the whole credential including its public-ID segment** before generic ID replacement. Test worst-case secrets made only of lower-case letters/digits too; generic token heuristics are insufficient.

### Retention, export and registries

- Keep active keys until expiry/revocation or their owning workspace/account is deleted. Keep expired/revoked metadata for 90 days after its effective end (the earlier of expiry and revocation), then bounded cleanup deletes it and its selections. Revocation clears the digest immediately; an expired record's digest is removed with the record at cleanup. Authentication always refuses expiry, independently of cleanup.
- Personal metadata is visible to its owner and the workspace's managers; workspace-key metadata to managers. Digests are never visible. Add both tables to the compliance data map, `DataTables`, `WorkspaceTables`, table-size monitoring and their factories. Seeding must not create usable credentials.
- Download my data adds the account's own personal-key metadata, never secret/digest. Do not export other members' keys or workspace-key secrets/creator details from an account export. Erasure cascades personal keys and clears workspace-key creator attribution; retained event rules continue to apply.
- Lifecycle events: `workspace.api_key.created`, `.updated`, `.revoked` for workspace-owned credentials; `auth.api_key.created`, `.updated`, `.revoked` for personal credentials, with key/workspace public IDs, bounded scopes/selections/changes and a fixed reason for automatic revocation. No secret, digest or credential string. Workspace managers revoking a personal key remain the actor of its `auth.api_key.revoked` event.
- Both API-key lifecycle families are security audit records kept one year, with the existing audit lines. Explicitly classify `workspace.api_key.*` as audit events; its current `workspace.*` default is only 90 days. Project mutations keep their existing event names/retention and gain credential attribution. Do not record an event on each key read, successful authentication or failed credential guess.

Development migrations remain squashed as documented; implementation updates the relevant create files and new table pairs/factories together. Recheck whether any deployment has persistent data before choosing the migration workflow. No database rebuild is part of planning.

## Rate limits and quotas

Machine traffic has its own budgets, so a busy/leaked key cannot exhaust the browser's recovery allowance. Workspace aggregate budgets prevent creating many keys to multiply throughput. Initial numbers are conservative defaults to confirm:

| Limit | Initial budget | Storage |
| --- | --- | --- |
| `api-key-auth:per-ip` | 300 / 5 minutes, before credential lookup on every v1 request | In-memory, bounded counters; edge limiting across servers |
| `api-key-read:per-key` | 1,000 / 5 minutes | In-memory, like existing read budgets |
| `api-key-write:per-key` | 300 / 15 minutes | Existing PostgreSQL counters |
| `api-key-read:per-workspace` | 5,000 / 5 minutes, both key kinds combined | In-memory |
| `api-key-write:per-workspace` | 1,500 / 15 minutes, both kinds combined | PostgreSQL |
| `api-key-read:per-user` | 1,000 / 5 minutes, all of a user's personal keys combined | In-memory; separate from SPA budget |
| `api-key-write:per-user` | 300 / 15 minutes, all personal keys combined | PostgreSQL; separate from SPA budget |
| `api-key-create:per-user` | 20 / hour, personal and workspace issuance combined | PostgreSQL, browser management |
| `api-key-project:per-workspace` | 100 project creates / hour, both kinds combined | PostgreSQL, in addition to normal project quota |
| `api-key-revoke:per-user` | 60 / hour, browser revocation; counted by the authenticated browser session, as session recovery already is | PostgreSQL |

All have block levels only, 429 with `Retry-After`; no CAPTCHA or new global challenge budget. After the pre-auth IP budget, count authenticated per-key, then personal-owner when applicable, then workspace budgets; scope/authorization still precedes business side effects. Invalid credentials must never spend a guessed key's/user's/workspace's quota or create unbounded per-token counters. Global/anonymous floods remain the edge's responsibility. `PER_IP_LIMITS` and `RATE_LIMITS` retain their documented behavior.

Quota: at most 100 live keys total per workspace (both kinds combined), and 50 live personal keys per user across workspaces. “Live” means unrevoked and unexpired. Enforce in creation transactions under the user/workspace locks, plus the 100 selected-project bound and existing 1,000-project quota. Revoked metadata does not consume a live slot but its growth is bounded by issuance rate and retention.

Management reads/revocation use session authentication outside the ordinary browser read/write budgets, with bounded dedicated management-read limits (1,000 / 5 minutes per session) and the revocation limit above. Key writes cannot call management at all. This keeps the owner able to find/revoke keys after their ordinary SPA budget has been exhausted. Creation/edit retain normal browser budgets and fresh-proof limits. Session-specific recovery counting follows the existing documented precedent rather than letting another session exhaust recovery for the owner.

**Convention decision requiring confirmation:** rate-limit names currently allow neither `per-key` nor `per-workspace`. Add those two counting kinds to `docs/naming.md`, `docs/checks.md` and `TestRateLimitNames` only after approval. Do not disguise service credentials as `per-user`, loosen the check or add one-off exceptions. Declare the dedicated management-read budget as `api-key-management-read:per-user`, documenting its per-session recovery counting alongside the revoke budget, as existing session revocation does.

In-memory read budgets are per process; N servers multiply those ceilings. This initial design inherits that explicit limitation, requires an edge limiter for scaled production, and does not claim globally exact read limits. Write counters remain shared/exact. Metrics label only fixed route/limit/kind/outcome names, never key/user/workspace IDs. Access lines may carry `authType` and the non-secret public `credential` ID; never the Authorization value. Recheck the observability overhead budget.

## Web app

### Workspace settings → API Keys

Page `/app/:workspace/settings/api-keys`, `pages/app/[workspace]/settings/api-keys.tsx`, default `WorkspaceApiKeyIndex`; page constant `routes.workspaceApiKeys.index(ws)`. Add it under the existing settings layout and build tab visibility from each tab's permission. Non-managers see no key data and fetch none, including when navigating directly.

- Workspace-owned list, with **Create API key** in its section header; a separate **Personal keys in this workspace** list for review/revocation. Each follows the server's pagination and search. Both use the same metadata row primitive where appropriate.
- Columns: Name, identifier, project access, permissions, owner/creator where applicable, created, expires/status, last used (“Never” initially), and a right-aligned row menu. Use words for Active / Expired / Revoked, not color alone.
- Workspace-key row actions: Edit and Revoke while active. Personal-key rows: Revoke only; explain the key belongs to the member and is limited by their current access.
- Explain workspace credentials survive staff changes and are for shared integrations; personal credentials are for personal scripts.

### Account & security → API Keys

Page `/app/settings/api-keys`, `pages/app/settings/api-keys.tsx`, default `UserApiKeyIndex`; page constant `routes.userApiKeys.index()`. Introduce `pages/app/settings/layout.tsx` with General / API Keys tabs and move the existing account page to `settings/index.tsx`. `/app/settings` remains the General URL. No new sidebar item; the account menu remains selected on both pages.

- List only the signed-in user's personal keys, with workspace as a column. Creation chooses one current workspace, then the same project/action controls. Users with no remaining workspaces cannot create; retain the existing onboarding gate, whose recovery/account-deletion paths still work.
- The project picker fetches normal session-authorized projects from the chosen workspace; server validates every selection again. Changing workspace clears project choices. Page/search project options through the existing API; do not assume its first 20/100 projects are the whole workspace. Preserve selected IDs across option pages and offer Retry for failed loads.
- Only offer actions the workspace response allows, but the server enforces the same rule. A viewer can create a read-only personal key. Refresh failures preserve available data with Retry.

### Shared interactions

Shared form, metadata rows and one-time secret UI go in `components/app/api-keys/` because both settings areas use them; each page's unique content stays local. Use existing `Dialog`, `ConfirmationDialog`, `Listbox`/`Combobox`, `Checkbox`, `Input`, `Reveal`, `CopyButton`, `PaginatedTable`, notices, skeletons and toasts. If the project picker needs remote-option behavior, extend the existing combobox API rather than introducing another interactive library.

Create dialog: name, workspace (personal only), Project access, project selector when selected, action checkboxes, expiration, fresh proof. Cancel · **Create API key**, primary last. On success move to a one-time secret panel that is masked with a decoy until revealed, supports Copy while masked, and says it cannot be shown again. Explicit Done discards it; closing early warns that it will be lost. A close after a committed create does not revoke the record automatically.

Keep the secret only in local component state for that creation flow; never SWR, module state, history, URL, local/session storage, analytics or client-error payloads. Clear it on Done, navigation, workspace switch and sign-out. Use a secret-shaped placeholder for screenshots/fixtures, never a usable credential. Include a curl example referencing `$LEWCONTEXT_API_KEY`, the chosen workspace ID and `/api/v1` base URL; do not render a ready-to-share command containing the secret.

Revoke uses `ConfirmationDialog variant="danger"`: “Integrations using this key will stop working immediately.” Cancel · **Revoke API key**. All mutations use `useAction`, prevent duplicates, show field errors/pending state, announce success and revalidate metadata only. No automatic creation retry after timeout. Register new pages in `routes.ts`, `returnRoutes`, both client-error route-pattern catalogues and page naming checks.

## Security risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Key bypasses browser Origin/CSRF | Separate route-selected security branch; never switch behavior based on whether an Authorization header exists. Regression-test every browser/admin mutation boundary. |
| Key turns into a human/admin session | Typed credential principal, no fabricated session/role, explicit v1 route allowlist; all management/session/admin endpoints remain inaccessible to a key. |
| Cross-workspace/project access or leaked totals | Workspace binding, composite FKs, access predicates before counts/paging, same 404 for absent and inaccessible records, write predicates rechecked transactionally. |
| Personal key outlives authority | Live role/account/membership intersection; permanent revocation on removal, suspension and recovery; no revival on rejoin/unsuspension. |
| Creator's permissions silently define service access | Workspace key has explicit grants and workspace ownership, no creator-derived authority; managers review/revoke it. |
| Stolen browser session creates durable access | Fresh password or MFA proof for issuance/broadening, normal CSRF/Origin and auth limits, bounded expiry and audit events. |
| Secret leaks in errors, logs or UI cache | Digest only, creation-only response, dedicated whole-credential scrubbing, no-store, local ephemeral state, regression scans of exports/logs/events/error reports. |
| Flood or key multiplication | Pre-auth IP/edge protection, bounded headers/counters, per-key plus aggregate workspace/personal-owner budgets, live-key quotas and cleanup. |
| Concurrent revoke/demotion still permits a later write | Consistent locks and in-transaction recheck; adversarial concurrency tests. |

## Tests and acceptance criteria

### Go unit and PostgreSQL integration

- Credential generation/parser bounds, randomness, digests, kind/ID matching and generic 401/`WWW-Authenticate`; multiple/oversized Authorization headers, malformed values, URL/body/cookie auth refused. No numeric IDs or secret/digest in any ordinary response.
- Cookie-only v1 denied; valid cookie + invalid bearer denied; valid bearer + unrelated cookie uses only the bearer principal. Both key kinds denied on every browser/admin/session/key-management/page-gate boundary. V1 writes work without Origin/CSRF; browser mutation protections remain unchanged even with an Authorization header.
- Complete scope × key kind × role × route matrix: viewer read-only personal keys, member create/update but not delete, managers' workspace credentials, unknown values denied, new route missing from the matrix fails.
- Selected/all filtering before pagination counts/search/sort; cross-workspace selections rejected atomically; details/writes for unselected or missing projects identical 404; an empty/deleted selection never becomes all; selected + create rejected; future project included only for all.
- Stored grants capped on create/edit; demotion removes actions immediately; promotion adds no unsaved actions; removal/rejoin, suspension/unsuspension, password change/reset, CLI/dashboard MFA reset and deletion follow the lifecycle table. Creator removal/deletion/suspension leaves workspace keys working with no false human actor.
- Concurrent create at quota, revoke vs write, edit/narrowing vs write, demotion/removal vs write, password reset/suspension vs personal create/write, manager revoking another user's key and account/workspace deletion: deterministic boundaries, no deadlock, no post-revocation mutation. Existing project validation/quota/events still apply to both APIs.
- Dedicated and aggregate budgets, two keys sharing workspace/owner caps, browser recovery after exhausted budgets, bounded invalid-auth counter subjects, trusted proxy/IPv6 behavior, 429/Retry-After and one rate-limit event per window. No CAPTCHA for v1.
- Metadata retention and bounded cleanup; clearing secret material; composite FKs/checks/indexes; a factory per table and registries/data-map complete. Account export/erasure contains no credential/hash or other people's keys; security lifecycle retention one year.
- Project events carry the credential public ID and correct human/null actor; automatic revocation has a fixed reason and actual manager actor where applicable. Logs/events/CSV/errors never contain full/partial credentials or digests. Whole-credential scrubbing includes adversarial alphabet shapes. Metrics have no identifier-valued labels; unknown `/api/v1` and `/api/v2` paths are JSON 404s, never SPA HTML.

### Vitest and Playwright

- Per-tab permissions and direct-navigation denial without fetching; workspace manager review/revoke, own-only personal list, workspace reset of selection, paginated project options, viewer restrictions and server field errors.
- Duplicate create submits once; creation timeout preserves a warning to inspect/revoke the possibly created record; create→secret→Done, reveal/copy with existing feedback, no raw secret in DOM while masked, no secret retained in cache/history/storage or after navigation/sign-out.
- Empty/loading/refresh failure with retained data and Retry; edit/broadening proof, expiry, confirmation/failure/success for revoke; mobile/desktop, keyboard focus, reduced motion and unchanged admin-build isolation.
- E2E: create a selected-project key in UI; use it with an HTTP client to read/update only that project and inspect scoped totals; revoke in UI and confirm 401. Repeat with a personal key, then demote/remove its owner and confirm changed access. Verify SPA cookie+CSRF still works and no page error/CSP violation. Save screenshots of settings/form/confirmation with secrets concealed.

Implementation checks: `make test`, `make check`, `make build`, `make test-e2e E2E_PORT=auto`, `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` from `apps/api`, and `npm audit` from `apps/web` (auth change). Check competing runs/machine capacity first and run long suites in the background. No new behavior tests or build are needed merely to review this proposed Markdown plan; validate its formatting/links and report any required repository-check failures separately.

## Implementation order

1. Confirm the decisions below, including the deliberate extensions to the current session-only/workspace-member rules and rate-limit vocabulary. Update the plan with the answers before writing code.
2. Add key primitives/scopes, schema/factories/registries, metadata CRUD, lifecycle revocation and audit attribution/retention. Cover persistence, recovery and lock ordering first.
3. Introduce the distinct v1 security boundary and project access predicate, then the five public project routes. Keep SPA behavior behind its existing session boundary; complete the authorization/concurrency matrix before exposing v1.
4. Add both settings pages and shared key UI/typed client, then end-to-end integration and revocation flows. Document v1 requests, rotation and compatibility.
5. Run the checks, record screenshots with secrets concealed, document deviations, set Status to implemented with date, and move this plan into `implemented/` with fixed links.

## Docs to update when implemented

- `docs/api/api-keys.md` (new): both ownership models, effective access, wire/header format, management/public API examples, expiry, rotation, revocation/recovery, rates and compatibility policy. Add its rows to `docs/README.md` and AGENTS.md.
- `docs/naming.md`, `docs/checks.md`: optional version prefix keeps controllers in their domain area; approved per-key/per-workspace limit kinds and corresponding exact validation. No new controller actions or store-verb exceptions are needed.
- `docs/api/README.md`, `docs/api/endpoints.md`, `docs/api/auth.md`: route/security split, session-only management, typed credential principal, browser safeguards, permissions, lifecycle and public contract; fix the stale Next proxy sentence.
- `docs/api/rate-limits.md`, `docs/api/database.md`, `docs/api/seeding.md`: dedicated/aggregate/recovery budgets, quotas, tables/constraints/indexes, cleanup, factories and no usable seeded keys.
- `docs/api/events.md`, `docs/api/observability.md`, `docs/api/admin.md`, `docs/api/monitoring.md`: credential attribution in existing event detail/CSV, actor rules, lifecycle retention/audit lines, safe logs/scrubbing, table/limit registry coverage, recovery action effects. No secret visibility or new dashboard powers.
- `docs/compliance.md`, `docs/incident-response.md`, `docs/deployment.md`: data map, retention/export/erasure, keys as an incident-containment surface, revocation/rotation, API path routing/Authorization forwarding and scaled read-limit caveat.
- `docs/ui/README.md`, `docs/ui/pages.md`, `docs/ui/api.md`, `docs/ui/design.md`, `docs/ui/forms-and-actions.md`, API/UI testing docs: new routes/layout/tabs, permission-aware controls, typed metadata clients, one-time secret UI, actions and behavior coverage.
- AGENTS.md's architecture/security rules: private browser endpoints still require sessions; the explicit supported v1 routes use key authentication/workspace binding instead. Workspace data's human-membership rule gains this intentional service-principal case. All other session, role, scope and admin invariants remain enforced.

## Decisions to confirm

All rows below are recommendations, not approved decisions. The first six set the main product behavior; the rest make the security/lifecycle contract implementable.

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | SPA URL vs integration URL | Keep `/api` for SPA/admin and use **`/api/v1`** for supported integrations, same host/process. No frontend migration or `/v1` alias. |
| 2 | Meaning of workspace vs personal | Workspace keys are independent workspace credentials managed by owners/admins. Personal keys act as their owner with live permission intersection. Both call the same public routes. |
| 3 | How many workspaces per personal key? | **One chosen workspace per key**; account settings lists all of the user's keys. A new membership can never silently widen a key to a new workspace. |
| 4 | What does All projects mean? | Current **and future** projects in that one workspace, subject to a personal owner's live access; Selected is an explicit allowlist. No per-project human ACL in this feature. |
| 5 | Actions and first public endpoints | Read / Create / Update / Delete project scopes, read-only default; initial public API is project CRUD only. Selected-project keys cannot create projects. |
| 6 | Workspace visibility/control of personal keys | Workspace owners/admins can inspect metadata and revoke personal keys for their workspace, but cannot edit grants or recover secrets. |
| 7 | Expiry and rotation | Default 90 days, mandatory maximum 365 days, no expiry extension/reactivation; rotate by creating a replacement, deploying it, then revoking the old key. |
| 8 | Issuance/broadening proof | Current password if MFA off, authenticator/recovery code if on; no extra proof for narrowing or revocation. |
| 9 | Recovery/offboarding | Permanent personal revocation on password change/reset, operator MFA reset, suspension or leaving/removal. Browser logout doesn't revoke keys; workspace keys survive creator changes. |
| 10 | Grants after role changes | Live intersection: demotion removes effective actions; later promotion can restore **saved** actions only. Removed/revoked credentials never revive. |
| 11 | Limits and new vocabulary | Adopt the proposed budgets/quotas, max 100 selected projects, and explicitly approve `per-key`/`per-workspace` counting kinds. Per-process read limits initially, documented edge requirement when scaled. |
| 12 | Retention/attribution | One-year security lifecycle events for both kinds; key public-ID snapshot on existing project events, no fabricated service actor; ended metadata retained 90 days, no secret/hash export. |
| 13 | Settings locations | Workspace settings → API Keys, and Account & security → API Keys, with the dedicated pages/layout described above. |

## Not included

- OAuth/third-party consent, JWT access tokens, refresh tokens, workload identity, impersonation or a separate API host/gateway.
- Keys spanning several workspaces, custom roles, project privacy/membership, workspace-level “disable personal keys” policy or admin approval workflow.
- Workspace/member/invitation/account/admin/credential management through API keys; new project sub-resources.
- IP allowlists per key, per-key custom throughput, usage billing/charts, per-request database access logs, webhooks, SDK generation or an interactive API explorer. V1 gets a documented HTTP contract and curl examples first.
- A one-click rotation endpoint or displaying a secret again after creation.
