# Admin: the Events page, redesigned

Status: **implemented 2026-11-01**, with every recommendation in the Decisions table confirmed as written (the user's go-ahead of 2026-11-01); see [How the build differs](#how-the-build-differs). Plan A10 of the [admin plans](../../admin/README.md). Builds on [events and mail](events-and-mail.md) (the page as it was), keeps the [rules every admin plan keeps](../../admin/README.md#rules-every-admin-plan-keeps), and draws its charts the way [monitoring](monitoring.md) and [events and mail decision 12](events-and-mail.md#charts-decision-12) do. Docs updated: [admin.md](../../../../docs/api/admin.md), [events.md](../../../../docs/api/events.md), [endpoints.md](../../../../docs/api/endpoints.md), [database.md](../../../../docs/api/database.md), [pagination.md](../../../../docs/api/pagination.md), [seeding.md](../../../../docs/api/seeding.md), [api/testing.md](../../../../docs/api/testing.md), [testing.md](../../../../docs/testing.md), [ui/design.md](../../../../docs/ui/design.md), [ui/base-ui.md](../../../../docs/ui/base-ui.md), [ui/forms-and-actions.md](../../../../docs/ui/forms-and-actions.md), [ui/pages.md](../../../../docs/ui/pages.md), [ui/pagination.md](../../../../docs/ui/pagination.md), [ui/README.md](../../../../docs/ui/README.md), [ui/testing.md](../../../../docs/ui/testing.md).

## Why

The Events page works, but it reads like a debugging tool:

- **Filtering is slow.** Eight fields sit open above the table. The event field takes one name or one whole area, so "failed sign-ins and MFA checks" or "everything about invitations except sends" can't be asked in one view.
- **Time is two UTC date boxes.** "The last hour" or "yesterday" means typing dates, and nothing shorter than a day can be picked.
- **There is no picture.** An operator can't see that sign-in failures tripled at 14:00, or which events fill the log, without paging through rows.
- **Applied filters are hard to see.** They live in the form's fields, so nothing says at a glance what the list is showing.

The user's notes of 2026-11-01 ask for a picker that selects several events and whole groups with search, one time dropdown, a Filters dialog with removable badges, and graphs: "more professional and production ready". This plan does that without breaking the dashboard's rules: one recorded read per load, bounded aggregates, and admin code only in the admin build.

## The page

```
Events                                        [Last 24 hours ▾]  [⛛ Filters  3]
Oct 31, 14:30 – Nov 1, 14:30 UTC
[Events: auth.*, workspace.invitation.created +1  ×] [Failed checks only ×] [IP: 203.0.113.0/24 ×]  Clear all

 Events          Failed checks       Limits reached     Busiest
 12,345          312 · 8% of sign-ins  4                 14:00–14:30 · 841

 Events over time (stacked by group: auth.* · workspace.* · admin.* · project.* · Other)
 ▁▂▂▃▅▇█▆▄▃▂▂▁ …

 Failed checks over time              | Top events
 (sign-in · second factor · password  | auth.login_attempted      4,021 ████████
  · sign-up code · admin door)        | workspace.invitation.sent   902 ██
                                      | …

 ▸ Time (UTC)          Event                              Actor        About        IP
 ▸ Nov 1, 14:29:58     auth.login_attempted  [Failed]     –            Ada L.       203.0.113.9
                       wrong_password · ada@example.com
                                                                        [Newer] [Older]
```

- **Header** ([forms-and-actions.md](../../../../docs/ui/forms-and-actions.md), page header row): the heading on the left; on the right the time range (a `Listbox`, `aria-label="Time range"`) and the **Filters** button (`Button outline`, `FunnelIcon`, the word "Filters" and, when any are applied, a zinc `Badge` with their count; decision 3). Below the heading, a `Text` line with the window the server resolved, in UTC.
- **Badges** for the applied filters, then the summary, then the list. There is no Refresh button (the user's note of 2026-11-01); a browser reload, or picking a range again, loads again.

## Filters

### Event names: a picker with groups (decision 1)

**Groups come from the catalogue**, defined once in Go beside it (`internal/events/groups.go`, `events.Groups()`), and served by `GET /api/admin/catalogue` (still not recorded: names only). Built from the names, so a new event joins its group without anyone remembering:

| Group ID | Label | Holds |
| --- | --- | --- |
| `auth.*` | Accounts and sign-in | every `auth.` name; sub-groups `auth.registration.*`, `auth.email_verification.*`, `auth.password_reset.*`, `auth.mfa.*`, then the single names (`auth.login_attempted`, `auth.logged_in`, …) |
| `workspace.*` | Workspaces | sub-groups `workspace.invitation.*`, `workspace.member.*`, then `workspace.created`, `.renamed`, `.deleted` |
| `project.*` | Projects | `project.created`, `.updated`, `.deleted` |
| `email.*` | Email addresses | `email.blocked`, `email.unblocked` |
| `rate_limit.*` | Rate limits | `rate_limit.reached` |
| `admin.*` | Admin | every `admin.` name **except reads**; sub-group `admin.session.*` |
| `admin-reads` | Admin reads | the 11 `admin.<resource>.listed` and `.viewed` names |

- **A sub-group** is a three-segment prefix with two or more names (`auth.mfa.*`); a prefix with one name (`auth.recovery_codes.created`) is just that name. Every name is in exactly one group, checked by `TestEventGroups`.
- **Admin reads are their own group** (decision 7), as the page hides them today: picking **Admin** shows admin sign-ins, sessions, grants and the Unblock action, not thousands of reads. `admin-reads` can't clash with a name (the `events.name` CHECK allows no `-`).
- **Nothing picked** means every event except admin reads, as today. The picker says so: "All events except admin reads".

**The picker** is `Combobox` (decision 13), a new `components/ui/combobox.tsx` on BaseUI's `Select` with `multi`, `searchable`, `filterOutSelected={false}`, `closeOnSelect={false}`, `onSelectResetsInput={false}` and `backspaceRemoves={false}`, styled with `slot()` like `Listbox` (its classes, `Highlight`, `popoverOverrides`, `usePopoverScrollLock`):

- **Props:** `value: string[]` (the leaf values), `onChange(value: string[])`, `options: ComboboxNode[]` (`{ value, label, text, children? }`: a tree), `placeholder`, `aria-label`; it reads `useField()` for its label and description, as `Listbox` does. Nothing in it knows about events.
- **Rows:** the tree flattened in order, indented by depth (`pl-4`, `pl-8`). Each row draws a check box: checked, empty, or **mixed** (a dash) when some of a group's names are picked. A group row also shows how many names it holds ("22"). The row's text for screen readers says it: "auth.mfa.*, group, 1 of 3 selected".
- **Choosing a group** picks every name under it, or clears them all when they're all picked. BaseUI's multi `Select` already toggles an option chosen twice (`selectValue`); `Combobox` turns a group's toggle into its names before calling `onChange`.
- **Search** matches a name or group label, ignoring case. A group stays listed while any of its names match (so the match is shown in context), and a matching group lists all its names.
- **Keyboard** is BaseUI's: type to search, ↑ ↓ to move, Enter to toggle, Esc to close. Backspace never removes a choice (the chips can hide some).
- **The closed control** shows the choice compressed by group (a fully picked group is one chip, `auth.*`), at most two chips and then "+N", each chip removable with its own × (`aria-label="Remove auth.*"`).

### Time: one dropdown (decision 2)

| Option | Window (UTC) | URL `window` |
| --- | --- | --- |
| Last hour | the last 60 minutes | `1h` |
| **Last 24 hours** (default) | the last 24 hours | `24h` (left out of the URL) |
| Today | since 00:00 UTC | `today` |
| Yesterday | the whole UTC day before | `yesterday` |
| Last 7 days | the last 168 hours | `7d` |
| All time | no bound; the list only, no charts | `all` |
| Custom range… | the days picked, or the column clicked | `from`, `to` |

- **UTC, labelled** (decision 5), like every other time in the dashboard (`formatDateTime`), so a link shared between admins in different places shows the same rows and the event page agrees with the list. Options read "Today (UTC)" and "Yesterday (UTC)"; the caption under the heading always says "UTC".
- **The server resolves the window** from its own clock, so the browser's clock can't skew it, and the response echoes `window: {from, to}` for the caption. A relative window starts on its first bucket's boundary (below), so "Last 24 hours" at 14:47 starts at 14:30 and the chart's first column is whole. `to` is the request's time.
- **Custom range…** opens a small `Dialog` ("Custom range", `size="sm"`) with `Calendar` (decision 14, below), the picked days written out ("Oct 20 – Oct 23, UTC"), Cancel and **Apply range**. The URL gets `from` (the first day's 00:00 UTC) and `to` (00:00 UTC after the last day: exclusive, as today). Days from 365 days ago to today; later than today can't be picked. While a custom range is applied, the `Listbox` shows it as its value ("Oct 20 – Oct 23 UTC", or "Nov 1, 14:00 – 14:30 UTC" when it isn't whole days), and choosing it again reopens the dialog.
- **Ranges of any length list rows**; **charts need 7 days or less** ([Graphs](#graphs)). "All time" and a longer custom range show the list with one line where the charts would be: "Charts cover 7 days or less." and a `TextButton` "Show the last 7 days".
- **`Calendar`** is a new `components/ui/calendar.tsx` on BaseUI's `Calendar` from `baseui/datepicker` (`range`, `value`, `onChange`, `minDate`, `maxDate`, one month on phones and two from `sm:`), its slots replaced with `slot()`: zinc day cells, the range in `bg-blue-500` like a selected `Listbox` option, `data-focus` rings, `dark:` variants. It comes with `baseui` (with its `date-fns` adapter), so no new dependency. **Pitfall:** it hands back local `Date`s at local midnight; the page reads their year, month and day (`getFullYear`, …) and builds UTC days from them, never `toISOString()`, which shifts the day east of UTC.

### Outcome: failed checks only (decision 12)

"Only failed checks" (a `Listbox` in the dialog: Any outcome / Failed checks only; URL `outcome=failed`) keeps the events that record a failed check, by one fixed table in the store, `adminEventFailures`, which the chart, the rows' **Failed** badge and this filter all read, so the three never disagree:

| Kind | Label | Event | Failed when |
| --- | --- | --- | --- |
| `sign_in` | Sign-in | `auth.login_attempted` | `result` is `wrong_password` or `unknown_account` |
| `second_factor` | Second factor | `auth.mfa.checked` | `ok` is false |
| `password` | Password checks | `auth.password_checked` | `ok` is false |
| `sign_up_code` | Sign-up codes | `auth.registration.checked` | `ok` is false |
| `admin_door` | Admin door | `admin.login_attempted`; `admin.access_refused` | `result` is `wrong_password`, `unknown_account`, `not_admin` or `mfa_off`; always |

Each row is a name list and a fixed SQL condition (`e.payload->>'ok' = 'false'`, `e.payload->>'result' IN (…)` with the values as `?` placeholders). Nothing in it comes from input.

### The other filters

Account (`usr_…`), Workspace (`wsp_…`), Email and IP address or range keep today's rules, fields and checks (`problem()` in the page: "An account ID starts with usr_.", a CIDR no wider than /16 or /32). One value each, as now.

### Where filters live: the Filters dialog and badges (decision 3)

- **Filters** opens a `Dialog` (`size="xl"`), "Filter events": Events (the `Combobox`, with the description "Pick events or whole groups. Admin reads show only when picked."), Outcome, then Account, Workspace, Email and IP in a two-column grid from `sm:`. Actions in `DialogActions`: **Clear all** (`plain`: empties the dialog's fields, applies nothing), **Cancel** (`plain`), **Apply filters** (solid, last). A field's problem shows under it on Apply and nothing is applied. Opened again, the dialog starts from what's applied (keyed by `useOpenCount`). Every Apply is one recorded read, so nothing applies as you type.
- **The time range is not in the dialog** (decision 15): it's the filter changed most and the charts depend on it, so it stays one click away in the header and applies when chosen.
- **Badges** below the heading, one per applied filter, in the dialog's order: `Badge` gains `onRemove` and `removeLabel` (decision 19), drawing a small × button inside it (`XMarkIcon`, 16px, `aria-label="Remove filter: IP 203.0.113.0/24"`). Removing applies at once. Clicking a badge's text opens the dialog. After two or more, **Clear all** (`TextButton`) removes every filter but the time range.

| Filter | Badge |
| --- | --- |
| Events | `Events: auth.*, workspace.invitation.created +1` (the same compression as the chips: groups first, two shown, then "+N"); a `Tooltip` lists every token |
| Outcome | `Failed checks only` |
| Account, Workspace | `Account: Ada Lovelace`, `Workspace: Acme`: the names from the response's `refs` (decision 18); the ID alone when the record is deleted or unknown |
| Email, IP | `Email: ada@example.com`, `IP: 203.0.113.0/24` |

### URL state

The page URL holds everything, so a link is shareable and back and forward walk through filter changes (each a navigation with `preventScrollReset`):

```
/admin/<key>/events?window=7d&name=auth.*,workspace.invitation.created&outcome=failed&ip=203.0.113.0/24&before=evt_…
```

- **`name`** is one comma-separated list of tokens: a group ID (`auth.*`, `auth.mfa.*`, `admin-reads`) or a catalogue name. The page writes it **canonically**: a fully picked group as its ID, then the names left over, in catalogue order. At most 64 tokens (the catalogue has 60 names), so the longest canonical value is under 2 KB, well inside every browser's and proxy's limit. `*` and `,` need no escaping in a query string.
- **The parser forgives, the API doesn't.** Reading the URL, the page drops what it doesn't understand (an unknown token, a malformed ID), as `eventFilters` does today, so a hand-edited link still shows a list; the API answers anything it doesn't accept with 400.
- **Email and IP stay in the URL**, as today and as [email blocks](email-blocks.md) accepted for `?email=`: admin pages send `Referrer-Policy: no-referrer`, and our own logs never store query strings ([observability](observability.md)). A proxy in front may log the API's query string; that is no different from today, and `docs/deployment.md` already tells operators what their proxy logs.
- **`window`** is left out for the default (`24h`); `window` and `from`/`to` never appear together.
- **Links in** from other pages pass their own window: a user's and a workspace's **Activity**, and Email blocks' **Show activity**, pass `window=all` (the account's whole history, as the link shows today); Mail's frequent addresses pass Mail's window (`24h` or `7d`); an IP in the Events list keeps the current window and filters and adds the IP.

## API changes

### `GET /api/admin/events`

| Param | Rule | Default |
| --- | --- | --- |
| `name` | Comma-separated tokens: catalogue names and group IDs from `events.Groups()`. **400** for an unknown token, an empty one (`a,,b`), a repeated one, more than 64, or a value over 4,096 bytes. Expanded in Go to the union of their names, so the query is `e.name IN (?)` over at most 60 known names; never `LIKE` | every name except admin reads |
| `outcome` | `failed` ([above](#outcome-failed-checks-only-decision-12)) | any |
| `account`, `workspace`, `email`, `ip` | As today | none |
| `window` | `1h`, `24h`, `today`, `yesterday`, `7d`, `all` | **`24h`** (decision 6) |
| `from`, `to` | RFC 3339, `from` < `to`; instead of `window` (400 with it). One alone is open-ended | none |
| `summary` | `include`: add the [summary](#graphs); refused (400) with `before` | none |
| `before`, `perPage` | As today | newest, 50 |

- **Changed from today:** `name` takes several tokens and groups are `auth.*`, not `auth.`; `adminReads` is gone (pick the `admin-reads` group, or a read by name) and is now an unknown parameter (400); the default window is 24 hours, not all time. Nothing outside the dashboard links here, and the dashboard's own links change in the same commit.
- **Response:** `{data, next, window, refs, summary?}`. `window` is `{from, to}` as resolved, or `null` for `all`. `refs` names the account and workspace filters, `{account: {id, name, email} | null, workspace: {id, name} | null}` (the account filter already looks the `usr_` up; the workspace is one lookup by public ID, `{id}` alone once deleted), so the badges read as names. Each row gains `failed: true | false` from `adminEventFailures`, computed in the same `SELECT` (`CASE WHEN … END AS failed`).
- **Recorded as today**, one `admin.event.listed` per request, with `filters` now `{names: [tokens], outcome, account, workspace, email, ip, window, from, to (as resolved), before, summary: true}`, only the ones used. `names` replaces the old `name` string (older rows keep their shape until pruned).

### `GET /api/admin/catalogue`

Gains `eventGroups`: `[{id, label, names: [...], groups: [{id, label, names: [...]}]}]`, in catalogue order. Still names only, still not recorded.

### Query shape and indexes

- **Names:** `e.name IN (?)` with `bun.In`, or `e.name NOT IN (?)` with the reads when nothing is picked (as today). Both use `events_name_created_idx (name, created_at DESC)` or the time index, as the planner chooses.
- **Outcome:** `(e.name = ? AND <condition>) OR …` over `adminEventFailures`'s rows, built from the fixed table, values as placeholders; the name index finds the five names, then the payload is read.
- **Time:** `e.created_at >= ? AND e.created_at < ?`.
- **One index changes** (decision 11): `events_created_idx` becomes `ON events (created_at) INCLUDE (name)`, so the unfiltered summary (the default view) counts by bucket and name from the index alone (an index-only scan), without reading 7 days of rows. It still serves prune's `created_at < cutoff`. Edited into `007_create_events` while migrations are squashed. **A3 (observability) is editing that file now** (`request_id`): make this change on top of it.
- **All time with a rare name** can make the planner walk the time index backwards past millions of rows. Check it with `EXPLAIN` on the events seeder (below); if it's slow, the store pages each name's own index and merges (a `UNION ALL` of `… WHERE e.name = ? ORDER BY created_at DESC LIMIT n` per name, via Bun's `UnionAll`), which every name index answers in a few rows. The request's 10 s timeout cancels the query either way.

## Graphs

Decided per the user's notes 2026-11-01 (decision 4). What earns its place:

| Part | Shows | Why |
| --- | --- | --- |
| **Four tiles** (`Stat`) | **Events** (the total); **Failed checks** (their total, and "N% of sign-ins" when there were sign-in attempts); **Limits reached** (`rate_limit.reached`); **Busiest** (the fullest bucket and its count) | the numbers someone reads first; each is from the counts below, no extra query |
| **Events over time** (`BarChart`, stacked) | events per bucket, stacked **by group** (the four largest groups in the window, then Other); when every picked name is in one group, **by name** (the four largest, then Other) | spikes and their source at a glance |
| **Failed checks over time** (`BarChart`, stacked) | the five failure kinds per bucket | attacks on sign-in, MFA, passwords and the admin door, which the summary's totals would hide among ordinary events; hidden when no picked name can fail |
| **Top events** (`BarList`, new) | the 10 names with most events, with counts and share | what fills the log; each row links to the page filtered to that name |

- **Five series at most**, from `chart.tsx`'s fixed order (sky, orange, emerald, violet, pink); "Other" folds the rest, as the chart's rules say. The failure kinds are exactly five. No new colours, so no new validator run.
- **Clicking a column** narrows the window to that bucket (a custom range, `from`/`to`) (decision 16): `BarChart` gains an optional `onSelect(row)`, with `cursor-pointer` on the columns. The tooltip says "Click to zoom in". The custom range does the same from the keyboard.
- **`BarList`** joins `components/ui/chart.tsx` (decision 17): plain HTML, not Recharts: an `<ol>` of rows, each a link with the name, the count (`tabular-nums`) and its share, over a bar whose width is its share of the largest (`bg-sky-600/15 dark:bg-sky-500/20`). A list of links is readable as it is, so it needs no hidden table.

### Served with the list: one request, one recorded read (decision 9)

- **`summary=include`** on the **first page only** (no `before`). The page asks for it with every new window or filter; Older and Newer don't, and the page keeps showing the summary it got for the same filters (in component state keyed by the filters without the cursor; never module state). Arriving at an `Older` link directly, the charts say "Charts show with the newest events." with a `TextButton` "Show newest".
- **The same `admin.event.listed`** records it (`filters.summary: true`), so opening the page or changing a filter is still one read.
- **The summary answers apart from the list:** the list always comes back; the summary carries a `status`:

| `status` | When | The page shows |
| --- | --- | --- |
| `ok` | computed | tiles and charts |
| `too_wide` | the window is over 7 days, or `all` | "Charts cover 7 days or less." · Show the last 7 days |
| `too_slow` | its 5 s deadline passed | `Notice variant="warning"` "Charts took too long. Add filters or pick a shorter range." with Retry |
| `busy` | two summaries are already being computed on this server | the same notice, "Charts are busy. Try again in a moment.", with Retry |

```json
{
  "data": [{ "id": "evt_…", "name": "auth.login_attempted", "failed": true, "…": "…" }],
  "next": "evt_…",
  "window": { "from": "2026-10-31T14:30:00Z", "to": "2026-11-01T14:47:12Z" },
  "refs": { "account": null, "workspace": null },
  "summary": {
    "status": "ok",
    "bucketSeconds": 1800,
    "total": 12345,
    "stackedBy": "group",
    "series": ["auth.*", "workspace.*", "admin.*", "project.*", "other"],
    "buckets": [{ "start": "2026-10-31T14:30:00Z", "counts": [12, 3, 0, 1, 0] }],
    "topNames": [{ "name": "auth.login_attempted", "count": 4021 }],
    "failures": {
      "total": 312,
      "series": ["sign_in", "second_factor", "password", "sign_up_code", "admin_door"],
      "buckets": [{ "start": "2026-10-31T14:30:00Z", "counts": [3, 0, 0, 0, 1] }]
    },
    "limitsReached": 4,
    "signInAttempts": 4021
  }
}
```

`counts` line up with `series`; every bucket in the window is listed, empty ones as zeros. The page labels series: group labels from the catalogue, names as they are, failure kinds from a map in `types/admin.ts`.

### Buckets

| Window length | Bucket | Columns at most |
| --- | --- | --- |
| up to 1 hour | 1 minute | 60 |
| up to 6 hours | 5 minutes | 72 |
| up to 2 days (24 h, today, yesterday) | 30 minutes | 96 |
| up to 7 days | 3 hours | 56 |

Aligned to UTC (`date_bin(?, e.created_at, '2000-01-01T00:00:00Z')`, the interval from this table as a placeholder). Labels: "14:30" up to 2 days, "Oct 31 15:00" beyond.

### Queries

Two, both under the filters and the window, in `internal/store/admin_event_summary.go` (`SummarizeAdminEvents`):

1. **Counts:** `SELECT date_bin(…) AS bucket, e.name, count(*) … GROUP BY bucket, e.name`. At most 96 buckets × 60 names of rows back. Go folds them into the tiles, the stack (four largest plus Other, by group or by name), the top 10 and `limitsReached`; the folding is pure functions with unit tests.
2. **Failures:** the same, restricted to `adminEventFailures`'s names, grouped by bucket and a `CASE` naming the kind. It reads payloads, but only of those names (through the name index).

Both run on the request's context with a **5 s deadline** (monitoring's), after the list. **At most two summaries at once per server** (decision 10: a buffered channel, waited on for at most 1 s, then `busy`), so a burst of wide charts can't take the 10-connection pool from the app. Neither is a rate limit: `admin-read:per-user` still counts the request once.

### States and accessibility

- **Loading:** tiles show "–" (as `Stat` does), each chart a placeholder of its height (`animate-pulse`, `motion-reduce:animate-none`, `aria-label="Loading chart"`), the list `ListSkeleton`. Changing filters keeps the last answer dimmed with `aria-busy` (as `useAdminResource`'s `kept` data does today).
- **Empty:** no rows means no charts; the list's `EmptyState` says why: with filters, "No events match" and "Try fewer filters or a wider range." with **Clear filters**; without, "No events in the last 24 hours" with **Show the last 7 days**.
- **Error:** the request failing is the `Notice variant="error"` with Retry above the summary, keeping what was shown (rule 8). There's no Refresh button.
- **Accessible:** each chart keeps `chart.tsx`'s `summary` sentence ("12,345 events in the last 24 hours, most at 14:00 UTC (841).", "312 failed checks, most of them sign-ins (280).") and its `sr-only` table; the SVG stays `aria-hidden`. `BarList` is a list of links. Animation is off under reduced motion (as today). Light and dark through the existing classes.

## The list

| Column | Shows |
| --- | --- |
| (disclosure) | a `plain` chevron button, `aria-expanded`, `aria-controls`: opens the row (below) |
| Time (UTC) | to the second ("Nov 1, 14:29:58"), linking to the event's page |
| Event | the name; a rose **Failed** `Badge` when `failed`; the one-line summary under it (`eventSummary`, as today) |
| Actor | as today (from `sm:`) |
| About | as today (from `md:`) |
| IP | as today (from `lg:`); the link keeps the current window and filters |

- **A row opens in place** (decision 21): a row below it across every column with the payload as indented JSON in a `<pre>` (React's escaping, as the event page) and **Open event**. The list already returned the payload, so this loads and records nothing more. The event's page stays for deliveries, related events and the user agent (its own recorded read).
- **The event's page** gains "More like this" links: events with this name, from this IP, by this account (each `window=7d`).
- **Cursor pagination stays** (`CursorNav`, Older / Newer, `before` in the URL, the trail in navigation state). With a relative window, each page resolves "now" when loaded, so the far end of an older page can move by the seconds between loads; the cursor keeps rows from repeating.

## Web app files

| File | Holds |
| --- | --- |
| `pages/admin/[key]/events.tsx` (or `events/index.tsx` if the page-folders plan lands first) | `AdminEventIndex`: the header, the range `Listbox` and the custom range dialog, the Filters dialog, the badges, the summary (tiles and charts), the list and its states, as named local functions (rule 3: used by this page alone) |
| `pages/admin/[key]/events/[event].tsx` | "More like this" |
| `components/app/admin/event-filters.ts` | Shared by the events page, the event page and the links in (user, workspace, Mail, Email blocks): `eventFilters(search)` (the forgiving parser), `compressNames` / `expandTokens` (names ↔ canonical tokens), `selectionLabel` (the chip and badge text), `windowLabel` |
| `components/app/admin/events.tsx` | `EventTable` with the disclosure, the Failed badge and seconds |
| `components/ui/combobox.tsx`, `calendar.tsx` | `Combobox`, `Calendar` (generic; only admin pages import them for now, so they're only in the admin build) |
| `components/ui/chart.tsx` | `BarChart onSelect`, `BarList` |
| `components/ui/badge.tsx` | `Badge onRemove removeLabel` |
| `lib/api/admin.ts`, `lib/admin-routes.ts`, `types/admin.ts` | the new query (`EventFilters` with `names`, `outcome`, `window`, `summary`), `AdminEventPage` (`CursorPage<AdminEvent>` plus `window`, `refs`, `summary`), `EventSummary`, `EventGroup`, failure kind labels |

## Seeding (decision 20)

`make seed only=events count=1000000` inserts fake events over the last 7 days (plus a thin tail back 90 days) across the catalogue, with bursts of failed sign-ins from a few IPs and plausible payloads, through the events factory. Insert-only and refused in production, like the other seeders. It exists to check the summary and list queries with `EXPLAIN (ANALYZE, BUFFERS)` at volume, recorded in the plan's build notes.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A read that isn't recorded | One `admin.event.listed` per request, summary included (`filters.summary`), with every filter and the resolved window. The catalogue stays the one unrecorded read, names only |
| New data exposed | None: the summary counts rows the same admin can already list with the same filters; `refs` names what the rows already name; `failed` is derived from the payload the row carries. No secrets are in payloads (`TestPayloadsHoldNothingSensitive`), and `decodeAdmin` keeps walking every admin response |
| Admin code in the app build | Page code stays in `pages/admin` and `components/app/admin`; API paths and types in the admin modules. `Combobox`, `Calendar` and `BarList` are generic primitives holding no admin path, key or string; `no-admin-modules` and `scripts/check-app-build.sh` still guard the app build |
| Query cost: many names | Tokens checked against the catalogue and expanded in Go: at most 60 names, 64 tokens, 4 KB |
| Query cost: wide or repeated charts | Charts only for 7 days or less, only on the first page, a 5 s deadline, two at a time per server, the covering index; the list answers even when the charts are skipped. `admin-read:per-user` (1,000 / 5 min) bounds a stolen session's requests |
| Query cost: the list over all time | Cursor pages count nothing; every filter has an index; the 10 s request timeout cancels the query; the per-name merge if `EXPLAIN` shows a slow plan |
| SQL through filters | Names, groups, windows, outcomes and bucket sizes all come from fixed Go tables; values through `?`; IDs, emails and IPs checked as today |
| Markup in a name or payload | React escaping only: chart labels, tooltips, chips and badges are text; `react/no-danger` stays on |
| Personal data in shared links | As today: admin pages only, `no-referrer`, our logs store no query strings |

## Tests

**Go** (`endpoints/admin/events_integration_test.go`, `events_test.go`; `store/admin_event_summary_test.go`; `events/groups_test.go`):
- `TestEventGroups`: every catalogue name in exactly one group; IDs unique; `admin-reads` is exactly the `admin.*.listed`/`.viewed` names; `admin.*` holds none of them; a sub-group has two or more names.
- Several tokens return the union; `admin.*` leaves reads out; `admin-reads` and a read named exactly show them; no `name` hides them. 400 for an unknown, empty, repeated or 65th token, an over-long value, `adminReads`, `window` with `from`, `from` ≥ `to`, an unknown window, and `summary` with `before`.
- `outcome=failed` returns exactly the failed checks of each kind, and `failed` on rows agrees with it.
- Windows resolved by a pure function from a fixed instant (`resolveWindow`), every option, at midnight and at a month's end; integration tests check the relative windows with rows well inside and outside.
- The summary: zero-filled buckets of the right size for each window length; four groups plus Other; by name inside one group; top 10; failure kinds; `limitsReached`; `too_wide` for 8 days and `all`; `too_slow` under a tiny deadline (the deadline injectable in tests); `busy` with both slots held. Its totals equal the rows the list returns for the same filters.
- One event per request with `filters.summary`; `refs` for a live, a deleted and an unknown account and workspace; the catalogue's `eventGroups`.
- Unit tests for folding (Other, ties, empty) and token compression.

**Vitest** (`pages/admin/events.test.tsx`, `components/ui/combobox.test.tsx`, `calendar.test.tsx`):
- `Combobox`: picking a group picks its names and shows it checked; one name of a group shows it mixed (and says "1 of 3 selected"); picking the group again clears it; search keeps the matching name's group; Enter toggles; Backspace removes nothing; chips compress and remove.
- The page: the URL read and written canonically (groups compressed, catalogue order), a token the API would refuse dropped; Apply writes one navigation; Cancel and Clear all apply nothing; badges show names from `refs` and remove one filter each; the range `Listbox` writes `window`; Custom range builds UTC days from the calendar's local dates (tested with `TZ` set east and west of UTC); the summary requested only without `before` and kept for Older; each `status`'s text and Retry; the charts' screen-reader tables; a row opening in place without a request; Failed badges; loading, empty (both wordings) and error with Retry.
- `lib/api/admin.test.ts`: the new paths, defaults left out.

**Playwright** (`tests/e2e/admin.spec.ts`): an admin opens Events, sees the tiles and charts, picks the `auth.*` group and one workspace name in the Filters dialog, applies, sees two badges and the URL, removes one; chooses Last hour; picks a custom range of one day; clicks a column and lands on that bucket; follows a Top events row; Back restores the previous filters. No CSP violations (BaseUI's calendar sets styles through Styletron, which `style-src` allows).

**Volume** (manual, `docs/testing.md`): with `only=events` at a million rows, the default view, each window, a group, `outcome=failed` and All time with a rare name each answer in under 2 s, plans recorded.

## Docs to update

- `docs/api/admin.md`: the Events parameters, response, summary and statuses, the concurrency bound; the catalogue's `eventGroups`; the Web app's Events paragraph; the activity links' `window=all`.
- `docs/api/events.md`: event groups (`events.Groups()`, `admin-reads`), and `admin.event.listed`'s new `filters`.
- `docs/api/endpoints.md`: the events and catalogue rows.
- `docs/api/database.md`: `events_created_idx … INCLUDE (name)`.
- `docs/api/pagination.md`: a cursor page may carry a first-page summary.
- `docs/api/seeding.md`: the `events` seeder. `docs/api/testing.md`: the tests above. `docs/testing.md`: the volume check.
- `docs/ui/design.md`: `Combobox`, `Calendar`, `Badge onRemove`, `BarChart onSelect`, `BarList` in the component table and Charts. `docs/ui/base-ui.md`: `Select` (`multi`, searchable) → `Combobox`, `Calendar` (datepicker) → `Calendar`, and the local-midnight pitfall under known issues.
- `docs/ui/forms-and-actions.md`: a row for **List filters** (a Filters button in the page header opening a dialog with Clear all · Cancel · **Apply filters**, applied filters as removable badges under the heading).
- `docs/ui/pages.md`, `docs/ui/pagination.md` (the kept summary), `docs/ui/README.md` (`event-filters.ts` in `components/app/admin`), `docs/ui/testing.md`.
- This plan, when it ships: status, how the build differs, moved to `implemented/admin/`. [monitoring](monitoring.md)'s `Drill` builds these parameters (`name` tokens, `window` or `from`/`to`, `outcome`) when A4 is built.

## Decisions (confirmed 2026-11-01, as recommended)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Picking events | **Decided per the user's notes 2026-11-01:** a searchable multi-select where a group picks all its names, partly picked groups shown mixed. Groups as in [Event names](#event-names-a-picker-with-groups-decision-1) |
| 2 | Time range | **Decided per the user's notes 2026-11-01:** one dropdown with Last hour, Today, Yesterday and Custom range (a date picker). Recommended additions: **Last 24 hours** (the default), **Last 7 days** and **All time** (the list only), which today's page and its links need |
| 3 | Where filters live | **Decided per the user's notes 2026-11-01:** a Filters button opening a dialog, Apply, then removable badges, long event lists collapsed ("auth.*, workspace.invitation.created +1"). Recommended: the button shows the word "Filters" and a count beside the icon, rather than an icon alone, for a label everyone reads the same way |
| 4 | Graphs | **Decided per the user's notes 2026-11-01:** graphs on the page. Recommended set: four tiles, events over time stacked by group, failed checks over time, top 10 events ([Graphs](#graphs)) |
| 5 | Time zone | **UTC**, labelled, like every time in the dashboard: shared links show the same rows and the list agrees with the event page. A "my time zone" switch is left for later |
| 6 | Default window | **Last 24 hours**, for the page and the API (which defaults to all time today), as Mail and monitoring do. Activity links pass `window=all`, Mail's links its own window |
| 7 | Admin reads | **Their own group, `admin-reads`**; `admin.*` leaves them out; nothing picked hides them; `adminReads` goes. Same behaviour as today's toggle, without a separate control |
| 8 | Encoding the choice | **One `name` parameter, comma-separated tokens**, written canonically by the page; at most 64 tokens and 4 KB; `auth.` prefixes become `auth.*` groups |
| 9 | Serving the graphs | **The same request, `summary=include`, first page only**, recorded in the same `admin.event.listed`; the page keeps it for Older pages. Keeps one endpoint and one read per page view |
| 10 | Bounding the graphs | **7 days or less, a 5 s deadline, two at a time per server**, and a `status` so the list always answers. No new rate limit |
| 11 | Index | **`events_created_idx` gains `INCLUDE (name)`** (edited into 007, after A3's edit), for index-only counts of the default view. Any other index only if the volume check shows a need |
| 12 | Failures | **One fixed table in the store** (`adminEventFailures`) behind the failures chart, the rows' Failed badge and a new `outcome=failed` filter ("Failed checks only") |
| 13 | Building the picker | **`Combobox` on BaseUI's multi `Select`** (searchable, kept open, toggling), a new generic primitive. Alternative: a search field over a checkbox tree inline in the dialog, simpler for screen readers but not the combobox the user asked for |
| 14 | Custom range | **BaseUI's `Calendar`** (`baseui/datepicker`, already installed with `baseui`), wrapped as `components/ui/calendar.tsx`, UTC days, the last 365 days. Fallback if its styling costs too much: two native date inputs in the same dialog, as today |
| 15 | Where the time range sits | **In the page header beside Filters**, applied when chosen, not inside the dialog: it's the filter changed most and the charts follow it |
| 16 | Clicking a column | **Zooms to that bucket** as a custom range |
| 17 | Top events | **`BarList`**, an HTML list of links with bars, in `chart.tsx`, rather than a Recharts horizontal chart |
| 18 | Names in the badges | **`refs` in the response** names the account and workspace filters, so a badge says "Account: Ada Lovelace", not an ID |
| 19 | Removable badges | **`Badge` gains `onRemove` and `removeLabel`** (an × inside it), rather than a new chip component; removing applies at once (one recorded read) |
| 20 | Checking volume | **An `events` seeder** (`make seed only=events count=…`), fake data, refused in production, to `EXPLAIN` the queries at a million rows |
| 21 | The table | Columns as today, **times to the second**, a **Failed** badge, **rows that open in place** with the payload (no request), "More like this" on the event page |
| 22 | Refresh | **None on this page** (the user's note on removing Refresh buttons); Retry stays on failures (AGENTS.md rule 8), and picking a range again reloads |

## How the build differs

- **The list of a window running to now has no upper bound.** `1h`, `24h`, `today` and `7d` filter `created_at >=` their start only, so the newest rows show whatever the database's clock says; the echoed `to` is the server's time. `yesterday` and a custom range keep their exclusive end. A custom range with only `from` runs to now; one with only `to` has no start, so its summary is `too_wide`.
- **A relative window keeps its own bucket:** `today` and `yesterday` are always 30 minutes, and `7d` 3 hours, though flooring its start to a bucket makes it a little over 7 days. Flooring also gives the last hour 61 columns and the last 7 days 57.
- **Empty values** (`name=`, `outcome=`, `summary=`) count as not given, as every other parameter's; anything else not accepted is still 400.
- **The summary's shape:** only `status` when it isn't `ok`; `series` and `topNames` are `[]` when nothing matched; `failures` is null when no picked name can record a failed check. A database error in the summary that isn't its deadline is a 500 like any other (nothing recorded), not a status.
- **Names in Go:** the summary is `store.AdminSummary` (`AdminEventSummary` already names a delivery's event on the Mail page), `SummarizeAdminEvents` and `FindAdminEventRefs`; the failure table is its own file, `store/admin_event_failures.go` (with `FailureNames`, `FailureKinds`); windows and buckets are `endpoints/admin/event_window.go` (not `event_windows.go`: Go builds a `_windows.go` file only on Windows). The bound on summaries is `httpapi.Summaries` (`summaries.go`: two slots, a 1 s wait, the 5 s deadline; `Wait` and `Deadline` exported so tests shorten them), one field on `Server`.
- **Groups:** every group has a label (sub-groups too: "Two-factor authentication", "Members", …); each group's `names` are the names directly in it, `groups` its sub-groups (`[]` for none). Groups come in catalogue order, so `rate_limit.*` comes before `email.*`. A group of one name is still a group, so picking `rate_limit.reached` reads `rate_limit.*`.
- **Volume** (`make seed only=events count=1000000`, PostgreSQL 17, recorded in [testing.md](../../../../docs/testing.md#volume-check-for-the-events-page)): the default view's list 0.6 ms, its counts 88 ms (index-only on `events_created_idx … INCLUDE (name)`, no heap fetches), its failures 0.3 s; 7 days' counts 1.2 s (an index-only scan of the name index and a sort that spills to disk at the default `work_mem`; 0.8 s at 16 MB), failures 0.3 s; `auth.mfa.*` over 7 days 29 ms; `outcome=failed` 2 ms; all time with rare names 0.1–30 ms (the planner takes the name index when few rows match). Everything is under 2 s, so **the per-name merge wasn't built** and no other index added.
- **The seeder** has its own bound (2,000,000; the others keep 100,000) and the seed command's timeout is 20 minutes; a million events take about 2½. Refusing production moved into `allowed` in `cmd/seed`, with a test.
- **`Calendar`:** BaseUI's month and year menus are plain text (the arrows change month); with nothing picked, two months end at the latest pickable one (`highlightedDate`); a range needs an array value, so nothing picked is `[]`. The custom range dialog is `size="2xl"`, not `sm`, to fit two months.
- **`Combobox`:** shares `Listbox`'s exported slots; its chips are `Badge onRemove` (BaseUI calls the remove `onActionClick`). Its chips list leftover names in tree order (sub-groups first), while the page's badges and URL use catalogue order.
- **Clicking a column** means clicking its bar (Recharts' `Bar onClick`, which carries the row): the chart-wide click reads the hover index, which lags a quick pointer and zoomed to the wrong bucket in the e2e test. An empty column has nothing to click; the custom range covers it.
- **Charts:** `BarChart` is generic over its row type (rows carry their bucket's `start`), and its ticks are compact ("1.5K") everywhere, Mail's too. The Failed checks tile reads "N% of sign-ins" (failed sign-ins over attempts), or what can fail when filtering to failed checks.
- **The page:** `useAdminResource` gained `stale` (the shown data is the last query's), so the charts and caption know when they're kept from other filters (dimmed charts also take no clicks, so a column can't zoom by a bucket about to change); the caption reads a whole-day range as days ("Oct 31 UTC"). An empty "Last 7 days" offers Show all time. The Filters dialog has a one-line description. An IP on an event's page links to its last 7 days (`IpLink` takes an `href`; `EventTable` an `ipHref`). `eventQuery` and `eventsUrl` (`lib/admin-routes.ts`) build both the page URL and the API path, commas kept readable.
- **The admin plans' table** had no A10 row; it gained one.
- **After the security review** (2026-11-01): the read records the charts' status (`summary: "ok"`, `"busy"`, `"too_slow"`, `"too_wide"`) rather than `true`, so the trail says whether they were served; `refs` holds an account's ID and name only, not its email; each admin may have one summary in flight (a second is `busy` at once); times are years 1970–9999 and a custom range at most 366 days (400 otherwise); the seeder marks every payload `"seeded": true`; the custom range takes UTC start and end times, so any bucket is reachable without a mouse; Apply waits for the catalogue; Retry on a kept summary goes to the newest page; the picker's rows carry `aria-checked`; removing a badge moves focus to the next one.

## Not included

- **Saved views** and per-admin defaults.
- **Comparing with the previous period** ("+12% on yesterday"): it doubles the window, past 7 days for a week.
- **Times in the browser's time zone** (decision 5).
- **Several accounts, workspaces, emails or IPs at once:** one each, as now.
- **Searching payload text:** no index serves it; filters by field do.
- **Counts beside each name in the picker**, distinct accounts or IPs in the tiles: more aggregates, mostly reading rows.
- **Drag to zoom** on a chart: clicking a column does it.
- **Live tail or auto-refresh:** every load is a recorded read.
- **CSV export:** [compliance](compliance.md).
- **Monitoring's signals and thresholds:** [monitoring](monitoring.md) links here instead.
