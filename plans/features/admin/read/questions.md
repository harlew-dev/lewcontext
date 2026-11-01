# Questions for you

Start here. Each question links to the file that explains it. Also in this folder:
- [progress.md](progress.md): what shipped, its commits and check results;
- [discoveries.md](discoveries.md): things found along the way.

Last updated 2026-11-01, during the overnight run.

## Waiting on you (nothing is blocked; each has a default in place)

| # | Question | Default now | Details |
| --- | --- | --- | --- |
| 1 | **Should a right step-up code reset the wrong-code count?** Today each admin session gets 5 wrong step-up codes in its whole life, then it ends. This is safer against a stolen cookie, but an admin who mistypes 5 times in one 8-hour session must sign in again | No reset | [email-blocks findings](../findings/email-blocks.md#security-review) |
| 2 | **Keep the event name `admin.session.moved`, or rename it `admin.session_moved`?** The plan used the first; `docs/naming.md` was amended to allow it | Keep `admin.session.moved` | [access-hardening findings](../findings/access-hardening.md#for-you-to-confirm) |
| 3 | **Bind the dev server (`make dev`, `:3000`) to `127.0.0.1` only?** Today it listens on every interface, so anyone on your LAN can load the dev app's code, admin pages included (not data: the API still checks the key). Say no if you open the dev server from another device | Unchanged (every interface), documented | [separate-build findings](../findings/separate-build.md#security-review) |
| 4 | **Refresh buttons (`plans/features/notes.md`).** I'm removing the Refresh buttons, but keeping **Retry when a load fails**, which AGENTS.md rule 8 requires. Is that what you meant? | Remove Refresh, keep Retry on failure | — |
| 5 | **"Didn't receive the email?" modal (`plans/features/notes.md`).** It will say "your address might be blocked" to *everyone*, never only to blocked addresses, because AGENTS.md forbids revealing a block | Same text for everyone | — |
| 6 | **Terms of service and privacy policy:** 19 decisions. You first asked for a plan only; it's now built on the ui line as recommended, since you asked for every plan. Main points: `POLICIES=on\|off` (default off) switched at runtime; Markdown files in `apps/web/content/policy/` turned into pages at build time with `marked`, a **new devDependency** that never reaches the browser; a required checkbox at sign-up; accepted versions stored on `users`; existing accounts aren't asked | Build as recommended | [policies plan](../../policies.md#decisions) |
| 7 | **Events page redesign (A10):** 22 decisions. Your four (the multi-select with groups, the date dropdown, the Filters dialog with badges, graphs) are marked decided. Ones worth a look: **"Today" and "Yesterday" are UTC days** (decision 5); **the time range sits beside the Filters button**, not inside the dialog, so the filter changed most often is one click (15); **the default window becomes the last 24 hours**, with All time still available and Activity links passing `window=all` (6); **charts only for 7 days or less** (10, the admin rule on bounded queries); your example groups `admin.read` and `user.read` don't exist, so groups follow the real event names (`auth.*`, `auth.mfa.*`), plus `admin-reads` | Build as recommended (building in the events worktree) | [events-page plan](../../implemented/admin/events-page.md#decisions-confirmed-2026-11-01-as-recommended) |
| 8 | **Request IDs from a proxy (A3, changes its decision 3).** The plan believed a trusted proxy's `X-Request-ID`. The security review showed Cloudflare, ALB and default nginx pass a *client's* header through, so an attacker could copy a victim's ID into the audit trail. Now the server always makes its own ID and logs a proxy's beside it as `proxyRequestId`, never on events. The alternative is an opt-in `REQUEST_ID_HEADER` for proxies known to overwrite the header | Always our own ID | [observability findings](../findings/observability.md#security-review) |
| 9 | **Sign out styling (your note "admin logout button not danger variant").** Sign out is being made plain in both the admin and the app account menus, since signing out isn't destructive, and the rule in `forms-and-actions.md` changed to match. Only admin's? | Both plain | [notes-ui findings](../findings/notes-ui.md#corrected-after-your-notes-were-misread) |
| 10 | **Back on the login page** goes to the home page, which the logo already does; the admin password step has no Back, since the admin door is a secret URL. Keep both as they are? | Keep | [notes-ui findings](../findings/notes-ui.md#open-for-you) |

## Every open plan: recommendations taken as accepted

On 2026-11-01 you asked for every open plan to be built while you sleep, to review in the morning. So each plan's Decisions table is being taken **as recommended**:
- A4 monitoring, A5 alerts, A6 actions, A7 compliance;
- captcha-challenges, convention-checks, page-folders;
- the two new plans: [policies](../../policies.md) (terms and privacy) and the [Events page](../../implemented/admin/events-page.md).

Any decision I'd change, or that a builder had to change, is listed in that plan's findings file under `../findings/`. It will also be summarised here as each plan ships.

## Fixed or decided without you (tell me if you disagree)

- **The assets pass is always signed with the current admin key** (A8), so sessions under the previous key (A2) keep working. See [separate-build findings](../findings/separate-build.md#decisions-made-while-building-worth-your-review).
- **The IP allowlist also covers `/admin-assets/`** (A2), so a stolen assets pass doesn't work from outside the list.
- **Step-up and Unblock need `operator`** (A9). A2 shipped first, so A9's "every admin can unblock until roles exist" waiver was dropped.
- **Five wrong step-up codes end that admin session** (A9). This was added after the security review: a stolen cookie could otherwise lock the real admin out of every code check.
- **"This address isn't blocked" counts against the admin action limit** (A9).
- **Project names and descriptions will refuse control characters,** like workspace names do. Today a NUL byte probably answers 500. This is a small fix, done with the notes work. See [project-input-validation](../findings/project-input-validation.md).
- **Two AGENTS.md lines updated for A8:** `make dev`'s processes, and what `internal/web` owns.
