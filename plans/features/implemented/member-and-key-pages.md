# Event, member and API key pages

Status: **implemented 2026-11-03.** Decisions 1–8 confirmed by the user as recommended; built in the three parts, with the differences under "As built". Builds on [workspace audit log](workspace-audit-log.md), [workspace members](workspace-members.md), [API keys](api-keys.md) and the admin [Events page](admin/events-page.md).

## Why

The Logs tab shows a list, but an owner can't open one change, see its request ID to quote to support, look at one member's history, or look at one API key's. The API keys table is too wide (name, identifier, status, access, owner, dates, last use in six crowded columns). Members are named in many places with nowhere to go. The date picker and filter dialog exist only inside the admin build.

Three parts, each shippable on its own:

1. **Logs page rework:** a reusable time-range picker, a Filters dialog with badges, Request ID, an event view page and a row menu.
2. **Member page:** Overview, API Keys and Logs tabs, actions in a ⋯ menu, and links to it wherever a member is named.
3. **API keys:** a slimmer table, human-readable permission names and a view page (Overview, Logs) for workspace keys and personal keys.

## Part 1: Logs page

### Request ID, and what the event page's ID is

`events.request_id` already stores the request's `req_…` ID (the one in `X-Request-ID`, a 500's `requestId` and the server logs). It **can't be the page's ID**: one request can record several events (removing a member also revokes their personal keys: one `workspace.member.removed` and N `auth.api_key.revoked`, same request ID), and the column has no index. The event page therefore uses the event's own public ID, `evt_` + 14 base62, which is already what the admin Events page uses: public, unguessable, and never the internal bigint.

- The request ID is shown in the row's details and on the event page, with a copy button, labelled "Request ID". It goes in the API as `requestId` (empty for the CLI and listeners; those events have none).
- The event page lists **Other changes in this request** (same `request_id`, same workspace, allow-listed names, at most 20), each linking to its own page.
- Not added to the allow-list's search; no new filter. (Owners can't read server logs, so the ID is a reference for whoever can.)

### Reusable time range

`components/app/admin/range-dialog.tsx` and the date helpers it imports from `event-filters.ts` (`localDay`, `rangeLabel`, `utcAt`, `utcDay`) move to shared code: `lib/date-range.ts` (helpers) and `components/ui/range-dialog.tsx` (the dialog). A `TimeRange` component in `components/ui/time-range.tsx` is the `Listbox` of presets with **Custom range…** that opens the dialog, taking its presets, the value (`window` or `from`/`to`) and `onChange`. The admin Events page and the Logs page both use it; the admin page's behaviour and tests stay as they are. Logs gains custom ranges (the API already takes `from`/`to`, at most 366 days apart, and 90 days are kept).

### Filters dialog and badges

As on the admin Events page ([forms-and-actions.md](../../../docs/ui/forms-and-actions.md), List filters): the time range stays beside the heading, **Filters** (`outline`, `FunnelIcon`, a count badge) opens a `Dialog` with **Clear all** · Cancel · **Apply filters**: Type (the groups and names, a `Combobox`), Person (a `Combobox` of members; for a former member the filter shows their name from `refs`), Project (a `Combobox` searching the workspace's projects, replacing today's "Show this project's changes" button, which stays on the event page) and Search. Applied filters show as removable badges under the heading (names from `refs`), with Clear all from two, focus moving to the next badge or the Filters button as on the admin page. Search remains the one live field in the header (applied as typed).

What both pages need (the Filters button with its count, the badge row and its focus handling) is extracted into `components/ui/` or `components/app/filters/`, not copied; the admin page is moved onto it only if that is a small change (otherwise it keeps its own and the two are noted as duplicates).

### Event view page

