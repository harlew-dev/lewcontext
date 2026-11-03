# A code instead of the password to change it when MFA is on

Status: **implemented 2026-10-31** as planned; see [How the build differs](#how-the-build-differs). Docs: [auth.md](../../../docs/api/auth.md#passwords), [endpoints.md](../../../docs/api/endpoints.md), [events.md](../../../docs/api/events.md), [rate-limits.md](../../../docs/api/rate-limits.md), [api/testing.md](../../../docs/api/testing.md), [ui/testing.md](../../../docs/ui/testing.md). Follows [change password](change-password.md) and [two-factor authentication](mfa.md) (whose decision 7 left step-up codes for later).

## Why

Changing the password while signed in needs only the current password. With two-factor authentication on, the second factor is the stronger proof: a password can be reused, phished or leaked, a current code means having the authenticator. So with MFA on, changing the password asks for a code **instead of** the current password (decision 1), like GitHub's sudo mode. Someone with a session and a phished password can no longer set a new one and sign the owner out everywhere.

Without MFA nothing changes: the current password, as now.

## Flow

The dialog in Account settings → Password, decided by `session.user.mfaEnabledAt` when it opens:

**MFA off** (unchanged): current password → [Next] (`POST /api/auth/password/verify`) → new password + confirmation → [Confirm].

**MFA on**:

1. **New password** + **Confirm new password** → [Next]. The client checks only that they match and are 12+ characters. Nothing is sent.
2. **Code**: "Enter the code from your authenticator app to confirm." The six boxes, plus "Lost your device? Use a recovery code" (the existing `AppOrRecoveryCode`) → [Confirm]. `PATCH /api/auth/password {newPassword, code}`.

A wrong code stays on step 2 with the error under the boxes; a refused new password (the current one again) goes back to step 1. What was typed is kept.

The code comes last and only the change checks it: a TOTP code works once (`last_used_step`) and goes stale in about 90 seconds, so it can't be checked by an earlier step and again by the change.

## API

`PATCH /api/auth/password` takes `newPassword` and **either** `currentPassword` **or** `code`, depending on the account:

| MFA (in the locked user row) | Needs | Ignores | Wrong or missing |
| --- | --- | --- | --- |
| off | `currentPassword` | `code` | 422 `field: "currentPassword"`, as now |
| on | `code` (an authenticator or recovery code, read by `SecondFactor`) | `currentPassword` | 422 `field: "code"`: `WrongMFACode`, or "Enter the code from your authenticator app, or a recovery code." when blank |

The server decides, from the user row it locks, not from what the client sends. With MFA on, a correct current password and no code is refused, so the API can't be used to skip the code. `POST /api/auth/password/verify` is unchanged, and the web app uses it only with MFA off.

### Order of checks (`account.Password.Update`)

1. Decode, check `newPassword`'s bounds and strength. With MFA on (per the session's user): a blank `code` is 422 on `code` before any limit or query. With it off: `currentPassword` out of bounds is 422 on it, as now.
2. Limits: `auth:per-account` with MFA off (as now), `mfa:per-account` with it on. The route stays behind `LimitAuth` (`auth:per-ip` and a hashing slot), since both paths hash the new password.
3. `Repo.ChangePassword(ctx, user, sessionHash, confirm, replace)` in one transaction with the user row locked. `confirm` is a `PasswordConfirmation` holding the factor and a password check:
   1. Lock the authenticator row. If MFA is on, use up the factor (`useConfirmedFactor`); a wrong one is `ErrWrongCode` and nothing changes. If it's off, run the password check against the current hash; a wrong one is the check's error.
   2. `replace(current)`: refuse a new password equal to the current one (an Argon2id check of the new password against the current hash), hash it, save it, delete the reset link and every session, as now. Refusing here rolls back, so the code isn't used up.
   3. Return which check ran (`Confirmed{MFA bool, FactorUsed}`), for the events.
4. Record `auth.password_checked` (step `update`) when the password was checked, or `auth.mfa.checked` (step `password_update`, with `recoveryCodesLeft` for a recovery code) when the code was. Recorded after the commit or rollback, as now.
5. 200 with the new session, as now.

