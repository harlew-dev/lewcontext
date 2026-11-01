# Findings: A6, admin actions

Plan: [actions](../actions.md) (on the events branch it has moved to `implemented/admin/actions.md`). Built on the **events** worktree (`../lewcontext-events`, branch `admin-run/events`), moved there from main to run in parallel. Every recommendation in its Decisions table was taken as accepted. Screenshots are in [actions/](actions/).

## What shipped

Every action, except the housekeeping tier (acknowledge an alert, silence it, resolve an error group), which waits for A4 and A5:

| Action | Where |
| --- | --- |
| **Sign out everywhere**, for an account or another admin | User page, Admins page |
| **Reset MFA** (never for an admin) | User page |
| **Suspend / unsuspend an account** | User page, and the CLI's `admin suspend` / `unsuspend` |
| **Retry a delivery, or failed deliveries in bulk** (the last 7 days, at most 1,000) | Mail |
| **Block an IP address or range**, for 1 hour, 24 hours, 7 days or 30 days | A new **IP blocks** page |
| **Revoke a workspace's invitations** | Workspace page |
| **Sign everyone out** (decision 9) | CLI only: `admin sessions revoke-all` |

Every account-and-access action goes through `signedIn.Action`: the `operator` role, a fresh code (step-up), a 10–500 character reason, and `admin-action:per-user`. Each records one `admin.*` event with the reason, in the same transaction as the change.

| Commit | What |
| --- | --- |
| `7bbcf1b` | Merge of main |
| `ba87fba` | Dead-link list entries removed; links the merge broke fixed |
| `ae94bda` | Migrations 060 (`user_suspensions`) and 061 (`ip_blocks`), factories, store, events |
| `6186fa3` | The API, IP-block enforcement, `Housekeeping`, the registry |
| `49d256a` | CLI: `suspend`, `unsuspend`, `sessions revoke-all` |
| `ba95925` | Web: action zones, dialogs, the IP blocks page |
| `061224e` | Playwright `admin-actions.spec.ts` |
| `0ddd6f5` | Docs, AGENTS.md; the plan implemented and moved |
| `9ac5892` | Signing out also ends sign-ins waiting for a code; a failed IP-block load at startup logs instead of stopping the server |

**How it looks:**
- [a user's Actions](actions/user-page-actions.png);
- [the suspend dialog](actions/suspend-dialog.png);
- [the IP blocks page](actions/ip-blocks-page.png);
- [what a suspended person sees at sign-in](actions/suspended-sign-in.png).

## Stronger than the plan asked

- **Suspension blocks new sessions too,** not only existing ones. Session lookups skip a suspended account, and creating a session checks under the same row lock suspension takes. Reset links and pending sign-in challenges are deleted.
- **A suspended account is never an admin.** `suspend` refuses an admin ("revoke admin first"), and `grant` refuses a suspended account.
- **Signing another admin out also ends their app sessions** (a lost laptop holds both).
- **Reset MFA needs `confirmed: true` in the API,** not only a ticked box in the dialog.
- **IP blocks:**
  - a range must be written from its first address;
  - a range overlapping `TRUSTED_PROXIES` is refused;
  - at most 1,000 blocks are live;
  - the admin door is exempt only for a request holding the key or an assets pass. Without the key, `/admin…` from a blocked address gets the same 403 as any URL.

## Where the build differs from the plan (recorded in the plan)

- **Suspension has its own table** (`user_suspensions`), not columns on `users`, so it records who suspended and doesn't collide with the ui branch's `users` edits.
- **New store events `auth.suspended` and `auth.unsuspended`** sit beside the `admin.user.*` ones.
- **A standalone IP blocks page** stands in for "Block this address" on A4's IP page, which doesn't exist yet.
- **409 when an action has nothing to do** (already suspended, nothing to revoke, …).
- **The store verb is `Requeue`,** because `Retry` is already the event queue's.

## For you to decide

1. **"You already have an account" email for suspended accounts.** Today it's still sent if someone signs up with a suspended account's address. Suppressing it wouldn't change what sign-up answers (the mail comes from a listener), but the suspended person then hears nothing. _Recommendation: keep sending; the email tells them nothing new._
2. **New convention-check entries** (`docs/checks.md` asks for your agreement):
   - store verbs `Suspend`, `Unsuspend`, `Requeue`;
   - the named action `Retry`;
   - stored event resources `admin.user`, `admin.workspace`, `admin.delivery`, `admin.ip`, `admin.admin`;
   - the `Housekeeping` registrar.

## Fixed without asking

- **A race let an invitation survive a suspension:** an invitation created at the same instant as the suspension. Creating an invitation will lock the inviter's row, as creating a session already does. _Being fixed with the review's findings._

## Checks

All passed:
- `make check`;
- `make test`: all Go packages; Vitest 341, after one flaky run of an Events page test that wasn't touched and passed alone 3 of 3 times;
- `make build`;
- e2e: 24 of 24.

## Security review

The independent reviewer found **nothing high-severity**. It confirmed sound:
- **Suspension is refused everywhere:** session lookups, session creation under a lock, reset links, MFA challenges, admin sessions and grant.
- **Sign-in answers:** a wrong password for a suspended account answers exactly as for any account.
- **Reset MFA** refuses admins and yourself, and needs the confirmation.
- **IP blocks:** each width, proxy and own-address refusal; the 403 is byte-identical everywhere; a forged header neither blocks nor unblocks anyone.
- **Retries:** delivered mail is never resent, and old links are never sent.
- **The registry:** every mutation is registered through `Action`.
- **Events:** one per action, in the change's transaction.
- **URLs:** they hold only public IDs.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Low–medium | **A blocked address could test admin keys.** From a blocked IP, a wrong key got the block's 403, while the right key from outside `ADMIN_ALLOWED_IPS`, or the retired key, got the door's 404. That broke A2's "looks exactly like a wrong key" guarantees. Confirmed by probe | Only the current key from an allowed address is exempt from blocks; byte-identical tests added |
| 2 | Low | An invitation sent in the same instant as a suspension survived and was mailed | Creating an invitation locks the inviter's row and checks for suspension |
| 3 | Low | **A 30-day block answered 500 for about a month before each spring-forward** if the database runs in a local time zone ("30 days" is 719 hours across DST). Confirmed by SQL | The limit is written as 720 hours |
| 4 | Low | If the block list failed to load at startup, a server enforced nothing, with only an ERROR line every 30 s; one bad row emptied the whole list | Quick retries until loaded; bad rows skipped; last-loaded time logged |
| 5 | Low | `sessions revoke-all` left sign-ins waiting for a code | Those end too |
| 6–9 | Nits | Reset MFA "succeeded" for a never-confirmed setup; stale MFA docs; the 1,000-block cap could be exceeded under concurrency; the CLI's reason skipped the character rules | Fixed |

**Design note (your call):** the IPv6 cap is a /32, which is a whole ISP's allocation and far wider than IPv4's /16. Blocking one could cut off an entire mobile carrier. The confirmed cap stays, but the dialog warns above /48 and suggests /64 for one person.

_Fix results: filled in once committed._
