# Two-factor authentication (TOTP)

Status: **implemented 2026-10-31** with the confirmed decisions below; see [How the build differs](#how-the-build-differs). Docs: [auth.md](../../../docs/api/auth.md#two-factor-authentication), [endpoints.md](../../../docs/api/endpoints.md), [events.md](../../../docs/api/events.md), [rate-limits.md](../../../docs/api/rate-limits.md), [database.md](../../../docs/api/database.md), [seeding.md](../../../docs/api/seeding.md), [api/testing.md](../../../docs/api/testing.md), [api/README.md](../../../docs/api/README.md), [deployment.md](../../../docs/deployment.md), [testing.md](../../../docs/testing.md), [ui/README.md](../../../docs/ui/README.md), [ui/design.md](../../../docs/ui/design.md), [ui/api.md](../../../docs/ui/api.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/testing.md](../../../docs/ui/testing.md), and AGENTS.md (rule 5, the security invariants). Ships before [admin access](admin-access.md), which requires it for every admin. Independent of [email delivery](email-delivery.md): nothing here sends mail.

## Why

A password alone is the only thing between an attacker and an account. Passwords get reused, phished and leaked. A second factor means a stolen password isn't enough to sign in. The admin dashboard needs it before it can exist: an admin account can read every user's data.

**Authenticator apps (TOTP, RFC 6238) plus single-use recovery codes**, as the first and only method (decision 1):

- **No dependency on email or SMS.** Email isn't delivered in production yet ([deployment.md](../../../docs/deployment.md#email)), and SMS codes can be stolen by SIM swaps.
- **Every authenticator app supports it:** 1Password, Bitwarden, Google Authenticator, Authy, Microsoft Authenticator, and the iOS and macOS password apps.
- **Small and self-contained.** TOTP is an HMAC of a counter. About 50 lines of standard library Go, tested against the RFC's own vectors, with no Go dependency.
- **Passkeys (WebAuthn) come later** as a second method beside TOTP. They resist phishing better, but need a WebAuthn library and their own enrolment and recovery flows. The tables below leave room for them.

## Flow

### Turning it on (Account settings → Two-factor authentication)

Optional for every user. The settings section reads "Two-factor authentication" with an **Enable** button.

```
[Enable]  → dialog, step 1: current password
  POST /api/auth/mfa/setup {password}
    limitAuth + auth:per-account, records auth.password_checked (step mfa_setup)
    already on?  → 409
    new 160-bit secret, encrypted; replaces any pending (unconfirmed) one
    → 200 {secret: "JBSW…", uri: "otpauth://totp/lewcontext:ada@example.com?…"}

step 2: <MfaSetup>: the QR code and setup key, hidden until "Reveal QR code", + "Code from your app"
  POST /api/auth/mfa {code}
    locks the user, checks the code against the pending secret, marks it confirmed,
    makes 10 recovery codes, deletes every OTHER session, records auth.mfa.enabled
    → 201 {recoveryCodes: ["k7p2m-x9qfr", …]}     ← shown once, never again

step 3: <MfaSetup>: the recovery codes, hidden until "Reveal recovery codes",
        Copy / Download, "I've saved these codes" → Done
```

A pending secret that isn't confirmed within 15 minutes is deleted by `Store.Cleanup`. Starting again replaces it.

**Admins** set up MFA the same way, or at their first admin sign-in, where the door runs the same setup stage before letting them in ([admin access](admin-access.md#admin-sign-in)). Both places use the same setup code, described next.

### One setup, two entry points

Setting up MFA from account settings and at the admin door share everything except **how the user is identified**:

| | Account settings | Admin door (first sign-in) |
| --- | --- | --- |
| Who | the session user, after their password (`setup`'s body) | the user behind an `admin_setup` challenge: password already checked at the door, inside the setup window |
| Start | `POST /api/auth/mfa/setup {password}` | `POST /api/admin/login/mfa/setup` |
| Confirm | `POST /api/auth/mfa {code}` | `POST /api/admin/login/mfa {code}`, which also starts the admin session |
| Response shapes | `{secret, uri}` then `{recoveryCodes}` | the same, plus the admin session on confirm |
| Web | `pages/app/settings.tsx`, in a `Dialog` | `pages/admin/[key]/index.tsx`, as the sign-in's setup step |

- **Shared Go:** two handler helpers in `httpapi/mfa.go`, `s.beginMFASetup(w, r, user, step)` and `s.confirmMFASetup(w, r, user, step) (codes []string, ok bool)`, over the store's `SetupMFA` and `EnableMFA`. The routes are thin wrappers that find the user and call them. Encryption, code checks, recovery codes, events and limits exist once.
- **Shared web:** `MfaSetup` (`components/app/mfa/mfa-setup.tsx`) takes `start()` and `confirm(code)` functions and renders the scan step, code entry and recovery codes. Settings passes the account API calls, and the admin sign-in passes the admin ones. Both get the same reveal behaviour, copy, download and acknowledgement.
- **Why not literally one route:** the admin door's routes must stay behind `X-Admin-Key`, and must work without an app session. `/api/auth/mfa*` must never accept an admin door challenge in place of a session. Keeping two entry points over one implementation keeps those two rules simple and testable (decision 11).

### Signing in

```
/auth/login  email + password
  POST /api/auth/login            (unchanged limits and checks)
    wrong password, unknown email → 401, as today
    right password, MFA off       → session, as today
    right password, MFA on        → no session. A challenge:
        256-bit token; digest in mfa_challenges (user, purpose "login", 5 minutes, 5 tries)
        cookie lewcontext_mfa (__Host- in production), HttpOnly, SameSite=Strict, 5 minutes
        auth.login_attempted with result mfa_required
        → 200 {mfaRequired: true}

same page, step 2: "Authentication code"  ("Use a recovery code instead" switches the field)
  POST /api/auth/login/mfa {code}
    challenge cookie missing, expired or used up → 401 "Sign in again." (back to step 1)
    wrong code  → 422 field "code"; the challenge's tries go up; the 5th wrong one deletes it
    right code  → delete the challenge, clear its cookie, start the session (as login does),
                  auth.mfa.checked {ok: true, method}, auth.logged_in
                  → 200 {user, csrfToken, expiresAt, signedInAt}
```

`code` takes either a 6-digit authenticator code or a recovery code; the server tells them apart by shape. A refresh during step 2 starts over at step 1. The challenge lives only in its cookie, never in the URL or browser storage.

### Turning it off, and new recovery codes

Both need the current password **and** a code (authenticator or recovery), in one request:

- `DELETE /api/auth/mfa {password, code}`: deletes the secret and every recovery code, and records `auth.mfa.disabled` (`reason: "user"`). Admins can't turn it off (409); see [admin access](admin-access.md).
- `POST /api/auth/mfa/recovery-codes {password, code}`: replaces every recovery code with 10 new ones, records `auth.recovery_codes.created`, and returns them once.

### Lost the phone and the codes

There is no self-service way back in: that would be a way around MFA. In v1 the operator runs `admin reset-mfa -email ada@example.com -confirm` (`cmd/admin`, new). It deletes the user's secret, recovery codes, challenges and **every session** (admin sessions too). For an admin, it also reopens the door's 24-hour setup window, so they set up a new authenticator at their next admin sign-in. It records `auth.mfa.disabled` with `reason: "reset"` and no actor. The operator must check who is asking out of band first; the docs say so. Resetting from the admin dashboard is left out, because v1 of the dashboard is read-only (admin access, decision 4).

### What doesn't change

- **A password reset leaves MFA on.** Someone holding a reset link (or, later, the inbox) still needs the second factor.
- **A password change leaves MFA on.** It already signs out every other session.
- **Deleting a workspace or the account** keeps today's password check only (decision 7). The session has already passed MFA, and the password check stops someone holding a stolen session.
- **Registration** never asks for MFA. It's turned on afterwards, in settings.

## TOTP and recovery codes (`internal/auth/totp.go`, `internal/auth/recovery.go`)

| Setting | Value | Why |
| --- | --- | --- |
| Algorithm | HMAC-SHA1, 6 digits, 30-second steps | What every authenticator app supports. SHA-1's weaknesses don't affect HMAC |
| Secret | 20 random bytes (160 bits), base32 without padding | RFC 4226's recommended length |
| Clock drift | the current step ±1 | Phones drift. Wider windows only help an attacker |
| Replay | a code's step must be greater than `last_used_step` | Each code works once, even inside its 90-second window. Checked and saved under the row lock, so two requests with the same code can't both pass |
| Comparison | constant time | |
| `otpauth://` URI | issuer and label `APP_NAME`, account the email; `algorithm=SHA1&digits=6&period=30` | |

```go
// GenerateTOTPSecret returns 20 random bytes.
func GenerateTOTPSecret() ([]byte, error)
// TOTPCode is the code for secret at step (unix time / 30).
func TOTPCode(secret []byte, step int64) string
// CheckTOTP returns the step that code matches within ±1 of now, after last;
// ok is false when none does.
func CheckTOTP(secret []byte, code string, now time.Time, last int64) (step int64, ok bool)
// TOTPURI is the otpauth:// URI an authenticator app reads from the QR code.
func TOTPURI(issuer, account string, secret []byte) string
```

**Recovery codes:** 10 per user, each 10 characters from the 32-letter Crockford alphabet (no I, L, O or U), shown as `k7p2m-x9qfr` (50 bits). Input is lowercased, and hyphens and spaces are stripped. Each works once.

## Storing secrets (`internal/auth/encrypt.go`)

The server needs the TOTP secret itself to check codes, so it can't be hashed like a password. It is **encrypted with AES-256-GCM** under a new `APP_KEY`:

- **Why:** a leaked backup, replica or SQL injection would otherwise hand over every user's second factor.
- **Format:** `version byte (1) ‖ 12-byte random nonce ‖ ciphertext+tag`, stored as `bytea`. The version byte leaves room for rotating the key later (not included).
- **Bound to its row:** the user's internal ID is the GCM additional data, so a ciphertext copied onto another user's row fails to decrypt.
- **Recovery codes** are stored as HMAC-SHA256 digests, keyed with a subkey derived from `APP_KEY` (HKDF, label `recovery-codes`). A plain SHA-256 of a 50-bit code could be brute-forced from a database dump. With the key, guesses can't be checked offline.
- **Challenge tokens** are 256-bit, so a plain SHA-256 digest is enough, as for sessions.

### `APP_KEY` (`internal/config`)

| Variable | Value | Notes |
| --- | --- | --- |
| `APP_KEY` | 32 random bytes, base64 (`openssl rand -base64 32`) | Required everywhere (decision 3). `.env.example` and `.env.testing` carry throwaway keys, and production refuses those exact values. Error messages name the variable, never its value |

**Losing or changing the key locks every MFA user out**: their secrets can't be decrypted. [deployment.md](../../../docs/deployment.md) gets this as a warning next to the backup advice. Key rotation is not included.

> `internal/config/config.go` is being changed for [email delivery](email-delivery.md) right now. Build on top of that change; don't edit it in parallel.

## API

| Method | Route | Requires | Success | Failure |
| --- | --- | --- | --- | --- |
| POST | `/api/auth/login` | (unchanged) | as today, or `200 {mfaRequired: true}` with the challenge cookie and no session | as today |
| POST | `/api/auth/login/mfa` | Origin, JSON, challenge cookie, `auth:per-ip`, `mfa:per-account` | `200 {user, csrfToken, expiresAt, signedInAt}` and a session cookie | 401 no live challenge; 422 `field: "code"` |
| POST | `/api/auth/mfa/setup` | Session, CSRF, Origin, `limitAuth`, `auth:per-account` | `200 {secret, uri}` | 409 already on; 422 `field: "password"` |
| POST | `/api/auth/mfa` | Session, CSRF, Origin, `mfa:per-account` | `201 {recoveryCodes}`; other sessions signed out | 409 already on, or nothing pending; 422 `field: "code"` |
| DELETE | `/api/auth/mfa` | Session, CSRF, Origin, `limitAuth`, `auth:per-account`, `mfa:per-account` | 204 | 409 off, or an admin; 422 `field` (`password` or `code`) |
| POST | `/api/auth/mfa/recovery-codes` | Session, CSRF, Origin, `limitAuth`, `auth:per-account`, `mfa:per-account` | `201 {recoveryCodes}` | 409 off; 422 `field` (`password` or `code`) |

- **`GET /api/auth/me`** and every other response with the user gain `mfaEnabled` (boolean). `FindSession` reads it with an `EXISTS` on a confirmed `mfa_totp` row: no extra query.
- **Codes are checked after the password**, so an attacker without the password learns nothing about the code.
- **`DELETE /api/auth/mfa` and `…/recovery-codes`** use `requireSessionUnmetered`, like the other recovery routes. Someone holding a stolen session must not be able to use up the owner's budget and block them from regaining control.
- **Naming:** `/api/auth/*` holds named actions on the session ([naming.md](../../../docs/naming.md#controllers-internalhttpapi)). The MFA routes treat `mfa` as a singleton resource of the account (`store`, `destroy`), and `recovery-codes` as its sub-collection; `setup` and `login/mfa` are named verbs.

## Data

Three new create migrations, after `010` (squashed):

```sql
-- 011_create_mfa_totp: one authenticator per user. confirmed_at null = setup pending.
CREATE TABLE mfa_totp (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 secret bytea NOT NULL,            -- AES-256-GCM, never the plain secret
 last_used_step bigint NOT NULL DEFAULT 0,
 confirmed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);

-- 012_create_mfa_recovery_codes
CREATE TABLE mfa_recovery_codes (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 code_hash text NOT NULL,          -- HMAC-SHA256, keyed from APP_KEY
 used_at timestamptz,
 UNIQUE (user_id, code_hash)
);

-- 013_create_mfa_challenges: a password-checked sign-in waiting for its code.
CREATE TABLE mfa_challenges (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 purpose text NOT NULL CHECK (purpose IN ('login', 'admin', 'admin_setup')),
 attempts int NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX mfa_challenges_user_id_idx ON mfa_challenges(user_id);
```

- **`purpose`** stops a challenge made for one flow from completing another. `login` is the app's sign-in. `admin` and `admin_setup` come from [admin access](admin-access.md): an admin sign-in waiting for a code, and an admin without MFA allowed to set it up at the door. Only `admin_setup` can start or confirm a setup, and only `login` or `admin` can be answered with an existing code.
- **Store** (`internal/store/mfa.go`): `SetupMFA`, `EnableMFA`, `DisableMFA`, `ResetMFA`, `CreateRecoveryCodes`, `CreateMFAChallenge`, `CheckMFAChallenge` (the domain verbs of [naming.md](../../../docs/naming.md#store-methods-internalstore)). Every check that consumes something (a step, a recovery code, a challenge try) runs in one transaction that locks the user's `mfa_totp` row first, then the challenge.
- **Cleanup:** expired challenges and pending secrets older than 15 minutes, in `Store.Cleanup`.
- **Factories** for all three tables (`TestEveryTableHasAFactory`). The `mfa_totp` factory encrypts a known test secret with `.env.testing`'s key, so tests can make valid codes with `auth.TOTPCode`.
- **Seed:** the demo user has MFA off. A seeder option for MFA isn't needed.

## Events

Named under `auth`, with `mfa` and `recovery_codes` as stored sub-resources ([naming.md](../../../docs/naming.md#events-internalevents)):

| Event | Payload | Recorded by |
| --- | --- | --- |
| `auth.login_attempted` | gains result `mfa_required` (right password, code still needed) | `login` |
| `auth.mfa.enabled` | `user`, `otherSessionsRevoked` | `EnableMFA` |
| `auth.mfa.disabled` | `user`, `reason`: `user` / `reset` | `DisableMFA`, `ResetMFA` (CLI, no actor) |
| `auth.mfa.checked` | `user`, `ok`, `method`: `totp` / `recovery_code`, `step`: `login` / `admin_login` / `enable` / `disable` / `recovery_codes`, `ip`, and `recoveryCodesLeft` when a recovery code was used | every code check |
| `auth.recovery_codes.created` | `user`, `count` | `CreateRecoveryCodes`, for a new set. The first set is part of `auth.mfa.enabled` |
| `auth.password_checked` | `step` gains `mfa_setup`, `mfa_disable`, `recovery_codes` | as today |

Never in a payload: secrets, codes, recovery codes, their digests, challenge tokens. `TestPayloadsHoldNothingSensitive` covers the new structs. The admin dashboard's monitoring will read `auth.mfa.checked` with `ok: false` as failed second factors.

## Rate limits

| Name | Limit | Counted by | Where |
| --- | --- | --- | --- |
| `mfa:per-account` | 5 / 15 min | user (`usr_` ID) | every code check: `login/mfa`, enabling, disabling, new recovery codes, and the admin sign-in's code step |
| `auth:per-ip` | (existing) | IP | `login/mfa`, before the code is checked |
| per challenge | 5 tries | challenge | the 5th wrong code deletes the challenge: the password has to be entered again |

**The arithmetic:** with ±1 step, each guess covers 3 of a million codes. At 5 guesses per 15 minutes, an attacker who already has the password reaches about a 0.14% chance per day, and every miss is an `auth.mfa.checked` event with `ok: false` for monitoring to flag. Per-account counting allows lockout: someone with the password can keep the owner from signing in for 15 minutes at a time. The owner should change that password anyway. A recovery code check counts against the same limit.

`TestRateLimitNames` covers the new name. [rate-limits.md](../../../docs/api/rate-limits.md) gets the row.

## Web app

- **Login** (`pages/auth/login.tsx`): `{mfaRequired: true}` switches the page, in local state, to step 2:
  - an "Authentication code" field (`inputMode="numeric"`, `autoComplete="one-time-code"`, autofocus) with a full-width **Verify** button (sign-in page rules, [forms-and-actions.md](../../../docs/ui/forms-and-actions.md));
  - a text link "Use a recovery code instead", which switches the label and input mode;
  - "Sign in again" back to step 1.
  
  A 401 from `login/mfa` goes back to step 1 with "Your sign-in expired. Enter your password again." Sign-in's `?next=` is kept through both steps.
- **Account settings** (`pages/app/settings.tsx`): a new `SettingsSection` "Two-factor authentication" between Password and Sign-in sessions, with the description "Ask for a code from an authenticator app when you sign in."
  - **Off:** "Off", and one outline button, **Enable**, opening a `Dialog`. Its first step is the password; the rest is `MfaSetup`.
  - **On:** "On since <date>", then two outline buttons, **New recovery codes** and **Disable**. Each opens a dialog that asks for the password and a code. New codes are shown through the same hidden-until-revealed list.
- **`MfaSetup`** (`components/app/mfa/mfa-setup.tsx`, shared by settings and the admin door; see [One setup, two entry points](#one-setup-two-entry-points)):
  1. **Scan:** the QR code, hidden (below), with **Reveal QR code**. Under it, "Can't scan? Show setup key" reveals the secret as text, in groups of four, with Copy. Then the "Code from your app" field and **Verify**.
  2. **Recovery codes:** hidden the same way, behind **Reveal recovery codes**. Then Copy, **Download** (saves `lewcontext-recovery-codes.txt` from a `Blob`), and "I've saved these codes", which must be ticked before **Done**. The step can't be closed or skipped before that.
- **Hidden until revealed** (`Reveal`, `components/ui/reveal.tsx`, a new generic primitive). People set this up while screen sharing, recording, or on a desktop someone else can see. So nothing secret is on screen until they ask:
  - **Before reveal, the real QR code isn't rendered at all.** A fixed decoy pattern sits blurred in its place (`blur-md`, `select-none`, `aria-hidden`), with the button centred over it. Blurring the real code could still leave it readable in a screenshot or a zoomed recording, and the secret would already be in the DOM. The setup key and recovery codes are likewise not in the DOM until revealed.
  - **Reveal** shows the content; **Hide** puts the decoy back. Each `MfaSetup` step starts hidden, including after Back.
  - **Accessibility:** the button is a real `<button>` with an accessible name ("Reveal QR code"), focus moves to the revealed content, and a polite live region says "QR code shown." Reduced motion turns off the blur transition. Screen readers never get the hidden content, only the button.
  - It hides from casual viewers; it can't stop software already controlling the desktop, which can press the button itself.
- **The QR code** is drawn in the browser as an SVG `<path>` from the `otpauth://` URI with a small QR encoder (decision 5). No `innerHTML`, no image URL and no CSP change.
- **API client** (`lib/api/auth.ts`): `api.auth().loginMfa(code)`, `api.auth().mfa().setup(password)`, `.store(code)`, `.destroy({password, code})`, `api.auth().mfa().recoveryCodes().store({password, code})`. `User` gains `mfaEnabled`.
- **Secrets in the browser:** the secret, URI and recovery codes live only in the dialog's state. They are cleared when it closes, and never cached by SWR or written to storage.

## Security risks and how they're avoided

| Risk | Mitigation |
| --- | --- |
| Guessing codes | `mfa:per-account`, 5 tries per challenge, `auth:per-ip`, and the password must come first |
| Replaying a seen code (shoulder surfing, phishing relay after the fact) | `last_used_step` under the row lock: each step works once |
| Real-time phishing (a fake site relaying the password and code at once) | **Not stopped by TOTP.** That's what passkeys add later. `SameSite=Strict` and the exact Origin check stop a cross-site relay through the user's own browser |
| Database leak exposes secrets | AES-256-GCM under `APP_KEY`, bound to the user ID. Recovery codes HMAC'd with a key-derived subkey |
| `APP_KEY` leaks together with the database | Everything protected by it is exposed. Keep the key in the platform's secret store, never in the image; rotation is future work |
| Challenge stolen from the browser | HttpOnly, SameSite=Strict, `__Host-`, 5 minutes, single use, and useless without a code |
| Bypassing MFA through password reset | Reset never touches MFA, and signing in after a reset still asks for the code |
| A stolen session turns MFA off or reads new recovery codes | Both need the password **and** a current code. Turning MFA on signs out every other session |
| A stolen session turns MFA on, locking the owner out | Turning it on needs the password, which the session alone doesn't have |
| Support resets used for social engineering | Only the CLI can reset, so access to the server and database is needed. The docs require checking identity out of band. Recorded as `auth.mfa.disabled` `reason: "reset"` |
| Learning that the password was right | `{mfaRequired}` shows that, as every TOTP sign-in does. Password guesses keep `auth:per-account`, and the code is still needed |
| Secrets cached in the browser | The API sends `no-store`, and the web app keeps them only in dialog state |
| The QR code, setup key or recovery codes seen over a shared screen, a recording or someone's shoulder | Not rendered until **Reveal**: a blurred decoy stands in. Each step starts hidden again |
| Setting up MFA at the admin door with only a stolen password | The setup stage opens only inside the 24-hour window after `admin grant` or an operator's MFA reset ([admin access](admin-access.md#admin-sign-in)). Otherwise it's the ordinary refusal |
| An admin door challenge used to set up MFA through `/api/auth/mfa*`, or a session through the door | Separate entry points: the account routes need an app session, and the door routes need `X-Admin-Key` and an `admin_setup` challenge |

## Tests

**Go unit:**
- TOTP against the RFC 6238 SHA-1 vectors (times 59, 1111111109, 1111111111, 1234567890, 2000000000, 20000000000), accepting ±1 step and refusing ±2, refusing a step at or before `last`, and refusing wrong lengths and non-digits.
- The URI format, including escaping an email with `+`.
- Encryption: round trip, a tampered ciphertext, the wrong additional data, the wrong key and an unknown version byte all fail.
- Recovery codes: format, normalisation (case, hyphens, spaces), and an HMAC that changes with the key.
- Config: `APP_KEY` missing, wrong length, not base64, and the committed test keys in production are all refused, and no message contains the value.

**Go integration** (`mfa_integration_test.go`):
- **Setup:** needs the password, 409 when already on, and a second setup replaces a pending secret.
- **Enabling:** a wrong code is 422 and leaves it off; a right code turns it on, returns 10 codes and signs out the other sessions, but not this one.
- **Login with MFA:**
  - `{mfaRequired}`, no session cookie, and `/api/auth/me` with the challenge cookie alone is 401;
  - the right code starts a session and clears the challenge;
  - the same code again (new challenge) is refused (replay);
  - 5 wrong codes delete the challenge;
  - an expired challenge is refused;
  - a challenge with purpose `admin` can't complete a normal sign-in.
- **Recovery codes:** one works once, and `recoveryCodesLeft` counts down; new codes make the old ones fail.
- **Turning it off:** needs both factors (each wrong one is 422 with its `field`), and is 409 for an admin.
- **Recovery and accounts:** a password reset and a password change leave MFA on. Deleting the account deletes all three tables' rows (cascade). The CLI reset deletes sessions and records `reason: "reset"`.
- **Events and limits:** one event per check with the right fields. `mfa:per-account` exhausted gives 429 with `rate_limit.reached`. Responses never contain the secret except `setup`'s.

**Vitest:**
- The login page's second step: a code, switching to a recovery code, and expiry going back to step 1.
- The settings section in both states.
- `MfaSetup` with fake `start` and `confirm`:
  - nothing secret is in the DOM before reveal (no `otpauth://`, no secret, no recovery code);
  - reveal shows it, and hide removes it again;
  - Back resets to hidden;
  - Done stays disabled until the codes are acknowledged.
- `Reveal`: the accessible name, focus moving to the content, and the live-region announcement.

**Playwright** (`e2e/mfa.spec.ts`): register, then enable MFA:
- check that the QR code is hidden;
- reveal the setup key (the test computes codes with Node's `crypto` HMAC from it, with no extra dependency);
- then sign out, sign in with a code, sign out, sign in with a recovery code, and disable MFA.

## Docs

- [auth.md](../../../docs/api/auth.md): a "Two-factor authentication" section; take MFA out of "Not implemented".
- [endpoints.md](../../../docs/api/endpoints.md): the routes, plus `mfaEnabled` on the user.
- [events.md](../../../docs/api/events.md): the catalogue rows.
- [rate-limits.md](../../../docs/api/rate-limits.md): `mfa:per-account`, and the unmetered routes.
- [database.md](../../../docs/api/database.md): the three tables, and the migration list.
- [api/README.md](../../../docs/api/README.md): `APP_KEY`.
- [deployment.md](../../../docs/deployment.md): `APP_KEY` in configuration and the security checklist, the warning about losing it, and `admin reset-mfa`.
- [ui/README.md](../../../docs/ui/README.md): the QR dependency.
- **AGENTS.md:** the line "Do not claim … MFA … exists" becomes "TOTP MFA exists; passkeys and OIDC don't".

## Decisions

1. **Method: TOTP plus recovery codes.** **Confirmed 2026-10-31.** Passkeys later.
2. **Required for admins**, optional for everyone else. **Confirmed 2026-10-31** (with the admin plan; how admins set it up is decision 10).
3. **`APP_KEY` required everywhere**, not only in production. *Recommended:* one mode, no "MFA unavailable" path to build and test. The template files carry throwaway keys. The alternative is to make it optional in development and answer 503 to MFA setup without it.
4. **Turning MFA on signs out every other session.** **Confirmed 2026-10-31.** *Recommended:* those sessions never passed a second factor.
5. **QR code drawn in the browser** with a small, dependency-free encoder (`qrcode-generator` 2.x: MIT, no dependencies, maintained since 2009), rendered as an SVG `<path>`. *Recommended* over a Go library returning SVG markup, which the web app would have to inject as HTML, and over a PNG data URL, which needs `img-src data:` in the CSP.
6. **`mfa:per-account` at 5 per 15 minutes.** **Confirmed 2026-10-31.** *Recommended:* the arithmetic above. 10 would double an attacker's odds for little benefit to a real user.
7. **Workspace and account deletion stay password-only.** **Confirmed 2026-10-31.** *Recommended* for v1. Asking for a code there too ("step-up") can follow if wanted.
8. **No "remember this device".** **Confirmed 2026-10-31.** *Recommended:* sessions already last up to 24 hours, so a code is needed at most once a day per browser.
9. **`cmd/admin` as the operator CLI** (`reset-mfa` here; `grant`, `revoke`, `list` and `key` in the admin plan), shipped as a fifth binary in the image. *Recommended:* AGENTS.md rule 5 allows `cmd` entrypoints for operations like migrate, seed and prune. This adds one more, which the plan asks you to confirm.
10. **Optional for users with an Enable button; required for admins, who set it up at their first admin sign-in if they haven't yet; the QR code hidden until revealed; one shared setup component.** **Confirmed 2026-10-31.**
    - *Recommended within that:* the hidden QR code is a decoy, not a blurred copy of the real one, and the setup key and recovery codes are hidden the same way.
11. **Shared setup code behind two thin entry points** (account routes and admin door routes), not one route that accepts either a session or a door challenge. *Recommended:* see [One setup, two entry points](#one-setup-two-entry-points).

## Not included

- Passkeys / WebAuthn, SMS, codes by email.
- Requiring MFA for a workspace's members (a workspace setting), and step-up codes for other actions.
- Resetting a user's MFA from the admin dashboard (v1 is read-only).
- Rotating `APP_KEY`, and trusted devices.
- Telling the user by email when MFA is turned off or a recovery code is used (needs [email delivery](email-delivery.md); a natural listener on `auth.mfa.disabled` afterwards).

## How the build differs

- **`mfaEnabledAt`** (when it was turned on, or null) instead of a boolean `mfaEnabled`, so settings can say "On since …". It's in every response with the signed-in user, read with the session in one query.
- **`.env.testing` holds no key.** `TestEnvTestingHoldsNoSecrets` refuses a value for any `*_KEY`, so the e2e server (`scripts/serve-test.sh`) makes a fresh `APP_KEY` at every start (its database is rebuilt anyway), and Go tests use `factory.AppKey`, random per process. `.env.example` and the Docker Compose app carry the throwaway key production refuses. `migrate`, `seed`, `prune` and `admin` load the same config, so they need the key too.
- **`APP_KEY`, not `ENCRYPTION_KEY`** (renamed at the user's request, after the Laravel convention). `make app-key` (`admin app-key generate -env ../../.env`) writes one into `.env` only when it has none, and never replaces one; without `-env` it prints one, and in production it never writes. A server still given `ENCRYPTION_KEY` refuses to start with a message to rename it.
- **Two derived keys.** HKDF derives the AES-256-GCM key and the recovery-code HMAC key from `APP_KEY` (labels `lewcontext seal v1` and `lewcontext recovery-codes v1`), rather than using the raw key for GCM.
- **No checkbox for saving recovery codes.** There is no checkbox component, so the final button itself says "I’ve saved these codes", and it stays disabled until the codes have been revealed, copied or downloaded. Copy and Download work without revealing them.
- **Disabling** is a `ConfirmationDialog` with the password and code fields. Enabling and new recovery codes are step dialogs keyed per opening, which refuse to close on the recovery codes step until it's done.
- **The login code step** (`CodeStep`) lives in `pages/auth/login.tsx`, since only login uses it; `CodeField` is shared from `components/app/mfa`. The admin door will move what it needs. A malformed code counts as a wrong try, like any other.
- **Store methods:** `SetupMFA`, `EnableMFA`, `DisableMFA`, `CreateRecoveryCodes`, `ResetMFA`, `CreateMFAChallenge`, `FindMFAChallenge` (whose challenge it is, for the per-account limit before the check) and `AnswerMFAChallenge` (instead of one `CheckMFAChallenge`).
- **Settings, off:** only the **Enable** button, with no status row; on, "Authenticator app · On since …" with an On badge, then New recovery codes and Disable.
- **The setup key** shows in a read-only field behind a "Use setup key instead" link, in a read-only field without a visible label that starts masked, with Copy (works masked) and an eye to show or mask it inside the field (`InputActions`), and a "Hide setup key" link under it.
- **New primitives:** `Reveal` (`components/ui/reveal.tsx`) and `TextButton` (`text.tsx`).
- **CLI:** `make admin cmd=reset-mfa email=… CONFIRM=1` (`cmd/admin`), previewing without `CONFIRM`.
- **Not built yet, from the admin plan:** refusing to turn MFA off for an admin (409) waits for the `admins` table; the `admin` and `admin_setup` challenge purposes exist in the CHECK but nothing creates them.
