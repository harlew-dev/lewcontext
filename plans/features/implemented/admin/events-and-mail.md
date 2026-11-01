# Admin: Events and Mail pages

Status: **implemented 2026-10-31**, with the confirmed decisions below; see [How the build differs](#how-the-build-differs). Plan A1 of the [admin plans](../../admin/README.md). Builds on [admin access](../admin-access.md), [events](../events.md), [email delivery](../email-delivery.md), [email verification](../email-verification.md). Docs updated: [admin.md](../../../../docs/api/admin.md), [events.md](../../../../docs/api/events.md), [endpoints.md](../../../../docs/api/endpoints.md), [database.md](../../../../docs/api/database.md), [pagination.md](../../../../docs/api/pagination.md), [ui/pages.md](../../../../docs/ui/pages.md), [ui/pagination.md](../../../../docs/ui/pagination.md), [ui/design.md](../../../../docs/ui/design.md) (the chart component), [ui/README.md](../../../../docs/ui/README.md) (the dependency), [ui/api.md](../../../../docs/ui/api.md), [rate-limits.md](../../../../docs/api/rate-limits.md) (`rate_limit.reached`'s IP), [api/testing.md](../../../../docs/api/testing.md), [ui/testing.md](../../../../docs/ui/testing.md); AGENTS.md and [docs/README.md](../../../../docs/README.md) (admin.md's summary).

## Why

The `events` table already records every sign-in attempt, password reset, limit reached, workspace change and admin read, and `event_deliveries` already holds every email's status. Today an operator reads them with SQL against production. These two pages make them readable, filterable and linked to Users and Workspaces, without SQL and with every look recorded.

They are also the base the [monitoring](../../admin/monitoring.md) pages build on: each monitoring chart links to the Events page, filtered to the rows behind it.

## Data changes

### `events.ip` (decision 1)

Only some events carry the client's IP today, inside their payload (`auth.login_attempted`, `auth.password_reset.requested`, `auth.registration.requested`, `auth.registration.checked`, `auth.mfa.checked`, `admin.login_attempted`, `admin.logged_in`, `rate_limit.reached`). `auth.registered`, `workspace.created`, `workspace.invitation.created` and the rest don't, so "how many accounts did this IP sign up?" can't be answered.

```sql
-- 007_create_events (edited: migrations are squashed in development)
ip inet,   -- the client's address for events recorded during a request; null for the CLI and listeners
CREATE INDEX events_ip_created_idx ON events(ip, created_at DESC) WHERE ip IS NOT NULL;
CREATE INDEX events_email_created_idx ON events((lower(payload->>'email')), created_at DESC) WHERE payload ? 'email';
CREATE INDEX events_payload_workspace_idx ON events((payload->>'workspace'), created_at DESC) WHERE payload ? 'workspace';
CREATE INDEX events_actor_created_idx ON events(actor_id, created_at DESC) WHERE actor_id IS NOT NULL;  -- the account filter's actor half
```

- **Set like `user_agent`:** `clientAddress` puts `clientIP(r)` in the context (`events.WithIP`), and `record` copies it into the column. It's the full address (an IPv6 /64 is only how limits count).
- **The payloads' `ip` fields go** (decision 2): the column is the one place it lives. `TestPayloadsHoldNothingSensitive` gains a check that no payload has an `ip` field, so it can't come back.
- **Personal data:** the same data already stored for half the catalogue, now for all of it. Retention is the events' 90 days; [compliance](compliance.md) proposes shortening how long the IP itself is kept.

## Events page

| URL | File | Export |
| --- | --- | --- |
| `/admin/:key/events` | `pages/admin/[key]/events.tsx` | `AdminEventIndex` |
| `/admin/:key/events/:event` | `pages/admin/[key]/events/[event].tsx` | `AdminEventShow` |

### `GET /api/admin/events`

| Param | Rule | Default |
| --- | --- | --- |
| `name` | A catalogue name (`auth.login_attempted`) or a prefix ending in `.` (`auth.`, `workspace.member.`). The prefix is expanded in Go to the catalogue names under it, so the query is `name IN (…)` on the existing `(name, created_at)` index, never `LIKE`. Anything not in the catalogue is 400 | all |
| `account` | `usr_…`: events the account did or is about (`actor_id` or `user_id`) | all |
| `workspace` | `wsp_…`: matched on `payload->>'workspace'`, so a deleted workspace's events are still found (decision 7) | all |
| `email` | A full address, compared lower-case with the payload's `email` | all |
| `ip` | An address, or a CIDR no wider than /16 (IPv4) or /32 (IPv6) (`ip <<= ?`) | all |
| `from`, `to` | RFC 3339 times | none |
| `adminReads` | `include` to show `admin.*.listed` and `admin.*.viewed` | hidden (decision 5) |
| `before` | An `evt_…` cursor: rows older than it | newest first |
| `perPage` | 1–100 | 50 |

Responds `{data: [...], next: "evt_…" | null}`: **cursor pagination, no total** (decision 3). Counting millions of rows for "page 3 of 4,812" is the slow part of offset pagination, and nobody pages to the middle of an audit log; they filter. Unknown or repeated parameters are 400, as everywhere.

Each row: `id`, `name`, `createdAt`, `actor` and `user` (`{id, name, email}` or null; an account since deleted shows its `usr_` from the payload, if any), `workspace` (`{id, name}`, or `{id}` once deleted), `ip`, `payload` (as stored, below). `userAgent` only in the detail.

### `GET /api/admin/events/{event}`

The row as above with `userAgent`, plus `deliveries` (`listener`, `status`, `attempts`, `nextAttemptAt`, `deliveredAt`, `failedAt`, `lastError`) and `related`: the `evt_` named in a `request` field (`workspace.invitation.sent` → its `created` event), and events naming this one.

### Project content is shown (decision 4)

Admin access showed projects as counts only (its decision 9). That was for the dashboard's first phase, and **this plan supersedes it**: admins see project names and descriptions, so payloads are shown as stored, and `project.created`, `project.updated` and `project.deleted` read in full.

- **Secrets still never appear:** they were never in payloads to begin with. `TestPayloadsHoldNothingSensitive` already keeps passwords, hashes, tokens, codes and internal IDs out of every event struct, and `decodeAdmin` keeps walking every admin response.
- **The workspace page lists its projects**, so the log and the workspace page agree (decision 10). `GET /api/admin/workspaces/{workspace}/projects` (`adminWorkspaceProjectHandlers.index`) returns a page of `id`, `name`, `description`, `status`, `color`, `createdAt`, `updatedAt`, with the app's `q`, `status` and `sort` parameters ([pagination.md](../../../../docs/api/pagination.md)), and records `admin.project.listed` (`admin`, `workspace`, the filters). The workspace page shows it as a `PaginatedTable` under its members, rows without links (there's no project page in the dashboard).
- `docs/api/admin.md` drops "project names or descriptions (counts only)" from its "Never in a response" list.

