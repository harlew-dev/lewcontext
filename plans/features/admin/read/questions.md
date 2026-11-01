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
| 4 | **Refresh buttons ([notes.md](../../notes.md)).** I'm removing the Refresh buttons, but keeping **Retry when a load fails**, which AGENTS.md rule 8 requires. Is that what you meant? | Remove Refresh, keep Retry on failure | — |
| 5 | **"Didn't receive the email?" modal ([notes.md](../../notes.md)).** It will say "your address might be blocked" to *everyone*, never only to blocked addresses, because AGENTS.md forbids revealing a block | Same text for everyone | — |
| 6 | **Terms of service and privacy policy:** decisions to confirm. This is a plan only, nothing will be built | — | [legal pages plan](../../legal-pages.md) _(being written)_ |
| 7 | **Events page redesign:** decisions to confirm. Your notes already settle the multi-select with groups, the date dropdown, the Filters dialog with badges, and graphs. The rest are my recommendations, and I'll build with them tonight unless you say otherwise | Build with recommendations | [events-page plan](../events-page.md) _(being written)_ |

## Fixed or decided without you (tell me if you disagree)

- **The assets pass is always signed with the current admin key** (A8), so sessions under the previous key (A2) keep working. See [separate-build findings](../findings/separate-build.md#decisions-made-while-building-worth-your-review).
- **The IP allowlist also covers `/admin-assets/`** (A2), so a stolen assets pass doesn't work from outside the list.
- **Step-up and Unblock need `operator`** (A9). A2 shipped first, so A9's "every admin can unblock until roles exist" waiver was dropped.
- **Five wrong step-up codes end that admin session** (A9). This was added after the security review: a stolen cookie could otherwise lock the real admin out of every code check.
- **"This address isn't blocked" counts against the admin action limit** (A9).
- **Project names and descriptions will refuse control characters,** like workspace names do. Today a NUL byte probably answers 500. This is a small fix, done with the notes work. See [project-input-validation](../findings/project-input-validation.md).
- **Two AGENTS.md lines updated for A8:** `make dev`'s processes, and what `internal/web` owns.
