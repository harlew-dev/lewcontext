# Email verification

Status: **implemented 2026-10-31** with the confirmed decisions below; see [How the build differs](#how-the-build-differs). Docs updated: [auth.md](../../../docs/api/auth.md), [endpoints.md](../../../docs/api/endpoints.md), [rate-limits.md](../../../docs/api/rate-limits.md), [events.md](../../../docs/api/events.md), [mail.md](../../../docs/api/mail.md) (including the listener-side limit, an exception to its rule), [database.md](../../../docs/api/database.md), [seeding.md](../../../docs/api/seeding.md), [api/testing.md](../../../docs/api/testing.md), [api/README.md](../../../docs/api/README.md) (environment), [admin.md](../../../docs/api/admin.md), [deployment.md](../../../docs/deployment.md) (turning it on for an existing install; `APP_KEY` now also keys email codes, so every server on one database needs the same one), [testing.md](../../../docs/testing.md), [naming.md](../../../docs/naming.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/api.md](../../../docs/ui/api.md), [ui/forms-and-actions.md](../../../docs/ui/forms-and-actions.md) (the email field's action), [ui/testing.md](../../../docs/ui/testing.md) (the settings spec, the second port, `E2E_SHARED_DATABASE`, `E2E_APP_KEY`), and AGENTS.md (the "Do not claim email verification" line, rule 4's page list). Builds on [forgot password](forgot-password.md) (request → event → listener → mail) and [MFA](mfa.md) (a challenge cookie, `CodeField`, HMAC digests under `APP_KEY`).

## Why

Anyone can sign up with an address they don't own. That has two costs today:

1. **Nothing proves the account holder owns the inbox.** Reset links and invitations go there, and an admin reading the Users list can't tell a real address from a typo or someone else's.
2. **Sign-up shows which emails have accounts.** Register answers 409 "Unable to create this account. Try signing in or use a different email." for a taken address, so anyone can test an address. Login and password reset already give the same answer for every email; sign-up is the one hole.

## Settings

| Variable | Values | Default | Does |
| --- | --- | --- | --- |
| `EMAIL_VERIFICATION` | `on`, `off` | `on` | `on`: new sign-ups must enter a code before an account exists, and unverified accounts can verify from Account settings. `off`: sign-up as today (409 included), and nothing about verification shows |
| `EMAIL_VERIFICATION_EXISTING` | `optional`, `required` | `optional` | Only with `EMAIL_VERIFICATION=on` (startup refuses `required` with `off`). `required`: an account that was never verified must verify before it can do anything after signing in |

- `EMAIL_VERIFICATION=on` with `MAIL_DRIVER=off` refuses to start: "set EMAIL_VERIFICATION=off to run without email".
- **Turning it on for an existing install** (deployment.md): turn on `EMAIL_VERIFICATION` first. New sign-ups are verified from then on, and existing users see "Not verified" and can verify when they like. Switch to `EXISTING=required` later, once most have. Users then verify at their next sign-in, which spreads naturally over hours or days rather than all at once, and `email-verification:global` (below) caps the mail if it doesn't.

## New sign-ups

```
/auth/register   name, email, password  [Create account]
  POST /api/auth/register {name, email, password}
    validate (unchanged), limits (register:per-email counts every address alike)
    hash the password (always)
    insert a pending_registrations row {email, name, password_hash, token digest, expires +15 min}
    record auth.registration.requested {email, ip}       ← no user lookup here
    set cookie lewcontext_registration (256-bit token, HttpOnly, SameSite=Strict, 15 min)
    → 202 {email}                                        (identical for every address)
  → /auth/register/verify

worker → listener mail.registration (≈1 s later, after the response)
    request older than 15 min?  → drop
    an account uses the email?
      no  → a 6-digit code; its HMAC on the row
            mail.VerifyEmail     "Confirm your email address"   {Code}
      yes → the HMAC of a random code nobody is sent, so the row looks like any other
            account-exists:per-account (5 a day) allows it?
              yes → mail.AccountExists  "You already have an account"  {Name (the account's), LoginURL, ResetURL}
              no  → send nothing (rate_limit.reached, the first time in the window)
    record auth.registration.sent {email, request, existingAccount, user?}   (when sent)

/auth/register/verify     (its own page, so a reload or an app switch on a phone resumes it)
  POST /api/auth/register/check          (cookie) → {email, expiresAt} | 401 → back to /auth/register
  "Enter the 6-digit code we sent to ada@example.com."
  [Code]  [Verify]   · Send a new code · Use a different email · Already have an account? Sign in
  POST /api/auth/register/verify {code}  (the cookie says which registration)
    wrong                                 → 422 field "code"  (attempts + 1)
    5th wrong, expired, no cookie         → 401 "This code has expired. Start again."
    right, still no account
      one transaction: create the user (email_verified_at = now), delete EVERY pending
      registration for that email, record auth.registered {…, verifiedBy: "code"}
      start the session → 201 → onboarding (or back to the invitation link in ?next=)
    right, but an account appeared since the code was sent
      → delete the row, 409 "You already have an account. Sign in instead."
```

- **Send a new code** is `POST /api/auth/register/resend` (cookie): a new code for the same row, a new 15 minutes, `attempts` back to 0, and a new `auth.registration.requested` through the same listener and limits. For a taken address it's another "You already have an account", within the hidden cap.
- **Use a different email** goes back to the form; the old row expires on its own.
- The 409 can only be reached with a real code, so only the inbox's owner sees it.
- **Invited people** sign up the same way: the code, then back to the invitation link, which joins as today. A link doesn't verify the address ([rejected below](#invitation-links-verifying-the-address)).

### "You already have an account"

> **You already have an account**
>
> Someone, hopefully you, tried to create a lewcontext account with this email, but you already have one.
>
> **[Sign in]** · Forgot your password? **[Reset it]**
>
> If this wasn't you, you can ignore this email. Nothing about your account has changed. We recommend turning on two-factor authentication in Account settings if you haven't already.

- **Sign in** opens `/auth/login?notice=account-exists`: "You already have an account. Sign in here, or [reset your password]." The notice is fixed text picked by the parameter; nothing from the URL is shown, and the URL carries no email.
- **Reset it** opens `/auth/forgot-password`.
- No code and nothing that changes the account, so it's harmless if someone else caused it. Both links work in any browser: nothing here needs the sign-up's cookie.

### Phones

- **The code is typed into the page that asked for it**, so it works wherever the email is read: sign up on a laptop and read the email on a phone, or do both on a phone and copy the code across. A link couldn't do this: it would open in the mail app's own browser, which doesn't have the sign-up's cookie.
- The code field is `inputMode="numeric"` and `autoComplete="one-time-code"`, so iOS and Android offer the code from Mail or Messages above the keyboard.
- Phones often reload a background tab when you switch apps. The code step is its own page that rebuilds itself from the cookie (`register/check`), so coming back from the mail app lands on the same step.
- Finishing in a *different browser* from the one that started doesn't work, deliberately: see the two-sign-ups race in [Security risks](#security-risks-and-how-theyre-avoided).

## Existing accounts

Every account made while `EMAIL_VERIFICATION` was off, or before this ships, has `email_verified_at` null. With verification on:

### Account settings (both modes)

The Email address field's label gets a badge: **Verified** (`green`) or **Not verified** (`amber`). When not verified, the field's description reads "We haven't confirmed this address. [Verify email]", where Verify email is a `TextButton` that opens a dialog:

```
Dialog "Verify your email"
  step 1: "We'll send a 6-digit code to ada@example.com."   Cancel · [Send code]
    POST /api/auth/email-verification              → 202
  step 2: [Code]                                      Send a new code · Cancel · [Verify]
    PATCH /api/auth/email-verification {code}
      wrong → 422 field "code"; 5th wrong or expired → 410 "This code has expired. Send a new one."
      right → email_verified_at = now, toast "Email verified." → the badge says Verified
```

Neither the badge nor the action shows with `EMAIL_VERIFICATION=off`. forms-and-actions.md has no pattern for an action inside a field's description; this adds one (decision 13).

### At sign-in (`EMAIL_VERIFICATION_EXISTING=required`)

The user signs in as now (password, then the second factor if MFA is on), so the session exists, but:

- **The API refuses everything else** until the email is verified: `requireSession` answers 403 `{error: "Verify your email to continue.", verificationRequired: true}` for an unverified user. Only `GET /api/auth/me`, `POST /api/auth/logout`, `DELETE /api/auth/sessions` and the two `email-verification` routes accept such a session, through `requireSessionUnverified`, a variant like `requireSessionUnmetered`. The check is in the API, so no route added later can forget it.
- **The web app** sends them to `/app/verify-email` (a standalone signed-in page, like onboarding), from the app layout's loader, `/app`'s loader, onboarding's loader, and the API client on a 403 with `verificationRequired`:

```
/app/verify-email      AuthLayout
  "Verify your email"
  "You must verify your email to continue. We'll send a 6-digit code to ada@example.com."
  [Send code]    (full width; a sign-in page)        · Sign out
  → [Code] [Verify]  · Send a new code
  right → back to ?next= or /app
```

- It uses the same two endpoints as Account settings. A verified user who opens the page goes on to `/app`.
- Mail can't be sent while someone signs in: the code goes out only when they press **Send code**, so turning the flag on never emails anyone by itself.

### Signed-in verification, in detail

- **`POST /api/auth/email-verification`**: already verified → 409. Otherwise it records `auth.email_verification.requested {user}` and answers 202; the `mail.email_verification` listener makes a code, stores its HMAC in `email_verifications` (one row per user, so a new code replaces the old one) and sends the same `mail.VerifyEmail` as sign-up. Limits: the signed-in budget, `email-verification:per-email` (3 an hour), `email-verification:global` (500 an hour, 503 "Email verification is busy. Try again in a few minutes.").
- **`PATCH /api/auth/email-verification {code}`**: one transaction locks the user, then the row, compares, counts the attempt; the right code sets `email_verified_at`, deletes the row and records `auth.email_verified {user, via: "code"}`. Limits: `email-code:per-email`, shared with sign-up.
- **A completed password reset also verifies the email** (decision 11): the link was 256 bits sent only to that inbox, and whoever completes it controls the account anyway, so it proves the same thing. `ResetPassword` sets `email_verified_at` if null and records `auth.email_verified {via: "password_reset"}`.

## Timing: no difference to measure

For sign-up to hide which emails have accounts, a taken address and a new one must cost the same in the request, not just answer the same. "Constant time" here means two things:

1. **The same work for every address.** `register`, `register/resend` and `register/check` never read `users`. Each hashes the password (where it takes one), inserts or updates the same row shape, records the same event and writes the same response bytes and cookie. The only lookup happens in the listener, after the response has gone, where no one can time it. The hidden `account-exists:per-account` cap is in the listener for the same reason.
2. **The same comparison for every row.** A taken address's row holds the HMAC of a random code nobody was sent, never null, so `register/verify` does exactly what it does for a new address: lock the row, compute the HMAC of the typed code, compare it with `subtle.ConstantTimeCompare`, update `attempts`. Only a *right* code takes a different path, and only the inbox's owner has one.

**Tested structurally, not with a stopwatch.** Timing tests are flaky. Instead, a Bun query hook in the integration tests records the tables each request touches and asserts that `register`, `resend`, `check` and a wrong `verify` touch the same tables for a new and a taken address, and never `users`. Login already compares against a dummy hash for unknown emails.

The signed-in routes don't need this: they act on the session's own email, which the user already knows.

## API

| Endpoint | Change |
| --- | --- |
| `POST /api/auth/register` | With verification on: 202 `{email}` plus the registration cookie. With it off: unchanged (201 or 409) |
| `POST /api/auth/register/check` (new) | Cookie only. 200 `{email, expiresAt}` for a live registration, 401 otherwise. `auth:per-ip` |
| `POST /api/auth/register/resend` (new) | Cookie only. 202 `{email}`. `auth:per-ip`, `register:per-email`, `register:global` |
| `POST /api/auth/register/verify` (new) | `{code}`: 6 digits, spaces allowed. 201 with the session, 422 `field: "code"`, 401 when ended, 409 when an account appeared meanwhile. Clears the cookie on every outcome except 422. `auth:per-ip`, `email-code:per-email` |
| `POST /api/auth/email-verification` (new) | Signed in (`requireSessionUnverified`). 202, or 409 when already verified |
| `PATCH /api/auth/email-verification` (new) | Signed in (`requireSessionUnverified`). `{code}`: 200 with the user, 422 `field: "code"`, 410 when the code has ended or none was sent |
| every other signed-in route | 403 `verificationRequired` for an unverified user while `EXISTING=required` |
| `GET /api/auth/me` and login | the user gains `emailVerifiedAt`; the session gains `emailVerification`: `off`, `optional` or `required` |

All five new routes are registered only with `EMAIL_VERIFICATION=on`; otherwise they're unknown routes, and pending rows just expire. Cookie: `lewcontext_registration` locally, `__Host-lewcontext_registration` in production; HttpOnly, SameSite=Strict, Path=/, Secure in production, 15 minutes. The web app only sends it on its own same-origin API calls, so Strict costs nothing.

The admin door isn't gated: admins are granted from the command line and must have MFA ([admin.md](../../../docs/api/admin.md)). Their app session is gated like anyone's.

## Data

Migrations are squashed in development: `002_create_users` gains the column, and two create migrations add the tables; then `make migrate-fresh CONFIRM=1 && make seed`.

```sql
-- 002_create_users: add
 email_verified_at timestamptz

-- 016_create_pending_registrations
CREATE TABLE pending_registrations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 email text NOT NULL,
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
 password_hash text NOT NULL,
 token_hash text NOT NULL UNIQUE,      -- SHA-256 of the cookie token
 request_id text NOT NULL UNIQUE,      -- evt_ ID of the latest auth.registration.requested, so the listener finds the row
 code_hash text,                       -- HMAC-SHA256 under a key from APP_KEY; null only until the listener runs
 attempts smallint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX pending_registrations_email_idx ON pending_registrations(email);
CREATE INDEX pending_registrations_expires_at_idx ON pending_registrations(expires_at);

-- 017_create_email_verifications
CREATE TABLE email_verifications (
 user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 code_hash text NOT NULL,
 attempts smallint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL
);
CREATE INDEX email_verifications_expires_at_idx ON email_verifications(expires_at);
```

- **With verification on, the `users` table only gains verified accounts.** Sign-ups wait in `pending_registrations` until the code is right; completing one creates the user and deletes every pending row for that email in the same transaction. `Store.Cleanup` deletes expired rows of both tables every 15 minutes.
- **`email_verified_at`** is null for accounts made while verification was off or before this ships, which is what Account settings, the sign-in gate and the admin badge read.
- **Each sign-up is its own row**, tied to the browser that made it, not one row per email. `register:per-email` bounds how many exist.
- **Codes are HMACs, not plain hashes:** with a million possible codes, a plain SHA-256 is reversed instantly from a database dump. The key comes from `APP_KEY` by HKDF, like the recovery-code key (`auth.Keys`).
- Store methods: `CreatePendingRegistration`, `ResendPendingRegistration`, `CheckPendingRegistration`, `IssueRegistrationCode` (the listener), `CompleteRegistration`, `IssueEmailVerification`, `VerifyEmail`. Factories: `factory.PendingRegistrations`, `factory.EmailVerifications`; `factory.Users` sets `email_verified_at` by default, with an `Unverified` state. The demo seed's users and seeded fake members are verified; `make seed only=users` makes a few unverified ones so the badge and gate can be tried.

## Events

| Event | Payload | Recorded by |
| --- | --- | --- |
| `auth.registration.requested` (new) | `email`, `ip` (no `user`: no lookup) | `register` and `register/resend`, every request past the limits |
| `auth.registration.sent` (new) | `email`, `request` (evt_), `existingAccount`, `user` when it exists | the `mail.registration` listener, when it sent something |
| `auth.registration.checked` (new) | `email`, `ok`, `ip`, `attempt` | every sign-up code check that reached the code, like `auth.mfa.checked` |
| `auth.registered` | gains `verifiedBy`: `code`, or `none` with verification off | `CompleteRegistration`, `CreateUser` |
| `auth.email_verification.requested` (new) | `user` | `POST /api/auth/email-verification` |
| `auth.email_verification.sent` (new) | `user`, `request` | the `mail.email_verification` listener |
| `auth.email_verified` (new) | `user`, `via`: `code` / `password_reset` | `VerifyEmail`, `ResetPassword` |

Neither a code, the cookie token nor the password hash is ever in an event, the log or a URL. Admins can see `existingAccount`; they can already see every account, so it tells them nothing new.

## Mail

| Template | Constant | Data | Subject | Sent for |
| --- | --- | --- | --- | --- |
| `verify-email` | `mail.VerifyEmail` | `VerifyEmailData{Code}` | Confirm your email address | a new sign-up; an existing account's code |
| `account-exists` | `mail.AccountExists` | `AccountExistsData{Name, LoginURL, ResetURL}` | You already have an account | a sign-up for a taken address |

- **`verify-email` has no name in it.** Sign-up lets anyone send it to any address, and the name is whatever they typed, such as "Claim your prize at evil.example", in an email from our domain. Leaving the name out means a stranger controls none of its words. It reads: "Your code is 482 913. Enter it where you asked for it to confirm this address. It works for 15 minutes. Never share it. If you didn't ask for this, ignore this email."
- **`account-exists` greets the account's stored name**, not the one just typed.
- The code is shown spaced (`482 913`), never as a link. Subjects are fixed; the code never goes in a header.

### A limit in a listener (an exception to mail.md)

mail.md's rule is that limits go on the request, never on sending: a listener must not hold back mail the requester was told is coming, and must never be a budget an attacker can spend to suppress security mail. `account-exists:per-account` breaks it on purpose, and mail.md records why:

- **A limit on the request would give the account away.** If only taken addresses hit it, its 429 says "this address has an account". It has to be invisible to the requester, so it can only live where the account is looked up: the listener.
- **Nothing important is held back.** The email is informational. The person who caused it was never promised it, and the owner loses only a sixth copy of the same notice in a day. Security mail and codes are never capped in a listener.
- It drops rather than delays, and records `rate_limit.reached` the first time in each window, so admins can see someone hammering an address.

## Rate limits

| Name | Limit | Counted by | Where | Refusal |
| --- | --- | --- | --- | --- |
| `register:per-email` (new) | 3 / hour | email typed | register and resend, once the body is valid, every address alike, account or not | 429 "This email address has had too many sign-up attempts recently. Try again in …" |
| `account-exists:per-account` (new) | 5 / day | the account (`usr_` ID) | **the `mail.registration` listener**, before sending "You already have an account" | none: nothing is sent, and the requester can't tell |
| `email-code:per-email` (new) | 10 / day | the email | every code check, sign-up and signed-in, from any browser or IP | 429 |
| `email-verification:per-email` (new) | 3 / hour | the session user's email | `POST /api/auth/email-verification` | 429 |
| `email-verification:global` (new) | 500 / hour | everyone | `POST /api/auth/email-verification`, after the per-email limit. Logs a warning | 503 "Email verification is busy. Try again in a few minutes." |
| `auth:per-ip` | unchanged | IP | now also `register/check`, `/resend` and `/verify` | 429 |

- Register counts `auth:per-ip` → `auth:per-account` → `register:per-ip` → `register:per-email` → `register:global`. `register:global` (1,000 an hour) now also caps how many sign-up emails go out in an hour.
- Each code also ends after 5 wrong tries. Together with `email-code:per-email`, guessing a code for an address you don't own is about 1 in 100,000 a day, and every try emails the address.
- `account-exists:per-account` uses `Store.Allow` directly, since it has no request to answer; it's declared beside the others in `limits.go` so `TestRateLimitNames` still sees every name.
- `RATE_LIMITS=off` (e2e) switches them all off, the listener's included.

## Web app

- **`pages/auth/register.tsx`**: on 202 it opens `/auth/register/verify` with `?next=` carried over.
- **`pages/auth/register/verify.tsx`** (new): its `clientLoader` calls `register/check` (a 401 redirects to the form with the message). `CodeField`, **Verify** (`<Button loading>`, full width), then text links: Send a new code (toast "We sent a new code."), Use a different email, Already have an account? Sign in. A 422 shows under the field, a 401 goes back to the form, a 409 goes to login with the notice, a 429 shows as the form's error.
- **`pages/auth/login.tsx`**: `?notice=account-exists` shows a `Notice` with the reset link; an unknown `notice` shows nothing.
- **`pages/app/verify-email.tsx`** (new, `/app/verify-email`, outside the app layout like onboarding).
- **`pages/app/settings.tsx`**: the badge on the email label and the Verify email dialog, both shown only when `session.emailVerification !== "off"`. The dialog's code step and the verify page share one component, `components/app/email-verification/code-form.tsx`, since three pages use it, built on `CodeField`.
- **Gate:** `lib/loaders.ts` gets `verifiedLoader` (or the app layout's loader checks it): `emailVerification === "required" && !user.emailVerifiedAt` → `/app/verify-email?next=…`. The API client turns a 403 with `verificationRequired` into the same navigation, as a 401 turns into login.
- **API client** (`lib/api/auth.ts`): `register()` returns the session or `{email}`, with a `needsVerification()` guard; `registerCheck()`, `registerResend()`, `registerVerify(code)`; `emailVerification().store()` and `.update(code)`.
- **Admin**: the Users list and detail show Verified / Not verified.

## Security risks and how they're avoided

| Risk | Avoided by |
| --- | --- |
| Sign-up tells you an address is taken, by its answer | The same 202, body, cookie and page for every address; a taken address's row fails codes exactly as a new one's does; request limits count every address alike; the cap that differs is hidden in the listener |
| …or by its timing | The same queries for every address, no `users` read until a right code, the same HMAC and constant-time compare on every row; asserted by the query-hook test ([Timing](#timing-no-difference-to-measure)) |
| **Two sign-ups for one new address at once:** an attacker starts one just after the owner with their own password. If codes were matched by email, the owner might type the code from the attacker's sign-up and create their account with the attacker's password | Each sign-up is its own row, found by **the browser's cookie**, never by email. A code only completes the sign-up in the browser that started it |
| Guessing a code to claim an address you don't own | 5 tries per code, `email-code:per-email` 10 a day from anywhere, 15 minutes per code, constant-time comparison |
| Taking over an existing account | Nothing in sign-up changes an existing account; its email has only Sign in and Reset links |
| Recovering a code from a stolen database | HMAC under a key from `APP_KEY`, not a plain digest |
| Using our server to spam an inbox, or to send a stranger's words | Per-email, per-IP and global limits on the request; the hidden 5 a day on "already have an account"; no typed name in `verify-email` |
| A flood of verification mail when `EXISTING=required` is turned on | Codes go out only when a user presses Send code; `email-verification:per-email` and `:global`; the recommended rollout (optional first) |
| An unverified session reaching data | `requireSession` refuses it outright; only five named routes use `requireSessionUnverified` |
| A stale code arriving late after a retry | The listeners drop requests older than 15 minutes; a rerun replaces the code |
| Being talked into reading the code out | The email says never to share it |
| A crafted login link showing misleading text | `?notice=` picks one of our fixed messages |
| Pending passwords lingering | Rows end after 15 minutes and `Cleanup` deletes them; completion deletes every row for the email in its transaction |
| A forged request from another site | Every new route is a mutation behind the Origin and content-type checks; signed-in ones need CSRF; the registration cookie is SameSite=Strict |

## Considered and rejected

### Invitation links verifying the address

**Invitation links can't verify email addresses.** The idea: someone who signs up from an invitation link holds a 256-bit token that was emailed only to that address, so the account could start out verified and skip the code. The token is that strong, and the shortcut would have checked that the typed email matched the invited one and that the invitation was live.

It was rejected because the link proves you *have the link*, not that you *own the inbox*, and links get passed around for seven days.

> **Human reviewer:** In the edge case, an invitation link is shared with someone else ("Just use my invitation link if you can't see yours."). That would allow the receiver to sign up with that person's email as "verified". An extreme edge case, but for corporate environments we shouldn't have this.

So invited people enter the code like everyone else (one extra step), then return to the link and join as today. A password reset link is different and does verify: whoever completes one controls the account anyway.

### Updating an existing account from the sign-up form

The idea: the "You already have an account" email carries a code, and entering it replaces the account's name and password with the ones just typed, like a password reset. Rejected because it turns a 6-digit code into a way to take over an account:

- **Guessing:** an attacker who starts sign-ups for someone's address needs only the code, not the inbox. Even with a daily cap that's about 0.4% a year against one account, where a reset link (256 bits) can't be guessed at all.
- **A race:** if the owner and an attacker both sign up for the address at once, the code the owner types could apply the attacker's password.
- **Two-factor:** it would also have needed care not to sign straight into an account with MFA on.

Instead the email links to sign-in and to password reset, which already work and can't be guessed.

### A visible per-account limit on "You already have an account"

Refusing the request after 5 a day for a taken address would show which addresses are taken (only they would get the 429). The cap stays, but in the listener ([above](#a-limit-in-a-listener-an-exception-to-mailmd)).

## Tests

Go integration (real PostgreSQL, factories):

- **Sign-up:** 202 with the same status, body, headers and cookie for a new and a taken address; a row and `auth.registration.requested`; no user row. **The query hook:** `register`, `resend`, `check` and a wrong `verify` touch the same tables for both, and never `users`.
- **The listener:** `verify-email` for a new address; `account-exists` and a never-sent code for a taken one; the 6th in a day sends nothing and records `rate_limit.reached` once; a request older than 15 minutes sends nothing; a rerun replaces the code.
- **Verify:** the right code creates a verified user, deletes every pending row for the email and starts a session; wrong is 422 and `attempts` + 1; the 5th wrong is 401; expired and no cookie are 401; **another browser's cookie with the right code fails**; a taken address's row gives the same 422s and 401; an account made meanwhile is 409.
- **Check and resend:** 401 without a live row; resend makes a new code, resets attempts and is counted.
- **Signed-in verification:** send then verify sets `email_verified_at`; 409 when already verified; 422, the 5th wrong and expired (410); a new code replaces the old; a password reset verifies.
- **The gate:** with `EXISTING=required`, an unverified user gets 403 `verificationRequired` on a workspace route, a project route and `PATCH /api/auth/me`, and 200 on the five allowed routes; with `optional` nothing is refused; a route test walks `Routes` and checks that only the five allowed routes use `requireSessionUnverified`.
- **Limits:** each new limit refusing alike for new and taken addresses where it applies, with one `rate_limit.reached`.
- **Verification off:** today's 201 and 409, `verifiedBy: "none"`, the new routes unknown, no gate.
- **Config:** `on` with `MAIL_DRIVER=off` refused; `EXISTING=required` with verification off refused; bad values refused; defaults `on` and `optional`.
- **Mail:** both templates in `examples` (`TestEveryTemplateRenders`), the account name escaped, no name in `verify-email`.

Vitest: the register verify page (loader 401, 422 under the field, resend toast, 409 → login with the notice), the login notice, the settings badge and dialog (hidden with verification off), the verify-email page's redirects.

Playwright: the `register` helper in `workspace.spec.ts` reads the code from Mailpit, as reset links are read now, so every flow runs with verification on. One test signs up a taken address and checks the email and the login notice; one reloads the verify page mid-sign-up.

### Changing the settings mid-run (`email-verification-settings.spec.ts`)

The main server runs with the defaults (`on`, `optional`) for the whole run. This spec plays the real rollout: accounts made while verification was off, then verification turned on, then made required. It does this by **restarting a second server with other settings on the run's database** (decision 12):

```
make test-e2e reserves two ports: E2E_PORT (main) and E2E_SETTINGS_PORT (this spec's)

1. start the second server: EMAIL_VERIFICATION=off
     sign up Ada and Ben through the UI           → no code step, unverified
     sign up Ada again                            → today's 409 (off mode end to end)
   stop it
2. on the main server (on, optional)
     sign in as Ada → Account settings: "Not verified" → Verify email → code from Mailpit → "Verified"
     sign in as Ben → no gate, the app works
3. restart the second server: EMAIL_VERIFICATION=on, EMAIL_VERIFICATION_EXISTING=required
     sign in as Ben → /app/verify-email "You must verify your email to continue"
     a workspace API call from the page → 403 verificationRequired
     Send code → code from Mailpit → Verify → /app
     sign in as Ada (verified in step 2) → straight to /app
   stop it
```

- **The second server is the same `bin/api`**, built once, started and stopped by the spec (`test.beforeAll`/`afterAll`, SIGTERM for a graceful shutdown) through `scripts/serve-test.sh` with `E2E_SHARED_DATABASE=1`. That uses the main port's database and skips the rebuild; every other check in the script stays. The settings come from the spec's environment over `.env.testing`. Only one extra process exists at a time, for about a minute, so the run costs about the same.
- **Both servers must have the same `APP_KEY`.** Both run the event worker on one database, so the main server's listener may make a code that the second server checks. With different keys the HMACs never match (and MFA secrets wouldn't open). Today `serve-test.sh` makes a fresh key on every start; it will take `E2E_APP_KEY` from `playwright.config.ts`, made once per run like `E2E_ADMIN_ROUTE_KEY`. The same applies in production: every server sharing a database needs the same `APP_KEY` (deployment.md says so).
- **Each server has its own origin** (`APP_ORIGIN` per port), so the Origin check passes on each. Browsers share cookies across ports on one host, so each step uses its own browser context.
- **The second port is reserved for the whole run** by `e2e-preflight.sh` (the first free port after the main one, the same flock), so another run can't take it between steps.

## Decisions

1. **Pending registrations, not unverified accounts.** With verification on, no user row exists until the code is right. *Agreed.*
2. **A 6-digit code typed into the page, 15 minutes, 5 tries**, not a link, so it works across devices and from a phone's mail app. *Agreed.*
3. **Each sign-up is tied to its browser by an HttpOnly cookie**, and verify takes only `{code}`. *Agreed.*
4. **Invitation links don't verify.** *Agreed* ([rejected above](#invitation-links-verifying-the-address)).
5. **A taken address gets "You already have an account" with Sign in and Reset links, and no code**; the sign-up page still shows the code step. *Agreed.*
6. **The cap on "You already have an account" (5 a day per account) is in the listener, hidden**, as a documented exception to mail.md's rule. The request keeps `register:per-email` (3 an hour, every address alike). *Agreed.*
7. **The column is `email_verified_at`** rather than `verified_at`: it says what was verified (an account can be verified in other ways later, such as MFA or an admin check), and it's the name Laravel uses, whose vocabulary [naming.md](../../../docs/naming.md) follows. *Recommended.*
8. **The settings are `EMAIL_VERIFICATION=on|off` (default `on`) and `EMAIL_VERIFICATION_EXISTING=optional|required` (default `optional`)**, rather than `REQUIRE_EXISTING_VERIFY`: one prefix keeps them together in `.env`, and the values say what they do. *Recommended.*
9. **Gating is in the API**, so a new route can't skip it: an unverified user's session works only on `me`, logout, revoking other sessions and the two verification routes while `required`. *Recommended.* Not allowed: account deletion and password changes, so a session that can't prove the inbox can't do anything lasting.
10. **The table is `pending_registrations`**, after the existing "register" vocabulary (`/api/auth/register`, `register:per-ip`, `auth.registered`). *Recommended.*
11. **A completed password reset marks the email verified.** It proves the inbox with a 256-bit link, and it cuts the mail needed when `required` is turned on. *Recommended.*
12. **e2e changes the settings mid-run by restarting a second server on the run's database** (off → optional → required), sharing one `APP_KEY` ([above](#changing-the-settings-mid-run-email-verification-settingsspects)). *Agreed.* Rejected: an API route that switches the setting at runtime, because a test-only switch for an auth gate doesn't belong in the production binary.
13. **The Account settings action is a `TextButton` in the email field's description ("We haven't confirmed this address. Verify email") opening a dialog**, with the badge on the label. forms-and-actions.md gains this pattern. *Agreed.*
14. **Admins' door isn't gated**: admins come from the command line and must have MFA. *Recommended.*

## Not included

- A way to change an account's email (no such feature yet). Under `required`, someone whose old address can't receive mail is stuck at the verify page; an operator command (like `reset-mfa`) or an email change would be its own plan.
- A "your password was changed" security email (none exists for resets or changes either).
- Sign-in by emailed code or magic link.
- Bounce and complaint handling.

## How the build differs

- **Register's 202 is `{verificationRequired: true, email}`**, not `{email}`, so the web client tells it from a session without guessing (`needsVerification`). `register/resend` answers the same.
- **The gate is a wrapper, not a session variant.** `allowUnverified(…)` marks the five routes in `routes.go` (it composes with `requireSession` and `requireSessionUnmetered`, since logout and revoking sessions are unmetered), and `signedIn` refuses everything else. `TestOnlyFiveRoutesAllowUnverified` reads `routes.go`, as `TestAdminRoutesStayInTheAdminGroup` does, instead of walking the mux, which can't be listed.
- **Store names:** `FindPendingRegistration` (it returns the record) instead of `CheckPendingRegistration`. `Store.recordID` returns an event's `evt_` ID for the row that points at it.
- **Badge colours** are the existing `lime` (Verified) and `zinc` (Not verified), as `MfaBadge` uses, rather than new green and amber. The admin Users list shows only "Not verified"; a user's page shows either.
- **"Send a new code"** says so with an inline success notice on the two sign-in-style pages, which have no `Toaster`; Account settings' dialog uses a toast. The shared component is `EmailCodeStep` (`components/app/email-verification/email-code-step.tsx`) for the two pages; the dialog uses `CodeField` directly, since its chrome is a dialog's.
- **The signed-in code ending** (expired, none sent, or the 5th wrong one) is 410, so the page and dialog go back to Send code; a sign-up's is 401, back to the form.
- **`account-exists:per-account` counts a delivery's first attempt only**, so retrying a failed send doesn't spend it; `RATE_LIMITS=off` switches it off with the others.
- **`register:global`** also counts `register/resend`.
- **"Try again in …" rounds to hours** past an hour ("24 hours"), for the daily limits.
- **Config's zero value is off,** so a `Config` built by hand (the Go tests) signs up as before; `config.Load` defaults to on. The `cmd/admin` config test now sets `EMAIL_VERIFICATION=off` beside `MAIL_DRIVER=off`.
- **E2E:** the Mailpit helpers moved to `tests/e2e/mail.ts` (`mailedCode`, `enterMailedCode`, `signUpThroughApi`), shared by all four specs. The rollout spec is `email-verification-settings.spec.ts`; the preflight reserves `E2E_SETTINGS_PORT` and `test-env.sh` keeps it and `E2E_APP_KEY`. `admin.spec.ts`'s "Workspaces" heading check became exact: the empty list's "No workspaces yet" heading matched it whenever the list loaded first (a race from before this change).

