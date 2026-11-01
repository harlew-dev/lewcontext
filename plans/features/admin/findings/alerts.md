# Findings: A5, alerts (and A6's housekeeping actions)

Plan: alerts (on the events branch it has moved to `implemented/admin/alerts.md`). Built on the **events** worktree (`../lewcontext-events`, branch `admin-run/events`), with every recommendation in its Decisions table taken as accepted.

## What shipped

- **An evaluator** run by one server at a time, chosen by a PostgreSQL advisory lock held on its own connection. Alert rules are the monitoring signals marked for alerting, with `For` durations in code. Alerts open after their `For` time, resolve after 10 minutes at OK, and escalate in place.
- **Email to admins:** each admin chooses `all`, `critical` (the default) or `none` on the Admins page. Critical alerts are re-emailed every 4 hours until acknowledged. More than 5 openings in 10 minutes become a digest.
- **An optional webhook** (generic JSON or Slack's shape, signed with HMAC when a secret is set) and an optional heartbeat URL pinged each minute, so a dead man's switch can notice if the app stops evaluating.
- **The Alerts pages** (a list and one alert), with an open-count badge in the sidebar.
- **A6's housekeeping actions:** acknowledge an alert (operator, no code), silence a signal for 1, 4 or 24 hours (needs a code and a reason), and resolve an error group on the Errors pages.
- **Content:** nothing in an email or webhook holds the admin key, a link, a name, an email address or an IP. A test renders every kind and checks.

## Where the build differs from the plan

- **Extra flood bounds:** a 1-hour cooldown per signal, a silence that carries over to the signal's next alert, and reminders for critical alerts only.
- **Two rules left out** because no signal exists for them: "server silent for 2 minutes" and "oldest event over 97 days".
- **New signals:** `admin.sessions_moved` and `errors.new_frequent`.
- **The sidebar count** is as of page load.

## What you must configure

All optional: `ALERT_WEBHOOK_URL`, `ALERT_WEBHOOK_FORMAT` (`json` or `slack`), `ALERT_WEBHOOK_SECRET` (32+ characters), `ALERT_HEARTBEAT_URL`, and `ALERT_ALLOW_PRIVATE_TARGETS` (default false). Alert email needs working SMTP (with `MAIL_DRIVER=off` alerts appear only on the dashboard and in the webhook). Schedule `prune alerts` daily. Point the heartbeat at a dead man's switch service and keep an external uptime check.

**Documented risk:** with `ALERT_ALLOW_PRIVATE_TARGETS=true`, the webhook and heartbeat can reach internal addresses. Only an operator can set it.

## Security review

**No critical or high findings.** Sound: outbound HTTP (the private-address check runs at connect time on the final address, so DNS rebinding is covered; no redirects, no proxy, bounded timeouts and bodies; URLs never logged), webhook signing, message content, the leader lock, every route behind the key and session (acknowledge only through `Housekeeping`, silence through `Action`), `PATCH /api/admin/me` acting only on the caller, and the table's constraints.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **Escalation bypassed the cooldown and burst limits.** A user could drive a signal warn → critical → OK and back, emailing every admin about once per signal every 12–15 minutes with no cap | Escalation and resolved mail count in the burst window; a per-signal hourly cap covers every kind |
| 2–3 | Low | The leader's lock check had no timeout, and a failed lock attempt could leave the lock on a pooled connection | Timeout with release; the connection is closed on error |
| 4 | Low | An alert for a signal that no longer exists couldn't be emailed (the subject check rejects `_`) | A fixed fallback title |
| 5 | Low | The address check treated some unusual ranges as public (documentation ranges, `fec0::/10`, Teredo, IPv4-compatible forms) | Added to the refused list |
| 6–7 | Info | Webhooks have no delivery ID for replay dedupe; acknowledging needs no code (as planned) | A delivery header added; documented |

**Fix results:** all made (`d41bd87`..`b834ba5`, plus a factory fix). Each mail kind now counts against the burst window and a cap of 3 emails an hour per signal; a test runs warn → critical → OK four times and gets exactly 3 emails. The leader's lock check times out after 5 s and lets go; an undefined signal gets the title "An alert"; the outbound guard refuses more ranges; webhooks carry an `X-Lewcontext-Delivery` header.

**Verified on the merged tree (now in main, `ea93e33`):** `make check`; `make test` (all Go packages with the race detector; Vitest 65 files, 456 tests); `make build`; e2e 32 of 32, including the new Alerts spec.

**Screenshots:** [the Alerts list](alerts/alerts-list.png), [one alert](alerts/alert-page.png), [the silence dialog](alerts/silence-dialog.png), [Resolve on an error group](alerts/error-resolve.png).
