# Progress

What has shipped on branch `nimble-quartz-1f9`, and what's next. Nothing is pushed. Open questions are in [questions.md](questions.md).

## How each piece was built

1. **A builder sub-agent** reads AGENTS.md, the plan and every doc it touches, then builds the plan with tests and docs.
2. **The orchestrator** checks the diff.
3. **A separate security reviewer** sees only the diff and the plan, not the builder's summary, and probes it, often with throwaway tests.
4. **Its findings go back to the builder.**
5. **`make check`, `make test`, `make build` and the e2e suite run,** one heavy run at a time.
6. **One commit per plan.**

## Shipped

| Plan | Commit | Checks | Findings |
| --- | --- | --- | --- |
| A8: the admin pages as their own build | `942a846` | check, test, build; e2e 22/22 | [separate-build](../findings/separate-build.md) |
| A2: IP allowlist, key rotation, roles, Admins page | `f5c815f` | check, test, build; e2e 22/22 | [access-hardening](../findings/access-hardening.md) |
| A9: the Email blocks page and Unblock (the first admin write) | `b84eaa7` | check, test, build; e2e 23/23 | [email-blocks](../findings/email-blocks.md) ([screenshot](../findings/email-blocks-unblock-dialog.png)) |
| A3 phase 1: logs, request IDs, panic recovery, health | `10797fd` | check, test, build; e2e 23/23 | [observability](../findings/observability.md) |

Security reviews found:
- **A8:** one medium (admin code could slip past both build guards) and 7 lower.
- **A2:** one medium (the previous key failed open while the database was down) and 7 lower.
- **A9:** 8 lower.

All were fixed before committing, apart from the few listed as accepted in each findings file.

## In progress

- **A3, observability,** in three phases. Each is reviewed and committed on its own. The plan moves to `implemented/` after phase 3.
  1. Logs, request IDs, panic recovery, health checks: **committed `10797fd`**.
  2. Grouped errors (`app_errors`), errors from the browser: _building_.
  3. Metrics, server heartbeats, slow queries, `/metrics`.
- **Plans written and committed:** [policies](../../policies.md) (terms and privacy, `0a08ab5`) and the [Events page redesign](../../implemented/admin/events-page.md) (A10, `e1cd3b4`).

## The parallel plan (from 2026-11-01, about 05:00)

You asked for every open plan to be built, with their recommendations accepted for you to review in the morning, and for unrelated work to run in parallel git worktrees, merged in once each is finished.

| Line | Where | Branch | Order |
| --- | --- | --- | --- |
| Main | this checkout | `nimble-quartz-1f9` | A3 (**done**: phases 1–3, `10797fd`, `9ae4822`, `063d6d2`…`ec5ed7d`) → A4 monitoring (_part 1 of 3 building_: Overview, Security, Rate limits) → A5 alerts → A7 compliance |
| ui | `../lewcontext-ui` | `admin-run/ui` | the notes.md UI fixes (**done**, reviewed and fixed; [findings](../findings/notes-ui.md)) → policies (terms and privacy: **done**, reviewed and fixed; [findings](../findings/policies.md)) → captcha challenges (**built**, under review; [findings](../findings/captcha.md)) |
| events | `../lewcontext-events` | `admin-run/events` | the [Events page redesign](../../implemented/admin/events-page.md) (**done**, reviewed and fixed; [findings](../findings/events-page.md)) → convention checks (**done**; [findings](../findings/convention-checks.md)) → **A6 admin actions** (_building_, moved here from main to save time; "acknowledge an alert" waits for A5) |
| Last, on main | after the merges | | page-folders (`plans/features/page-folders.md`, another session's untracked file): it moves every page file, so it would collide with everything else |

**Keeping parallel lines apart:**
- **Separate dev databases:** the worktrees use `lewcontext_ui` and `lewcontext_events`. Tests and e2e already use databases of their own.
- **New migration numbers:** main 020–049, ui 050–059, events 060–069, so merges don't collide.
- **Shared test runs:** about 2 heavy test runs at once, shared across all lines.
- **Commits:** each line commits each change on its own branch. Each branch merges into `nimble-quartz-1f9` once its review and checks pass.

## Next, after A3 (your `plans/features/notes.md`; moved to the ui worktree)

- **Small UI fixes:**
  - the "Didn't receive the email?" modal;
  - a Back ghost button under the logo on auth pages, including 2FA;
  - "Forgot password" and descriptions tighter under their inputs and headings;
  - the admin logout button not danger;
  - copying shows a 3-second tooltip, app-wide;
  - Refresh buttons removed;
  - the project input validation fix.
- **The Events page redesign,** built from its plan.

## Not started (needs your decisions first)

A4 monitoring, A5 alerts, A6 actions and A7 compliance: their Decisions tables haven't been confirmed.
