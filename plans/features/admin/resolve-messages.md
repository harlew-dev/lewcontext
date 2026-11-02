# Admin: resolve messages for error groups (and alerts)

Status: **draft 2026-11-01** — decisions to confirm before implementation.

Builds on [admin/actions](../implemented/admin/actions.md) (A6) and [admin/alerts](../implemented/admin/alerts.md) (A5).

## Why

Resolving an error group today is one click with no explanation, so the audit trail records that it was resolved but not why. The same gap exists for alerts: an alert resolves automatically when its signal returns to normal, with no note from whoever watched it, and acknowledging one only stops reminders.

## What it adds

1. **Error groups:** `PATCH /api/admin/errors/{error}` accepts an optional `message` alongside `resolved: true`, shown on the group's page and recorded in `admin.error.resolved`.
2. **Alerts:** an optional note can be attached when an alert is acknowledged (and, optionally, when it resolves), shown on the alert's page and recorded in the event.

## Findings from the current code

- **Error groups can be resolved by hand**, but `PATCH /api/admin/errors/{error}` accepts only `{"resolved": true}`; `app_errors` has `resolved_at` and no message column. A new occurrence clears the flag.
- **Alerts cannot be resolved by hand.** `alerts.resolved_at` is set by the evaluator after 10 minutes back at ok. The only admin actions are Acknowledge (stops reminders) and Silence (an `Action` with a code and reason). A "resolved message" on an alert therefore has no admin resolve step to attach to; this plan attaches the note to Acknowledge unless decision 3 changes that.
- Empty resolves are already one click on both pages: the Error group page has a direct `Resolve` button, and the alert page has `Acknowledge`; changing them to dialogs must keep the no-note path quick (Enter/Confirm with an empty field).

## Proposed design

- **Errors:** extend `resolveInput` to `{"resolved": true, "message": "..."}`. `message` is optional, trimmed, 0–500 characters, scrubbed as user text. Add `app_errors.resolved_message text` (nullable) in the create migration (migrations are squashed), update the factory, and add it to the event payload. Clear it when a new occurrence reopens the group.
- **Alerts:** extend acknowledge to accept an optional `message`, stored in a nullable `alerts.acknowledged_message`, cleared with the acknowledgement if that ever changes, and recorded in `admin.alert.acknowledged`. Auto-resolution keeps no message.
- **Events:** update `events.AdminErrorResolved` and `events.AdminAlertAcknowledged` with `Message string json:"message,omitempty"`; list the new field in `docs/api/events.md` and keep the existing event names (no rename).
- **UI:** replace the direct buttons with one shared `NoteDialog` in `components/app/admin` (or extend `ConfirmationDialog` only if it gains a textarea cleanly), used by the error group and alert pages with different titles/labels. It has an optional `Textarea`, a character count, Confirm/Cancel and the existing pending/error handling. Render the saved note as plain text on each page (`Whitespace-pre-line`), never HTML.
- **Tier:** these stay housekeeping actions: `operator`, `admin-action:per-user`, no step-up code. The note is a note, not the step-up reason required by the `Action` tier.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Free text becoming a stored XSS or secret leak | Scrub and render as text only; never HTML; length-capped |
| A message implying an action that didn't happen | The note is clearly labelled "note" and never replaces the audit event |

## Tests

- Resolving an error group with and without a message both succeed; the message appears in the event and on the page; a new occurrence clears it.
- Acknowledging an alert with a note records it; auto-resolved alerts have none.
- Over-long messages, and a message sent with `resolved: false`, are rejected with 422.
- Viewers are refused; operators succeed without a step-up code; the existing `admin-action:per-user` budget applies.
- The note is stored scrubbed, rendered as text, and survives a reload.

## Decisions (to confirm)

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Is the message required? | Optional, to keep the one-click path |
| 2 | Max length | 500 characters, matching reasons |
| 3 | Should alerts get a manual resolve action? | No — alerts resolve on their signal; only acknowledge gets a note. If a manual resolve is wanted, that is a separate plan and must not fight the evaluator's auto-resolve |
| 4 | Storage | Add nullable columns (`app_errors.resolved_message`, `alerts.acknowledged_message`) rather than reusing the reason field, since these are notes, not step-up reasons |
| 5 | What happens to an error group's note when it reopens | Clear it with `resolved_at`; the event keeps the history |
| 6 | Does the note need a code or step-up? | No — same housekeeping tier as today; the note is optional and does not replace a reason |

## Docs and checks to update

- `docs/api/admin.md` and `docs/api/monitoring.md` (the endpoints, bodies and notes),
- `docs/api/events.md` (the payload fields),
- `docs/api/database.md` and `docs/compliance.md` (the new columns and what the note may contain),
- `docs/ui/forms-and-actions.md` (dialog with an optional field),
- factories and integration tests; no new table, so no new data-map row is needed.

## Not included

- Editing a note after the fact.
- Notes on silencing (silence already requires a reason).
