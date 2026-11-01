# Findings: A10, the Events page redesign

Plan: [events-page](../events-page.md) (on the events branch it has moved to `implemented/admin/events-page.md`). Built on the **events** worktree (`../lewcontext-events`, branch `admin-run/events`), from your notes.md, with every recommendation in its Decisions table taken as accepted. Screenshots are in [events-page/](events-page/).

## What shipped

Commits on `admin-run/events`:

| Commit | What |
| --- | --- |
| `6417d21` | The Events API: event groups defined in Go next to the event catalogue, time windows, failed checks, the bounded chart summary, `refs`, `events_created_idx INCLUDE (name)`, a fake-events seeder |
| `7517291` | New generic components: `Combobox` (searchable multi-select with groups), `Calendar` (BaseUI's), `BarList`, `BarChart` with click-to-zoom, `Badge` with a remove button |
| `89f455f` | The page: the time range, the Filters dialog, removable badges, charts, rows that open in place, "More like this" |
| `3ae4795` | The Playwright flow, plus two fixes it found |
| `f584610` | Docs; the plan marked implemented and moved |

**How it looks** (the screenshots):
- [charts and badges](events-page/events-charts-badges.png), and the [full page](events-page/events-charts-badges-full.png);
- [dark mode](events-page/events-charts-badges-dark.png);
- [the Filters dialog with a group partly picked](events-page/events-filters-picker.png);
- [the custom range](events-page/events-custom-range.png), and [after applying it](events-page/events-after-range.png);
- [phone width](events-page/events-phone.png).

**Your notes, as built:**
- **Picking events:** a searchable multi-select where picking a group (`auth.*`, `auth.mfa.*`, …) picks all its events; a partly picked group shows a dash.
- **Time:** one dropdown with Last hour, Today, Yesterday, Last 24 hours (the default), Last 7 days, All time, and Custom range with a two-month calendar.
- **Filters:** a **Filters** button with a count opens a dialog. Applied filters show as removable badges, with long lists collapsed ("Events: auth.*, rate_limit.* +3").
- **Graphs:** four stat tiles, events over time stacked by group, failed checks over time, and the top 10 events. Clicking a bar zooms to that time slot.

## Speed, measured on a million seeded events

| View | Time |
| --- | --- |
| Default list | 0.6 ms |
| Default counts (from the index alone) | 88 ms |
| 7-day counts | 1.2 s (0.8 s with `work_mem` 16 MB) |
| 7-day failures | 0.3 s |
| `outcome=failed` | 2 ms |
| All time, rare names | 0.1–30 ms |

## Where the build differs from the plan (recorded in the plan)

- **Windows that run to now** (Last hour, Today, Last 24 hours, Last 7 days) have no end bound, so a database clock running ahead can't hide the newest rows.
- **Clicking a column means clicking its bar.** Recharts' hover position lagged a fast pointer and zoomed to the wrong slot.
- **The calendar dialog is wider** (`2xl`), so two months fit.
- **The seeder can write up to 2,000,000 rows** (others: 100,000).

## Things to know

- **"Today" and "Yesterday" are UTC days,** like every time in the dashboard. A "my time zone" switch is left for later.
- **The API's default window changed from all time to the last 24 hours.** The dashboard's own Activity links pass `window=all`, so nothing outside loses history.
- **Charts appear only for 7 days or less.** Longer ranges show the list with a note.
- **Your example groups `admin.read` and `user.read` don't exist.** Groups follow the real event names, plus `admin-reads` for the admin read events, which stay hidden unless picked.
- **Fake events in dev:** `make seed only=events` writes fake audit history into a dev database. It's refused in production.

## Security review

The independent reviewer found **nothing high or medium**. It checked these and found them sound:
- **SQL:** every value goes through placeholders; the fixed failure conditions are constant Go strings; nothing from input becomes SQL text.
- **Validation:** `name` is at most 64 entries and 4 KB, checked against the catalogue; unknown or repeated parameters get 400.
- **Recording:** exactly one recorded read per load, charts included.
- **Bounds:** chart queries are bounded (7 days, 5 s, two at a time per server).
- **Admin code boundary:** the new generic components import nothing admin.
- **XSS:** everything is rendered as text, with no raw HTML.
- **The seeder** is refused in production before it connects.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Low | Applying Filters before the event catalogue loaded silently dropped the event filter | Apply waits for the catalogue |
| 2 | Low | Retry on a slow or busy chart on an Older page re-fetched only the old rows, and recorded another read | Retry goes to the newest page |
| 3 | Low | The audit row said charts were shown when they were only asked for (too wide, busy or too slow returned none) | The row records what the charts returned |
| 4 | Low | Filtering on an account returned its name **and email** even with no matching rows, which amounts to an account lookup | The email is dropped (the badge needs only the name); the docs say what's returned |
| 5 | Low | Zooming to a short time slot was mouse-only | The custom range takes UTC times too |
| 6 | Low | Screen readers could hear the picker's highlight as "selected" | `aria-checked` (including "mixed") on each row, or documented as a BaseUI known issue |
| 7 | Low | One admin could keep everyone's charts "busy" | One chart query in flight per admin; the list's worst case documented |
| — | Nits | A year-0000 date answered 500; custom ranges weren't capped server-side; seeded events looked real; the Makefile comment was stale; removing a badge lost focus | Years 1970–9999, ranges at most 365 days, seeded events marked `"seeded": true`, comment fixed, focus moves to the next badge |

_Fix results: filled in once the fixes are committed._