### The page

- **Filter bar** above a `Table`: name (a `Listbox` from the catalogue, grouped by area: each area's prefix is itself an option, "All auth events" (`auth.`), followed by its names, so no section headings need adding to `Listbox`), account, workspace, email, IP, from and to. Filters live in the URL, so a link from elsewhere in the dashboard opens the right view.
- **Rows:** time, name, a one-line summary built from the payload (`wrong_password · ada@example.com`, `auth:per-ip:block`), actor, subject, IP. Every ID links: accounts to Users, workspaces to Workspaces, IPs to `?ip=`, and the row to its detail.
- **Older / Newer** buttons instead of numbered pages. A Refresh button; no auto-refresh, since every load is a recorded read.
- **Links in:** the user page gets "Activity" (`?account=usr_…`), the workspace page "Activity" (`?workspace=wsp_…`). Monitoring charts link here filtered to their rows.
- **Detail:** the fields in a `DescriptionList`, the payload as indented JSON in a `<pre>` through React's escaping, the user agent, the deliveries, and related events.

### `GET /api/admin/catalogue`

`{events: [names], limits: [names], listeners: [names]}`, for the filters. It holds no user data, so it's the one admin read **not** recorded (decision 6).

## Mail page

| URL | File | Export |
| --- | --- | --- |
| `/admin/:key/mail` | `pages/admin/[key]/mail.tsx` | `AdminDeliveryIndex` |

The API resource is `deliveries` (the table, and the listeners aren't all mail forever); the page reads naturally as Mail (decision 8, as [naming.md](../../../../docs/naming.md) allows: URLs don't follow the action names).

### `GET /api/admin/mail`: the summary

For a `window` of `24h` (default) or `7d`:

| Section | Shows | From |
| --- | --- | --- |
| Queue | due now, retrying (`attempts > 0`), **oldest pending age** (the worker's lag), failed in the window, delivered in the window, per listener | `event_deliveries` (the pending index) |
| Volume | emails sent per hour, by template: reset link, invitation, sign-up code, "you already have an account" (`existingAccount`), email verification code (decision 11) | `auth.password_reset.sent`, `workspace.invitation.sent`, `auth.registration.sent`, `auth.email_verification.sent` |
| Asked for vs sent | Resets: requests for an address with no account send nothing, so a wide gap is someone probing addresses. Sign-ups: every request sends a code or "you already have an account", so a gap is mail trouble, requests left over 15 minutes, or `account-exists:per-account` holding mail back. Email verification: every request sends | `auth.password_reset.requested` vs `.sent`; `auth.registration.requested` vs `.sent`; `auth.email_verification.requested` vs `.sent` |
| Top recipients | addresses asked for 3 or more times in the window, with counts per kind (decision 9). Shows an attempt to flood one inbox, which the per-email limits cap | `email` on `auth.password_reset.requested`, `workspace.invitation.created` and `auth.registration.requested` (`auth.email_verification.requested` names only the account, already capped per account) |
| Driver | `MAIL_DRIVER`, host, port, TLS mode, sender; the last success and the last failure's error | config, `event_deliveries` |

### `GET /api/admin/deliveries`

| Param | Rule |
| --- | --- |
| `status` | `pending`, `retrying`, `failed`, `delivered` |
| `listener` | a registered listener name |
| `from`, `to` | the event's time |
| `before`, `perPage` | a cursor (`evt_…:listener`) and page size, as Events |

Each row: `event` (`{id, name, createdAt}`), `listener`, `to` (the payload's `email`, where the event has one), `status`, `attempts`, `nextAttemptAt`, `deliveredAt`, `failedAt`, `lastError`. A row links to its event's detail.

### The page

Stat tiles for the queue, a bar chart of volume per hour, the asked-for-vs-sent pair, the top recipients table (each address links to Events `?email=`), the driver box, then the deliveries table with status tabs (Failed first when there are any).

Retrying a failed delivery is an action ([actions](actions.md)). When [email blocks](../email-blocks.md) ships, Mail gets a Blocked addresses tab listing `email_blocks`.

## Charts (decision 12)

The volume chart is the dashboard's first; [monitoring](../../admin/monitoring.md) adds many more (stacked time buckets, sparklines, meters). Rather than hand-drawing each, they use one library behind one wrapper.

- **Recharts 3.10.1** (MIT; released 2026-07-25), pinned exactly, with its peer `react-is` 18.3.1 to match React. SVG, declarative React components, React 18 supported.
- **Why Recharts:** SVG rather than canvas, so marks take Tailwind classes (`fill-zinc-900 dark:fill-zinc-100`) and follow dark mode without a second set of colours; keyboard navigation of the tooltip (`accessibilityLayer`, on by default in v3); bar, stacked bar, line, area and pie cover every chart monitoring plans. Alternatives considered: **visx** (smaller pieces, but far more code per chart and a `@react-spring/web` peer), **Chart.js** (canvas: no Tailwind classes or `dark:`, weaker screen-reader support), **Nivo** (heavier, its own theming system).
- **Its cost:** an estimated 100 KB or so gzipped, measured at the build (it brings Redux Toolkit, Immer and d3 pieces through `victory-vendor`). React Router splits per route, so only admin pages that draw a chart load it; `make build` is checked to keep it out of the home page's and the app's chunks.
- **One wrapper, `components/ui/chart.tsx`:** `BarChart` (time buckets, stacked by series, a legend) is the only importer of `recharts`; pages pass rows and series and never style Recharts themselves. Monitoring adds `Sparkline` and the rest beside it. Series colours are complete class strings from Tailwind's default palette (zinc first, then a fixed order), matching AGENTS.md rule 6; red, amber and green stay reserved for levels.
- **Accessibility:** every chart renders its numbers as a visually hidden table (`sr-only`) beside the SVG (the SVG itself is `aria-hidden`), with a one-line text summary ("142 emails in the last 24 hours, most at 14:00"). Animation is off under reduced motion (`isAnimationActive`).
- **CSP:** no change. Recharts draws SVG elements and sets styles through React (`element.style`), which `style-src 'self' 'unsafe-inline'` allows; it uses no `eval` or inline scripts.
- **Tests:** Vitest checks the hidden table's numbers, not the SVG (jsdom has no layout); Playwright sees the chart on the Mail page.
- `docs/ui/README.md` (Dependencies) gets an entry, `npm audit` is run, and the lockfile is committed. Monitoring's decision 4 (hand-built SVG) is revised to use this wrapper.

## Events recorded

| Event | Payload |
| --- | --- |
| `admin.event.listed` | `admin`, the filters used |
| `admin.event.viewed` | `admin`, `event` |
| `admin.mail.viewed` | `admin`, `window` |
| `admin.delivery.listed` | `admin`, the filters used |
| `admin.project.listed` | `admin`, `workspace`, the filters used |

Actor only, as the other admin reads.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Project content now visible to every admin | Admins already see every account and workspace; every list and detail read that shows it is recorded, so who looked at which workspace's projects is answerable. [Access hardening](access-hardening.md)'s roles can narrow it later if needed |
| A name, email or user agent with markup (`<script>`) in a payload | React escaping only; JSON shown as text in `<pre>`; `react/no-danger` is already on |
| SQL through filters | Every value through `?` placeholders; names checked against the catalogue and expanded in Go; IDs checked with `validPublicID`; IP and CIDR parsed with `netip` before the query |
| A filter that scans the whole table | Every filter has an index; CIDRs are bounded; `perPage` ≤ 100; the 10 s request timeout; cursor pagination counts nothing |
| `last_error` holds an SMTP reply naming the recipient | Admin-only, already cut to 500 characters, and never the SMTP password ([mail.md](../../../../docs/api/mail.md#smtp)) |
| The audit trail drowns in its own reads | Admin reads hidden unless asked for, still recorded |
| Looking someone up leaves no trace | Every list and detail read is recorded with its filters, so "who searched for this email" is answerable |
| IPs are personal data | One column, the events' retention, and [compliance](compliance.md)'s proposal to shorten it |

## Tests

**Go integration** (`admin_events_integration_test.go`, `admin_deliveries_integration_test.go`):
- Every filter, alone and combined; a name prefix returns only names under it; an unknown name, a malformed ID, a too-wide CIDR and a repeated parameter are 400.
- Cursor pagination: no row twice or missing across pages, with rows sharing a `created_at`.
- A deleted workspace's events are found by `workspace=`; a deleted account's events show its `usr_`.
- Project events show their names and descriptions; the workspace's projects list pages, searches, filters and sorts as the app's does, and refuses a missing or malformed workspace with 404.
- Admin reads hidden by default, shown with `adminReads=include`.
- `events.ip` is set for request events and null for CLI and listener events; no payload has an `ip` field.
- Mail summary: counts per state, oldest pending age, top recipients only at 3 or more.
- Each read records one event; both routes refuse without the key (unknown-route 404) and without an admin session (401).

**Vitest:** the filter bar writes and reads the URL; Older/Newer; loading, empty, error with Retry; the payload `<pre>` escapes markup.

**Playwright** (in `e2e/admin.spec.ts`): an admin filters events by a user's email, opens one, follows the account link; opens Mail and sees a failed delivery from a broken template seeded in the test database.

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Where the client IP lives | An `events.ip` column (`inet`) on every event recorded during a request, indexed. Without it, IP views would only cover the six events that carry one. **Confirmed 2026-10-31.** |
| 2 | The payloads' `ip` fields | Removed: one source of truth. Free while migrations are squashed **Confirmed 2026-10-31.** |
| 3 | Paging events and deliveries | Cursor pagination (`before`, `next`), no totals. A documented exception in [pagination.md](../../../../docs/api/pagination.md) for log tables **Confirmed 2026-10-31.** |
| 4 | Project names and descriptions | Shown to admins, in payloads and on the workspace page; admin access decision 9 was for its first phase only. **Confirmed 2026-10-31.** |
| 5 | Admin reads in the Events list | Hidden by default, with a toggle **Confirmed 2026-10-31.** |
| 6 | The catalogue endpoint | Not recorded: it holds no user data. The one exception to "every read is recorded" **Confirmed 2026-10-31.** |
| 7 | The workspace filter | On `payload->>'workspace'` (indexed), so it finds a deleted workspace's history and admins' reads of it **Confirmed 2026-10-31.** |
| 8 | Naming | Page `/mail` (`AdminDeliveryIndex`), API `deliveries`; events `admin.event.*`, `admin.mail.viewed`, `admin.delivery.listed` **Confirmed 2026-10-31.** |
| 9 | Showing recipients' addresses | Yes, only addresses asked for 3 or more times in the window: enough to spot flooding, not a mail log **Confirmed 2026-10-31.** |
| 10 | The workspace page's projects | A paginated, searchable list (`GET /api/admin/workspaces/{workspace}/projects`), recorded as `admin.project.listed`; no project detail page **Confirmed 2026-10-31.** |
| 11 | The emails email verification added (sign-up codes, "you already have an account", verification codes) | Counted in Mail: volume per template, a sign-up and a verification pair in asked-for vs sent, and sign-up requests in top recipients. **Confirmed 2026-10-31.** |
| 12 | Drawing charts | A chart library rather than hand-built SVG: **Recharts 3.10.1** behind `components/ui/chart.tsx`, the only file importing it, loaded only by admin pages ([Charts](#charts-decision-12)). Monitoring uses the same wrapper. **Confirmed 2026-10-31.** |

## Not included

- **Email bodies.** They aren't stored and won't be: they hold working reset and invitation links.
- **Bounces and complaints** from the SMTP provider. They need a public webhook endpoint per provider, with its signature check. Worth a plan before volume grows, since sender reputation depends on acting on them.
- **CSV export** of events: [compliance](compliance.md) covers exports.
- **Live tail / auto-refresh**: every refresh is a recorded read.
- **Retrying a delivery**, and any other write: [actions](actions.md).

## How the build differs

- **More indexes:** `events_actor_created_idx` (the account filter's actor half), `events_request_idx` (an event's related events: those naming it in `request`) and `event_deliveries_failed_idx` (the Failed tab, few among many delivered). Each Events filter was checked with `EXPLAIN` on 60,000 events: the IP range becomes an index range scan, email, workspace and request use their partial indexes.
- **The account filter** resolves the `usr_` first; an unknown or deleted account matches nothing (200, empty), rather than a 400.
- **Filters are recorded nested**, as `filters` in `admin.event.listed` and `admin.delivery.listed`, so a searched-for IP is `filters.ip` and no payload has a top-level `ip` field.
- **"Retrying"** is a waiting delivery that has failed at least once (`last_error <> ''`), not `attempts > 0`, which also counts a first try in flight.
- **Mail's finished counts** (failed, delivered, the last delivery and failure) are of the window's *events* (`events.created_at`, indexed), not of `failed_at`/`delivered_at`, which have no index. Waiting deliveries count at any age.
- **Volume and requests** cover email verification's mail too (decision 11): kinds `password_reset`, `invitation`, `registration_code`, `account_exists`, `email_verification`; requests `password_reset`, `registration`, `email_verification`. Top recipients' counts are `resets`, `invitations`, `registrations`.
- **The registration store methods** lost their `ip` argument: the IP comes from the context, like every event's.
- **Nothing reloads on its own** on any admin page: `useAdminResource` no longer revalidates on focus or reconnect, since each load is a recorded read. Refresh buttons on Events and Mail.
- **The filter bar is a form** applied with Apply filters (and Clear filters), not as typed, so typing doesn't record a read per keystroke. From and To are UTC days. Admin reads is a `Listbox` (Hidden / Shown).
- **Newer** pops the cursors kept in navigation state; arriving from a link, it opens the newest page.
- **Opening Mail records two reads**, the summary and the deliveries (two endpoints, as planned).
- **Moved to shared components:** the dashboard's `Stat` (now `components/ui/stat.tsx`, used by Mail) and the projects page's status filter (`components/app/projects/status-field.tsx`, used by the workspace page).
- **Chart colours** are hues in a fixed order (sky, orange, emerald, violet, pink), not zinc first: zinc can't be told apart from the greys around it. Every pair passes the dataviz validator's full-colour and contrast checks in light and dark (colour-blind separation in its 6–8 band, carried by the gaps, legend, tooltip and table); a first order with amber beside orange looked alike on screen.
