# Workspace audit log

Status: **implemented 2026-11-03.** Decisions confirmed by the user (below); built as planned apart from the differences listed under "As built". Builds on [events](events.md), [workspace permissions](workspace-permissions.md) and the admin [Events page](admin/events-page.md). Closes the known gap in [compliance.md](../../../docs/compliance.md#known-gaps) ("No audit log for workspace owners yet").

## Why

Owners can't see what happened in their workspace: who renamed a project, who removed a member, who created an API key, who was invited. The data is already recorded in `events` (same transaction as each change), but only admins can read it, across every workspace. Owners need a **Logs** tab in Workspace settings that shows their workspace's changes, filtered by person, type, date and text, with each entry's details readable (not raw JSON).

This is a new feature, but a thin one: no new table, no new event, no migration. The work is in what owners are allowed to see, and in a readable projection of each event.

## What it can and can't show

Only events recorded **against the workspace** (`events.workspace_id`): workspace renamed, members changed/removed/left, invitations created/revoked/accepted, projects created/updated/deleted, workspace API keys. It does not show what members did outside workspace changes: sign-ins (`auth.*` has no workspace), page views or reads, or anything API-side that records no event. The tab says "Changes in this workspace", not "all activity". Adding more recorded events later (for example project reads) is a separate decision.

## Design

```
/app/<wsp>/settings/events           Logs tab (owners only)
  GET /api/workspaces/{ws}/events    workspace.event.index (owner)
  filters: People · Type · Date range · Project · Search     (all in the URL)
  table:  Time · Who · What (a sentence) · row opens in place to Details
  Older / Newer (cursor)
```

### Which events, and which fields (the safety rule)

The admin Events page shows `payload` as stored. Owners get a **projection instead**: for each allowed event name, a fixed function in `internal/events/workspace_log.go` turns the stored payload into a small typed view. Anything not in the table is invisible, so **a new event is hidden from owners until someone classifies it** (`TestEveryEventIsClassifiedForOwners`: every catalogue name is either in `WorkspaceLog` or in an explicit `HiddenFromOwners` list with a reason).

| Event | Owners see | Never shown |
| --- | --- | --- |
| `workspace.created`, `.renamed`, `.deleted` | name, `changes.name` from/to. (`deleted` can't be read: the workspace is gone) | |
| `workspace.member.updated` / `.removed` / `.left` | the member (name), role, `changes.role` from/to | the member's email (it's in the Members tab for current members) |
| `workspace.invitation.created` / `.revoked` / `.accepted` | the role and, while it isn't minimised, the invited address (owners already see it in the invitation list) | the address after 30 days (a keyed pseudonym: shown as "address no longer kept"), `.sent` events (delivery plumbing) |
| `project.created` / `.updated` / `.deleted` | project (`prj_`), name, description, status, colour, `changes` per field from/to (workspace content owners can already read) | |
| `workspace.api_key.*` and `auth.api_key.*` (this workspace's keys only) | key's `apk_` ID, the bounded fields the event holds (grants, changed-field names, fixed revocation reason) | secrets and digests (never in the payload anyway) |
| every row | time, actor (name; "Deleted account" once it's gone; "API key apk_…" for key writes), the event's name | **IP, user agent, request ID, `admin_request`, internal IDs, the raw payload** |
| every other `auth.*`, `admin.*`, `alert.*`, `rate_limit.reached`, `server.*`, `prune.*`, `email.*` | nothing | all: other accounts' sign-ins, admins' reads of the workspace (`admin.workspace.viewed` has no `workspace_id` and is excluded twice: by column and by name), limits, security events |

- **Scope by column, never by payload.** The query is `WHERE workspace_id = ?` (the member's workspace from `RequireWorkspace`) `AND name IN (allow-list)`. The payload's `workspace` field is not used to scope, so a crafted payload can't pull in another workspace's events.
- The API returns the projection (`details`), not `payload`. The web app renders it as a description list (label/value pairs, from/to for changes), never JSON.
- Names are the stable event names; labels ("Renamed the project", "Removed a member") live in the web app, one table keyed by name.

### API

`GET /api/workspaces/{workspace}/events`, behind `RequireSession`, `RequireWorkspace`, permission `workspace.event.index` (owners and admins; added to `grants` and to the web `permissions`, and to `workspaceRoutesMatrix`). Members and viewers get 403; non-members and unknown workspaces the same 404 as every workspace route.

| Param | Rule | Default |
| --- | --- | --- |
| `name` | Comma-separated catalogue names and owner-log group IDs (`project.*`, `workspace.member.*`, `workspace.invitation.*`, `workspace.api_key.*`, `workspace.*`), expanded in Go to `name IN (…)`; 400 for unknown, repeated or empty tokens, never `LIKE` | all allowed names |
| `actor` | `usr_…`: events that person caused (`actor_id`). A user who isn't (or wasn't) in this workspace matches nothing | all |
| `subject` | `usr_…`: events about a member (`user_id`: role changes, removal) | all |
| `project` | `prj_…`, matched on the payload's `project` | all |
| `q` | ≤ 100 chars, same rules and `LIKE` escaping as project search. Matches a fixed set of payload text fields (`name`, `description`, `changes.*.from/to`) with `ILIKE`, never the email or anything not shown | none |
| `window`, `from`, `to` | as the admin Events list (`1h`, `24h`, `today`, `yesterday`, `7d`, `all`; or RFC 3339 range), capped to the retention | `7d` |
| `before`, `perPage` | an `evt_` cursor (400 when the event is gone), 1–100 | newest, 50 |

Response: `{data, next, window, refs}` as the admin list (a [cursor page](../../../docs/api/pagination.md#cursor-pages-log-tables): nothing is counted, `perPage + 1` rows, `(created_at, id) < (?, ?)`); `refs` names the actor/subject/project filters. Each row: `id`, `name`, `createdAt`, `actor`, `subject`, `details`.

- **Indexes:** `events_workspace_created_idx (workspace_id, created_at DESC)` serves the scope, window and cursor; `events_actor_created_idx` the person filter. A workspace writes few events (tens a day), so `name`, `project` and `q` filter a bounded set and need no new index. A test checks the plan uses the workspace index.
- **Not recorded:** owners reading the log write no event (it would add a row per page view to the log being read). The admin reads of the workspace stay recorded as today.
- **Rate limit:** an ordinary read behind the session budget, as the Members list. No new limit (see Decisions).

### Store and layout

`internal/store/workspace_events.go` (`IndexWorkspaceEvents`, Bun builder only, `?` placeholders; the filter and cursor code shared with `admin_events.go` where it's the same, not copied), `internal/events/workspace_log.go` (the table, groups, projections), `internal/httpapi/endpoints/workspace/events.go` (`Events.Index`), registered in `router/routes.go`. Web: `lib/api/events.ts` (`api.events(workspace.id).index(params)`), `pages/app/[workspace]/settings/events/index.tsx` (`EventIndex`), tab in `components/app/workspaces/settings-tabs.tsx`, filter parsing in `components/app/workspaces/event-filters.ts`.

### Web

- **A tab** after API Keys, shown with `can(workspace, "workspace.event.index")`. Same page shape as the admin Events page: heading, date-range `Listbox`, **Filters** dialog (People: `Combobox` of members, Type: the groups and names, Project, Search) with removable badges, URL as the source of truth so back/forward walk filter changes, `CursorNav` Older/Newer, skeleton while loading, an empty state ("No changes match these filters"), an error with Retry that keeps the rows already loaded. Reuses `range-dialog.tsx` and the cursor/filter patterns in `components/app/admin/`; moves what both use into `components/app/events/` or `components/ui`, per the "reused UI" rule, instead of duplicating.
- **Rows** read as a sentence: "**Ada Lovelace** renamed the project *Website* to *Marketing site*". Each opens in place to Details: a description list of the projection (field, from → to). The row's person and project link back to the filter.
- A former member isn't in the Members list, so the People combobox searches current members; a row's actor name (including a former member's) adds a filter when clicked, which covers the rest without a second endpoint.

### Docs updated with the build

`api/endpoints.md` (route), `api/auth.md` (permission matrix), `api/events.md` (the owner log: allow-list, groups, what's hidden), `api/pagination.md` (the second cursor list), `api/testing.md`, `compliance.md` (remove the known gap, add owners as readers of workspace events in Retention/who-sees-it, note the 90-day window), `ui/pages.md`, `ui/api.md`, `ui/testing.md`, and the table in `AGENTS.md` if a doc is added.

## Security and privacy risks

| Risk | How it's avoided |
| --- | --- |
| Owner reads another workspace's events | Scope by `workspace_id` from `RequireWorkspace`; a test with two workspaces and a crafted `project`/`actor` filter |
| Owner sees other people's IPs, browsers, emails | Projection, not payload; IP, user agent and request ID are never returned; emails only where the owner already sees them, and not once minimised; a test walks the JSON for those keys and values |
| Owner sees admin or security events | Name allow-list, plus the `workspace_id` column (admin reads have none); test creates `admin.workspace.viewed`, `auth.logged_in`, `rate_limit.reached` rows with the workspace and expects none |
| A new event leaks automatically | Classification test fails until it's allowed or hidden with a reason |
| Search reveals hidden data (an email, a hidden field) | `q` only searches the fields the projection shows |
| Erasure / minimisation | Allowed events hold public IDs and workspace content; invitation emails follow the 30-day pseudonymisation (shown as removed); erased accounts show as "Deleted account" |
| Over-fetching | Cursor, `perPage` ≤ 100, window capped to retention, 10 s request timeout |
| SQL | Bun builder; names expanded from a Go allow-list; no input in SQL text |

## Tests

- Go integration (real PostgreSQL, factories): owner 200; admin, member, viewer 403; non-member and unknown 404; two workspaces never mix; hidden events (`auth.*`, `admin.*`, `rate_limit.reached`) never appear; no `ip`/`userAgent`/`requestId`/internal-ID keys and none of their values in any response; projection per allowed event (a table test over `WorkspaceLog`); `TestEveryEventIsClassifiedForOwners`; each filter; cursor stable across equal timestamps and 400 for a gone cursor; `q` escaping (`50%`); a minimised invitation hides its address; an erased actor; the plan uses the workspace index; the permission matrix and `workspaceRoutesMatrix`.
- Vitest: tab only for owners; filters round-trip through the URL; empty, loading, error-with-retry; a row's details; a former member's name filters.
- Playwright: an owner changes a project and a member's role, opens Logs, filters by person and type, searches, opens details; an admin of the workspace has no Logs tab and gets 403 directly.
- Seed: the demo seeder creates Acme's history through the real store methods so the tab isn't empty in development (no hardcoded UI data).

## Decisions

Confirmed by the user:

1. **Owners and admins**, through the permission `workspace.event.index` in `grants`, so who can read the log changes in one place. (Replaces "owner only" everywhere above; a viewer or member gets 403.)
2. **No IPs** (nor user agent or request ID).
3. **Retention stays as is**: workspace and project events 90 days, API-key events a year. The page says so.
4. **Personal API-key events are shown when they relate to the workspace.** `auth.api_key.created/updated/revoked` are recorded with the workspace's ID (and the key's owner as `user_id`), so they are in the allow-list, scoped by `workspace_id` like the rest; the view holds the key's `apk_` ID, owner's name, scopes and changed-field names only. Other `auth.*` events stay hidden.
5. **Reads of the log are not recorded.**
6. **Label "Logs", page route `events`** (`routes.events.index(ws)`, `EventIndex`, URL `settings/events`).
7. **No separate rate limit.**

## As built

- **Owners and admins**, not owners only: the permission `workspace.event.index` is in both roles' `grants`; the tab and page follow it. Everything in the plan that says "owners only" means that permission.
- **Date:** presets only in the web app (24 hours, 7 days (default), 30 days, everything kept). The API also takes `from` and `to`; a custom-range dialog was left out (the existing one lives in the admin build).
- **Project filter:** from a row's "Show this project's changes" (a removable badge); there is no project picker.
- **Person filter:** a listbox of current members (first 100), plus the person in the URL when they have left.
- **No catalogue endpoint:** the web app mirrors the Type groups (`types/events.ts`); an unknown group is a 400.
- **`CursorNav` moved** from `components/app/admin/` to `components/ui/`, now used by the admin pages and this one.
- **The web build's route list for client errors** (`lib/client-errors.ts`, `endpoints/account/client_errors.go`) gained the page.
- **Index-plan test:** not written; the scope uses `events_workspace_created_idx` by its shape.
- Docs updated: [endpoints](../../../docs/api/endpoints.md), [auth](../../../docs/api/auth.md), [events](../../../docs/api/events.md#the-workspace-log), [pagination](../../../docs/api/pagination.md), [api testing](../../../docs/api/testing.md), [compliance](../../../docs/compliance.md), [ui pages](../../../docs/ui/pages.md), [ui api](../../../docs/ui/api.md), [ui pagination](../../../docs/ui/pagination.md), [ui testing](../../../docs/ui/testing.md).

## Not included

A custom date-range dialog, a project picker, CSV export (owners; the admin one exists, could follow with its own limit and audit), real-time tail, notifications or webhooks on events, recording reads or sign-ins as workspace events, showing IPs, per-project history pages, undo/restore from a log entry, extending retention, an admin-role (non-owner) view.
