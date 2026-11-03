# Findings: A9, the Email blocks page and Unblock

Plan: [email-blocks](../../implemented/admin/email-blocks.md), implemented 2026-11-01. Built by a sub-agent, checked by the orchestrator, then reviewed by a second agent that saw only the diff and the plan.

## What shipped

- **The Email blocks page** (`/admin/<key>/email-blocks`): search, sort, and whether the owner's block link still works. Every read is recorded as `admin.email_block.listed`.
- **Step-up** (`POST /api/admin/step-up`): a fresh authenticator or recovery code opens a 5-minute window on that one admin session.
  - It uses the same code check as the door's second step, so a code can't be replayed between the two.
  - It counts against `mfa:per-account`.
  - It records `auth.mfa.checked` with step `admin_step_up`, whether the code is right or wrong.
- **Unblock** (`DELETE /api/admin/email-blocks`, body `{email, reason}`):
  - it needs the `operator` role, the step-up window and `admin-action:per-user` (60 an hour);
  - it records `admin.email.unblocked` and `email.unblocked` (`by: "admin"`) in the same transaction as the delete;
  - the address never appears in a URL.
- **`AdminActionDialog`:** a required reason (10–500 characters), then a code when the API asks for one, then a retry. A6's actions will reuse it.
- **Unblock is hidden from viewers,** and the API refuses them (403).

## Decisions the orchestrator made (the plan didn't settle them)

1. **Step-up itself needs `operator`.** Every action behind it needs operator, so a viewer has nothing to step up for, and A2's route registry requires every admin write to name a role. Consequence for later: a future action open to viewers couldn't use step-up. A6 says every action needs operator, so this is consistent today.
2. **A session under the previous key (A2) may step up.** The session is live and a fresh code is still required.
3. **Step-up shares the door's code path and replay protection.** It isn't a copy.
4. **The step-up window is read on the database's clock** and belongs to one admin session. Signing out ends it.
5. **"This address isn't blocked" (404) counts against `admin-action:per-user`.** Otherwise a stolen operator session could test which addresses are blocked for free. Refusals before the step-up don't count.
6. **The address is normalised with sign-up's own function.** The reason is counted in Unicode characters.

## Where the build differs from the plan

- **A badly formed address** answers 422 on `email`.
- **The reason may contain line breaks and tabs, but no other control characters.** It's stored in a jsonb payload, which can't hold a NUL.
- **The index is `email_block_links (email, expires_at)`,** so "the newest live link" is read from the index.
- **A step-up racing a sign-out answers 401, and the code isn't used up.**
- **Once a step-up succeeds,** a later failure of the action doesn't ask for a code again.
- **`AppOrRecoveryCode` moved** from `pages/app/settings.tsx` to `components/app/mfa/mfa-setup.tsx`, because the dialog now uses it too (AGENTS.md rule 3).
- **A2's test-only operator route was removed.** `TestAdminRoles` now uses the real step-up route.

## Things to know

- **Free text in the audit trail.** Unblock's reason is written by an admin and kept in event payloads (noted under personal data in `docs/api/events.md`). If events are shipped to logs (A3, A7), the reasons go with them.
- **The step-up rate limit.** Step-up and the door share `mfa:per-account` (5 per 15 minutes). During an incident, an admin stepping up more than about 5 times in 15 minutes would be held back. Each step-up lasts 5 minutes, so this is unlikely.
- **Shared dev database.** The local dev database was rebuilt again for the migration edits. A Docker container (`lewcontext-app-1`, not started by this work) uses it; its data was reset, and its schema change is additive.
- **Another session is working in this checkout.** An untracked `plans/future/` folder appeared, from another session. It was left out of the commit, like `plans/features/page-folders.md`.
- **Outside this plan:** project names and descriptions don't refuse control characters, so a NUL probably answers 500. See [project-input-validation](project-input-validation.md).

## Security review

The independent reviewer found no high or medium defects. Its probes, run against the real database, confirmed:
- **Concurrent step-ups** with one TOTP code across two sessions of the same admin give exactly one 204.
- **Six concurrent unblocks** of one address give one 204, five 404s, and exactly one pair of events.

It also checked these and found them sound:
- step-up uses the door's own code check (`useConfirmedFactor`, under the TOTP row lock);
- the limit is counted before any code is checked;
- the window is bound to one session and read on the database clock;
- the guard order (Origin and JSON, key and allowlist, session and CSRF, role, step-up, limit, handler);
- `LIKE` escaping, the sort allowlist, and the index for `linkExpiresAt`;
- moving `AppOrRecoveryCode` didn't change either build's admin boundary;
- the AGENTS.md edits match the code.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Low | **Step-up is a lockout lever:** it counts against `mfa:per-account` with only a session, not the password, so a stolen operator cookie sending wrong codes keeps the real admin at 429 on every code check until the session is revoked | The shared limit stays (a confirmed decision). Five wrong step-up codes now end that admin session, so a stolen cookie gets one burst; the remaining lever is documented |
| 2 | Low | **Nothing made future actions use step-up and the action limit.** They were composed by hand, so A6 could add an action with only `Role` | A `signedIn.Action(...)` helper; the source test and the startup check require it for every operator mutation |
| 3 | Low | **The reason accepted invisible and direction-changing characters** (U+202E, U+200B, U+2028), which could make an audit record read misleadingly. Not XSS | Format characters and line/paragraph separators refused, apart from `\n`, `\r` and `\t` |
| 4 | Nit | **"Counting the 404 stops free probing" was moot,** since the list itself shows blocks to any admin (each read recorded) | The 404 still counts; the rationale reworded |
| 5 | Nit | **The step-up window survived a role change** | Cleared on a role change |
| 6 | Nit | **The dialog's copy said only "a new email" can re-block;** the owner's existing live links can too | Copy corrected |
| 7 | Nit | **The personal-data list in `events.md` missed `q`** on admin list reads, which can be a blocked person's address | Added |
| 8 | Nit | **The dialog kept stale state** after a 404 or a wrong code | The list refreshes on a 404; the code boxes clear on a wrong code |

**Not a defect:** on a 720px-tall screen the dialog's Unblock button is below the fold. The backdrop scrolls, so it can be reached.

**Also applies outside A9:** finding 3's format characters apply to [project-input-validation](project-input-validation.md) too.

**Fix results.** All eight are fixed.

- **A new column, `admin_sessions.step_up_failures`.** Five wrong codes delete the session and record a new event, `admin.session.ended` (reason `step_up_failures`).
- **`signedIn.Action(...)`.** The builder injected a `Role(operator)`-only mutation. The source test failed and the router panicked at startup; the injection was then reverted.
- **New tests:**
  - concurrency: one code sent 8 times at once gives exactly one 204; 6 unblocks at once give one pair of events;
  - the window ends with a password change and with an MFA reset;
  - a step-up racing a sign-out doesn't use up the code.
- **The dialog now fits a 720px-tall screen.** The notice became the code field's description, and the reason box is 2 rows ([screenshot](../../screenshots/email-blocks/email-blocks-unblock-dialog.png)).
- **All checks passed:**
  - `make check`;
  - `make test`: 20 Go packages, 290 Vitest tests;
  - `make build`;
  - e2e: 23 of 23.

**For you to confirm: a right code doesn't reset the failure count.** Each admin session gets five wrong step-up codes in its whole life, at most 8 hours. That bounds a stolen cookie to one burst. The cost is that an admin who mistypes five times in one session must sign in again. The alternative is to reset the count on a right code: friendlier, but a stolen session that also holds one right code could then guess again.
