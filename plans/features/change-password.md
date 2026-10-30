# Change password

Status: **implemented 2026-10-30.** Before [events](events.md).

## Why

Account & security (`pages/app/settings.tsx`) had a Password section with a "Protected" badge but no way to change it; the badge is replaced by the Change password button. Users should be able to set a new password while signed in, after proving they know the current one.

## Flow

A "Change password" button in the Password section ("Change your account password.") opens a two-step `<Dialog>`:

1. **Current password** → [Next]. The API checks it. Wrong → inline error in the dialog, stay on step 1.
2. **New password** + **Confirm new password** → [Confirm]. The client checks the two match and are 12+ characters; the API checks the current password **again**, validates the new one, and saves it.
3. Success → the dialog closes and a "Password updated. Other sessions were signed out." message is shown (a `toast.success`, since [toasts](toasts.md) land first).

Step 1 only gives faster feedback. The API keeps no "step 1 passed" state, so step 2 must re-send and re-check the current password. Anyone can call the step-2 endpoint directly, so that check is the one that protects the account.

## API

| Endpoint | Body | Success | Failure |
| --- | --- | --- | --- |
| `POST /api/auth/password/verify` | `{ "password" }` | `204` | `422` "Current password is incorrect." (`field: "password"`) |
| `PUT /api/auth/password` | `{ "currentPassword", "newPassword" }` | `200 { user, csrfToken, expiresAt, signedInAt }` | `422` with `field`: `currentPassword` (wrong) or `newPassword` (too short/long, or unchanged) |

- Both endpoints: `requireSession` + `limitAuth` (IP quota) + `accountLimit` on the session user's email, so an attacker with a hijacked session can't brute-force the password through `verify`. Each attempt counts, success or failure, as it does for login.
- **Not 401** for a wrong current password: the web client treats 401 as "session expired" and redirects to login (`use-resource.ts`, `loaders.ts`). Use 422.
- Existing `security` middleware already enforces Origin, JSON content type and CSRF; bodies stay under the 16 KiB limit; passwords never logged.
- New password rules match registration (12+ Unicode characters, ≤ 128 UTF-8 bytes), and it must differ from the current one (decision 3).
- `confirm` is a UI check only; the API doesn't need it.

### Saving (`store.UpdatePassword`)

In one transaction with the user row locked `FOR UPDATE` (same pattern as `CreateSession`):

1. Read the current hash, verify `currentPassword` against it (Argon2id, constant time).
2. Write the new hash (fresh salt via `auth.HashPassword`).
3. Delete **every** session for the user, including the current one.

Then `startSession` issues a fresh session token and CSRF token for this browser (rotation, like login), returned in the shape of `/api/auth/me` (login and register now return that shape too). The client passes it to `useRenewSession()`, so later mutations send the new CSRF token.

Signing out other sessions matters: if someone learned the old password and signed in, changing it should kick them out. Rotating the current token means a stolen cookie for this browser also stops working.

Hashing cost: one Argon2id verify on step 1, one verify + one hash on step 2. That stays within the existing 4-concurrent hashing bound (503 when busy).

## UI

- `pages/app/settings.tsx`: a local `ChangePasswordDialog` (it's only used on this page) built from `Dialog`, `Fieldset`/`Field`/`Label`/`ErrorMessage`, `Input type="password"` and `Button loading`.
- Password-manager support: a hidden `username` field with the email (`autocomplete="username"`), `autocomplete="current-password"` on step 1 and `"new-password"` on step 2.
- Pending: `<Button loading>` disables double submits; Back on step 2 returns to step 1.
- Errors: wrong current password on step 2 (e.g. changed in another tab meanwhile) sends the user back to step 1 with the error. 429/503 show the server's message inline.
- The current password lives only in the dialog's React state and is cleared when the dialog closes or succeeds; never in storage, URLs or SWR keys.
- `lib/api/auth.ts`: `api.auth().verifyPassword(password)` and `api.auth().updatePassword({ currentPassword, newPassword })`; paths in `paths.ts`.

## Tests

- Go integration (`httpapi`): verify succeeds and fails with 422; update with the wrong current password changes nothing; success means the new password logs in and the old one doesn't; other sessions are revoked and the old cookie is rejected; the new cookie and CSRF token work; rate limit returns 429; no session gives 401; missing CSRF or a bad Origin is rejected; a too-short new password gives 422; a new password equal to the current one gives 422 and changes nothing.
- Vitest: the dialog's steps, mismatch error, back-to-step-1 when the server says the current password is wrong.
- Playwright: register → change password → log out → log in with the new password.
- Docs: `docs/api/auth.md` (Passwords, Sessions, rate-limit table), `docs/api/endpoints.md`.

## Not included

- **Email notification** ("your password was changed"): there's no email delivery yet (`docs/api/auth.md`, Not implemented). Normally recommended; add it once email exists.
- Password recovery / reset by email: separate feature, needs email.
- Breached-password check (HIBP k-anonymity): not now (decision 4).
- **Events:** this ships before [events.md](events.md). When events land, `UpdatePassword` records `auth.password_changed` in its transaction, and both endpoints record `auth.password_checked` (`ok`, `step`).

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Two-step dialog or one form? | Two-step. Step 2 re-checks the current password. |
| 2 | Rate limit | Share the login `account:` quota (10 per 15 min) plus the `limitAuth` IP quota. |
| 3 | New password equal to the current one | Refused: 422 "Choose a password different from your current one." Checked after the current password is verified, in the same request. |
| 4 | Breached-password check | Not now. |
| 5 | After success | Sign out all other sessions; keep this browser signed in on a rotated session and CSRF token. |
| – | Wrong current password status | 422, not 401 (401 means "session expired" to the client). |
| – | Concurrency | Verify and update in one transaction with the user row locked. |
| – | Notification email | Later, as a listener on `auth.password_changed` ([events.md](events.md)) once email delivery exists. |