**The session and the row disagree** (MFA turned off in another tab between the session load and the lock): the row decides, so it asks for the password and answers 422 on `currentPassword`. The web app sends the user back to the password step. MFA turned on elsewhere signs this session out (401).

## Data

No new tables or columns.

## Events

- `auth.mfa.checked` gets the step `password_update` (`events.MFAStepPasswordUpdate`).
- With MFA on, `auth.password_checked` isn't recorded for the change, since the password isn't checked.
- `auth.password_changed` is unchanged.

## Rate limits

`mfa:per-account` (5 per 15 minutes, per user) now also covers changing the password with MFA on, instead of `auth:per-account`. `PATCH /api/auth/password` stays an unmetered recovery route behind `LimitAuth`.

## Web app

- `pages/app/settings.tsx` `ChangePasswordDialog`: with MFA on, the steps are `"new"` then `"code"`; with it off, `"current"` then `"new"`, as now. `field: "code"` shows under the code; `field: "currentPassword"` goes to the password step. The code is cleared with the passwords on close and success.
- `lib/api/auth.ts`: `changePassword` takes `{ newPassword } & ({ currentPassword } | { code })`.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| A stolen session plus a phished password changes the password and locks the owner out | With MFA on, the password isn't enough: a current code is needed |
| A stolen session plus the second factor, without the password, changes the password | **Accepted** (decision 1), as GitHub's sudo mode accepts it. Where the password manager also makes the codes, whoever has one has the other, so asking for both wouldn't help. What's left is narrower: the unlocked laptop **and** phone with the password kept elsewhere, or a downloaded recovery-codes file on the same computer as the session. Each recovery code works once and its use is recorded |
| Skipping the code by sending the password | The server picks the check from the locked row; with MFA on the password is ignored |
| Guessing codes through this route | `mfa:per-account`, shared with every other code check |
| Replaying a code | `useConfirmedFactor` under the row lock: each TOTP step once, each recovery code once |
| A recovery code alone (with a session) changes the password | Accepted, as for sign-in: a recovery code stands in for the authenticator. It's used up, and `recoveryCodesLeft` is recorded |
| Learning the current password through "must differ" | That check runs only after the code (or password) was right |

## Tests

- **Go integration** (`endpoints/account/password_integration_test.go`), with `apitest.WithMFA`:
  - the right current password and no code → 422 `code`, nothing changes, no event;
  - a wrong code → 422 `code`, the password and sessions unchanged, `auth.mfa.checked ok=false step=password_update`;
  - a right TOTP code without `currentPassword` → 200, the new password signs in (with a code), the same code refused for a second change;
  - a recovery code → 200, used up, `recoveryCodesLeft` recorded;
  - the new password equal to the current one → 422 `newPassword`, and the code still works afterwards (rolled back);
  - `mfa:per-account` exhausted → 429;
  - MFA off: a `code` alone → 422 `currentPassword`; the existing tests unchanged.
- **Vitest** (`settings.test.tsx`): with MFA on, no password step, the code step sends `{newPassword, code}` and no password; a 422 `code` stays on the code step; a 422 `newPassword` goes back. Without MFA, as now.
- **Playwright** (`mfa.spec.ts`): with MFA on, change the password with a computed code, then sign in with the new password and a code.

## Docs

`docs/api/auth.md` (Passwords, Two-factor authentication), `docs/api/endpoints.md`, `docs/api/events.md`, `docs/api/rate-limits.md`, `docs/api/testing.md`, `docs/ui/testing.md`.

## Decisions

1. **With MFA on, a code instead of the current password; with it off, the password.** **Confirmed 2026-10-31.**
2. **The code is the last step and only the change checks it.** *Recommended:* a code works once and goes stale.
3. **Recovery codes accepted**, as everywhere else a code is asked for.
4. **Deleting a workspace or the account stays password-only** (the MFA plan's decision 7).
5. **Verify stays password-only**, used only with MFA off.

## Not included

- Codes for deleting a workspace or the account.
- An email saying the password changed: not built, and separate from this.
- Password reset by link: unchanged; signing in afterwards still asks for the code.

## How the build differs

- The same-password check compares the new password with the current hash (one more Argon2id check) on both paths, since with MFA on there is no current password to compare it with.
- The Playwright flow changes the password with a recovery code, not a TOTP code: sign-in had already used this step's and the next step's codes.