`GET /api/workspaces/{workspace}/events/{event}` (new permission `workspace.event.show`, owners and admins), `EventShow` at `/app/:workspace/settings/events/:event`: heading with the sentence, a description list (When (UTC), Done by, About, Request ID, the projection's fields, from/to for changes) and **Other changes in this request**. The same projection as the list, so the same safety rules: scoped by `workspace_id`, allow-listed names, an event of another workspace or a hidden one is the same 404 as a missing one, no IP, user agent or stored payload. Reading it records nothing.

The list gets a ⋯ `Dropdown` in the last column (`TableCell className="w-0 text-right"`) with **View event**; the time stays a link to the same page. Expanding a row still shows its details in place.

## Part 2: Member page

`GET /api/workspaces/{workspace}/members/{member}` (new permission `workspace.member.show`, owners and admins, as `workspace.member.index`) → the member as the list gives it. Page `MemberShow` at `/app/:workspace/settings/members/:member`, a page of its own (not a tab of the settings layout):

- **Header:** the member's name, a `RoleBadge`, and a ⋯ `Dropdown` at the right: **Change role**, **Remove from workspace** (danger), opening the existing dialogs and shown only when the role may be managed (as today, never on yourself). The dialogs move from the members page into `components/app/members/` so both pages use them; Remove returns to the Members list, Change role refreshes the page.
- **Tabs** (`Navbar`, like the settings tabs): **Overview** (name, email, role, joined, counts of live personal keys and changes they made), **API Keys** (their personal keys in this workspace: the key list below with `owner=usr_…`), **Logs** (the log filtered to them: what they did or what was done to them).
- **API:** the workspace key list gets an `owner` filter (`usr_…`, personal keys of that member in this workspace), and the events list an `account` filter (`usr_…`: `actor_id` or `user_id`, as the admin Events page's Account) with the same checks; both stay scoped to the workspace.
- **Links:** every place that shows a member's name links to their page when the viewer has `workspace.member.show`: the Members table, an invitation's "invited by", Logs rows (actor, subject), the key list's Owner and the event page. A name for someone who is not (or no longer) a member links nowhere unless Decision 3 says otherwise.

## Part 3: API keys

### Table

Today: Name/identifier/status stacked, Access/permissions, Owner, Created/expiry, Last used, actions. New columns:

| Column | Shows |
| --- | --- |
| Name | the key's name, truncated, linking to its page (no ID or kind line) |
| Status | a `Badge`: **Active** green, **Expired** and **Revoked** zinc (red for revoked if you prefer) |
| Access | one grouped badge: **All projects: List, View** or **Selected projects: List, View, Create, Update, Delete**; "No selected projects remain" when empty |
| Owner / Workspace | name, truncated; a member links to their page |
| Last used | date and time, "Never" |
| ⋯ | Edit, Revoke (as now), and **View** |

Created and expiry move to the key page. Cells truncate (`truncate`, a `max-w-*` on the text cells, a `title` with the full text), and Access and Last used hide on small screens (`max-lg:hidden`) so a row never exceeds the page width.

- **Human names for actions** in one table in the web app (`types/api-keys.ts`): `project.index` List, `project.show` View, `project.store` Create, `project.update` Update, `project.destroy` Delete. The API's scope strings don't change; the form's checkboxes use the same labels. "Some actions are unavailable under the owner's current role" stays as a short note on the page and a tooltip in the table.
- **Badge colour:** `Badge` gets `green` (the existing greens are `lime` and none for success). Added to the component, with its dark variant, like the other colours.

### Key page

`GET /api/workspaces/{workspace}/api-keys/{key}` (`workspace.api_key.show`, owners and admins; either kind in this workspace) and `GET /api/auth/api-keys/{key}` (the signed-in owner's own personal key) return the key's existing metadata, plus selected project names. Today `api-keys.md` says no per-record GET exists; this adds it with the same metadata, never a secret, digest or email, on the same management read budget (`ApiKeyManagementReadPerUser`).

Pages: `WorkspaceApiKeyShow` at `/app/:workspace/settings/api-keys/:key` and `UserApiKeyShow` at `/app/settings/api-keys/:key`, each with tabs **Overview** (kind, status, owner or creator, workspace, project access with names, saved and effective actions, created, expiry, last used, revoked) and **Logs**, and a ⋯ menu with Edit and Revoke where allowed.

**Logs** are that key's events only: `workspace.api_key.*` / `auth.api_key.*` for it (payload `key`) and the project writes it made (events carrying `api_key_public_id`). `GET …/events` takes a `key` filter for owners and admins; for the personal key's owner, who needs no log permission, `GET /api/auth/api-keys/{key}/events` returns the same projection **forced to that key** (their own key, in a workspace they still belong to, and nothing else from the log). Reads by a key (listing projects) aren't recorded, only writes and lifecycle, so the tab says so and **Last used** is the sign of reads.

## Security and privacy

| Risk | How it's avoided |
| --- | --- |
| Request ID as the page ID leaks or collides | It isn't the ID; `evt_` is, scoped by workspace and allow-list |
| Event page used to find other workspaces' events | Same query scope, same 404 for missing, foreign and hidden; a test with two workspaces |
| A member's key log shows other people's events | The personal-key endpoint is forced to the key's own events and name allow-list; the session user must own the key |
| Member page shows a former member's data to the wrong people | Same permission as the Members list; names only, no email for former members |
| New permissions drift | `grants`, the web `permissions`, the matrix test and `TestPermissionNames` cover each |
| Key page leaks a secret | Same metadata as the list; the plaintext exists only in the creation response |
| Wide tables | Truncation and hidden secondary columns, checked at phone and desktop width |

## Tests

Go integration: each new route for every role and an outsider; event show (same 404 for foreign, hidden and unknown IDs; request ID present, no IP/agent; other-changes-in-request scoped); `owner` and `account` and `key` filters (scope, unknown person matches nothing); personal-key logs only for the owner and only that key's events; key show (metadata equals the list's; a member can't read another's personal key; revoked and expired keys readable). Vitest: the shared range picker (admin and logs), the filters dialog and badges, the event page, the member page's tabs and actions, key table columns (truncation classes, status colour, grouped badge text) and action labels. Playwright: an owner opens a change from the ⋯ menu and sees its request ID; opens a member's page, changes their role from the menu, sees the change in their Logs; opens a key's page and its Logs.

## Decisions

1. **Event page ID:** use `evt_…`, show the request ID as a reference and link the others from the same request. (Recommended; the request ID can't identify one event.)
2. **Key table columns:** Name, Status, Access, Owner/Workspace, Last used, ⋯; created and expiry move to the key page. (Recommended.)
3. **Former members:** their member page works when they still appear in the workspace's log (Overview says "No longer a member", no actions, Logs and past keys visible), instead of a 404. (Recommended: audit is the point.)
4. **Personal key logs for its owner:** a key's own events only, through `/api/auth/api-keys/{key}/events`. (Recommended.)
5. **Badge colour `green`** added to `Badge`; revoked and expired are zinc. (Recommended.)
6. **Member Overview:** name, email, role, joined, live personal key count, changes made. No "last active" (the account's last-seen time spans every workspace). (Recommended.)
7. **Admin pages move onto the shared range picker and filter pieces** where it is a small change. (Recommended.)
8. **Order:** build in the three parts above, each reviewed before the next. (Recommended.)

## As built

- **Tabs** are `?tab=` on one page (`PageTabs`, `useTab`), not nested routes: the member and key pages' Overview, API Keys and Logs tabs keep their own filters off each other's URLs, and back and forward walk through them.
- **Filters dialog:** Type is the multi-select `Combobox` of groups and events (a fully picked group is one chip and one URL token); Person and Project are `Listbox`es of the first 100 members and the first 100 projects by name (plus the applied one), not a searching `Combobox`; a larger workspace reaches a project from its change's page. The Logs search is applied as typed, outside the dialog.
- **Search** was reworked in the same change (the user's report: "api key" found nothing): every word must match an event's label or name, a field a row shows, or the name of who did it or whom it is about.
- **Key form:** its permission picker still says "Read" (list and view together), as a form's choice; the table and key page spell the API's scopes as List, View, Create, Update and Delete.
- **Shared pieces:** `lib/date-range.ts`, `components/ui/range-dialog.tsx`, `time-range.tsx`, `filter-bar.tsx`, `page-tabs.tsx`, `cursor-nav.tsx`; the admin Events page uses `TimeRange`, `FiltersButton` and `FilterBadges`, and the admin Alerts page the moved `RangeDialog`.
- **Member Overview** counts live personal keys and changes made; a former member's page has their name only. Their API Keys tab lists their personal keys here with `owner=`; their Logs tab uses `account=` and hides the Person filter.
- **Request ID** is in the list's rows as well as the change's page (`requestId`); the page's "Other changes in this request" are the allowed events of this workspace with the same `request_id`.
- **Route registries** also gained the four new pages (`returnRoutes`, `clientRoutePatterns` in the web and in `client_errors.go`).
- Docs updated: [endpoints](../../../docs/api/endpoints.md), [auth](../../../docs/api/auth.md), [api-keys](../../../docs/api/api-keys.md), [events](../../../docs/api/events.md#the-workspace-log), [compliance](../../../docs/compliance.md), [api testing](../../../docs/api/testing.md), [ui pages](../../../docs/ui/pages.md), [ui api](../../../docs/ui/api.md), [ui design](../../../docs/ui/design.md), [forms and actions](../../../docs/ui/forms-and-actions.md), [ui testing](../../../docs/ui/testing.md).

## Not included

A page per project, editing a key from its page's Overview, key rotation, exporting a member's or a key's log, showing IPs, recording reads by a key, admin dashboard changes beyond adopting the shared pieces.
