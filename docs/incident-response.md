# Incident response

For the person on call when something has gone wrong: an attack, a leak, an outage, a mistake. It uses what the admin dashboard and the command line already have; where a step needs something that isn't built yet, it says so. Pair it with [compliance.md](compliance.md) (what data exists and where) and [deployment.md](deployment.md) (how the servers run). Plan: [compliance](../plans/features/implemented/admin/compliance.md) (A7), section 6.

Commands below run the production image with its environment, `docker run --rm --env-file prod.env lewcontext admin …`, written `admin …` here. Commands that change something preview without `-confirm`.

## 1. Detect

Something tells you:

- **The external uptime check** on `GET /api/health` and `/` ([deployment.md](deployment.md#logs-and-health-checks)): the app is down, or the database is.
- **Monitoring** (`/admin/<key>/monitoring`): a tile at warning or critical on Overview, Security or Rate limits ([api/monitoring.md](api/monitoring.md)). Alerts that page someone come with plan A5; until then, look.
- **The server's log:** a warning that the admin key came from outside `ADMIN_ALLOWED_IPS` (`admin.access_refused`), a burst of 5xx, `LOG_LEVEL is above info` (no audit lines), slow queries.
- **A report:** a user, a security researcher, your hosting or SMTP provider.

Write down the time you became aware: GDPR's 72 hours count from it.

## 2. Triage

Work out what is happening, and to whom, before acting on more than the obvious.

- **Monitoring's Overview and Security:** failed sign-ins and from where, credential stuffing (many addresses from one network), resets and sign-ups flooding, the admin door, possible takeovers (a sign-in from a new network after failures), recovery codes running low.
- **Events** (`/admin/<key>/events`): filter by IP or range, account, workspace, email and time; **Failed checks** for wrong passwords and codes. A row's request ID (`req_…`) finds its log lines, and the events of the same request.
- **The log:** search the request ID from a 500 or an event; access lines name the route, status, account and IP, never a path or query. **Audit lines** (`msg: "audit"`) shipped off the host are the record to trust if the database itself may have been changed: an event in them that isn't in the table was deleted (or its request failed, which its access line shows).
- **Error groups:** `SELECT source, route, message, count, last_seen_at, last_request_id FROM app_errors WHERE resolved_at IS NULL ORDER BY last_seen_at DESC` (an Errors page comes with A4).
- **Who is affected:** the Users and Workspaces pages; an account's **Activity** (its events).

## 3. Contain

Stop it getting worse. Each dashboard action needs the `operator` role, a fresh code and a reason, and is recorded ([api/admin.md](api/admin.md#actions)).

| Situation | Do |
| --- | --- |
| An account taken over, or abusing others | **Suspend** it (signs every session out, revokes its invitations, refuses sign-in): the user's page, or `admin suspend -email … -reason … -confirm`. Then **Reset MFA** if the attacker set their own authenticator, once you've confirmed who the owner is |
| An account's sessions may be stolen, the owner is fine | **Sign out everywhere** on the user's page |
| One network attacking | **Block** the address or range for up to 30 days (IP blocks page); a flood is better stopped at the edge (Cloudflare, the load balancer) |
| A workspace sending abusive invitations | **Revoke pending invitations** on the workspace's page, and suspend the account sending them |
| Sessions may have leaked in general (a log or backup exposed, a bug) | `admin sessions revoke-all -confirm`: every app and admin session ends, and every sign-in waiting for a code |
| An admin account compromised | `admin revoke -email … -confirm` (ends their admin sessions); **Sign out** their other sessions from the Admins page; then treat it as a taken-over account |
| The admin key exposed | Rotate `ADMIN_ROUTE_KEY` ([deployment.md](deployment.md#rotating-the-admin-key)); set `ADMIN_ALLOWED_IPS` if it isn't |
| The SMTP password, `METRICS_TOKEN` or a database password exposed | Rotate it at the provider and in the secret store; restart the servers |
| `APP_KEY` exposed | It **can't be rotated yet** (it seals MFA secrets and keys the email pseudonyms; [compliance.md](compliance.md#known-gaps)). Treat every MFA secret as exposed: tell users with MFA on to set it up again, and plan the rotation. Treat the email pseudonyms as reversible too: with the key, anyone holding a database copy can check a guessed address against them (`hmac:…`), so minimised events no longer protect the addresses in them |
| A bad release | Roll the servers back to the previous image ([deployment.md](deployment.md#releasing-a-new-version)); don't run down migrations in production, fix forward |

Keep what you need for later: don't prune, delete or restore over the evidence while you're still scoping. **Pause the `pseudonymise` and `prune` cron jobs** ([deployment.md](deployment.md#retention-and-erasure)) until scoping is done: `pseudonymise` cuts IPs to their network and replaces addresses with pseudonyms, and `prune` deletes old events, both for good, so a run mid-incident destroys the detail you're scoping with. Write down when you paused them and why (the breach register), and turn them back on once you've exported what you need. Export what you'll need now (next step).

## 4. Scope

Answer: what data, whose, over what time, by whom, and is it still happening?

- **Events CSV:** on the Events page, set the window and the filters (the IPs, accounts, names involved) and **Download CSV** (at most 10,000 rows, the newest; narrow the window for more). Each export is recorded with its filters. Keep the files with the incident's record, not in a shared folder.
- **Who was affected:** from the export, the accounts (`user`) and workspaces touched; for an address-only event (an attempt, an invitation), the address (pseudonymised after 30 days: search the Events page by the address and it still matches).
- **What was read:** an admin's reads are all recorded (`admin.*.listed` and `.viewed`, with filters and searches); an app account's reads aren't (only its changes), so assume whatever its role could see.
- **Logs** for what Events don't hold (reads, refused requests), by IP and account, within their 30 days.

## 5. Notify

Under GDPR, a **personal-data breach** (personal data destroyed, lost, altered, disclosed or accessed without authorisation) is notified:

- **to the supervisory authority within 72 hours** of becoming aware, unless it's unlikely to be a risk to people (article 33). Late notice needs the reasons. If you don't know everything yet, notify what you know and add to it;
- **to the people affected without undue delay** when it's likely to be a **high** risk to them (article 34): plain words, what happened, what it means for them, what you've done and what they should do.

Processors notify the controller (the organisation) without undue delay; if your provider tells you of a breach, the clock is yours from then. Customers' own contracts may set shorter deadlines: check them.

**To the authority** (each authority has its own form; this is what it asks):

> On [date, time, timezone] we became aware that [what happened, e.g. "an attacker signed in to 14 accounts with passwords leaked elsewhere"] between [start] and [end]. Affected: about [number] people ([who: customers, their invitees]) and [number] records of [kinds of data: names, email addresses, IP addresses, project names]. Likely consequences: [e.g. "the attacker could read those accounts' workspaces"]. Measures taken: [suspended the accounts, reset their sessions, blocked the addresses, …]. Measures for those affected: [told them to change their passwords, …]. Contact: [name, email, phone].

**To the people affected:**

> Subject: Security incident affecting your [app] account
>
> On [date] we found that [what happened, in one sentence]. This affected your account: [what of theirs was involved]. We have [what you did: signed out every session, …]. Please [what they should do: set a new password, turn on two-factor authentication, be wary of emails asking for …]. We're sorry. If you have questions, reply to this email or write to [contact].

Don't put details an attacker could use (which defence failed, how) in a public notice.

## 6. Record

Every breach goes in the organisation's **breach register** (article 33(5)), **even one not notified**: the facts, its effects, what was done, and why it was or wasn't notified. Keep with it the Events exports, the relevant audit lines and log excerpts, the timeline (aware, contained, notified) and the decisions with who made them. The app doesn't hold the register: it's the organisation's.

## 7. Review

Within two weeks, without blame:

- **What would have caught it sooner?** A Monitoring signal or threshold, an alert (A5), a rate limit or quota, a check in Integrity.
- **What made it worse or slower?** A missing action, a step this runbook didn't have, access people needed and didn't have, or had and shouldn't.
- **What changes:** a plan for each fix, and this page updated.
