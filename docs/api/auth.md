# Authentication and security

Code: `internal/auth` (primitives), `internal/httpapi/endpoints/account` (the controllers), `internal/httpapi/credentials.go`, `sessions.go` and `middleware.go` (the checks, sessions and limits they share), `internal/store/sessions.go`.

## Passwords

- Argon2id: 19 MiB, 2 iterations, 1 lane, random 16-byte salt, 32-byte digest, constant-time comparison.
- Sign-up requires 12+ Unicode characters; every password is at most 128 UTF-8 bytes. Login only checks that cap, so seeded development accounts with a short `DEMO_PASSWORD` (`password`) can sign in. Emails are trimmed and lowercased; names are 1–80 characters.
- Login against an unknown email still checks a dummy hash, and every failure gives the same message.
- At most 4 password hashes run at once per process (503 when busy).
- **Deleting a workspace or the account** also checks the current password, in the same request, the same way (`PasswordConfirmed`: `auth:per-account`, behind `LimitAuth`), so a stolen session can't destroy either ([Deleting](#deleting)).
- **Changing the password** (signed in) without MFA takes the current password twice: `POST /api/auth/password/verify` checks it so the web app can move on to the new one, and `PATCH /api/auth/password` checks it again, because nothing remembers that the first check passed. **With MFA on, `PATCH` takes a `code` (authenticator or recovery) instead of the current password**, which it ignores, and the web app skips verify; the code counts against `mfa:per-account` instead of `auth:per-account`. Which one is needed is decided from the locked rows, not the request, so the right password without a code is refused (422 on `code`). The new password follows the sign-up rule and must differ from the current one. The check, saving the new hash and deleting **every** session of the user's run in one transaction with the user row locked (a refused new password rolls back, so the code isn't used up); then this browser gets a new session (cookie and CSRF token), like login. A wrong current password or code is 422 with `field`, not 401, which the web app treats as an expired session. Plan: [plans/features/implemented/password-change-mfa.md](../../plans/features/implemented/password-change-mfa.md).
- Plaintext passwords are never logged or stored.

## Password reset by link

Code: `internal/httpapi/endpoints/account/password_resets.go`, `internal/store/password_resets.go`, `internal/mail` (listener and mailbox). Plan: [plans/features/implemented/forgot-password.md](../../plans/features/implemented/forgot-password.md).

1. **Request** (`POST /api/auth/password-reset {email}`): after the rate limits below, the API records `auth.password_reset.requested` with the email and IP and answers `202`. It doesn't look the account up, so the status, body and timing are the same for every email.
2. **Listener** (`mail.password_reset`, run by the worker about a second later): for an email with an account, it makes a 256-bit token (`auth.Token`), stores only its SHA-256 digest in `password_resets` (one row per user, so a newer link replaces the older one), sends the link `APP_ORIGIN + /auth/forgot-password/<token>` with `mailer.Send`, and records `auth.password_reset.sent`. An email without an account, or a [suspended](#suspended-accounts) one, gets nothing. Requests older than the link's 30-minute lifetime are dropped, since retries can run an hour later. The token is never in the database, an event or the server log.
3. **Page** `/auth/forgot-password/<token>`: checks the link (`POST …/check {token}`) before asking for a password.
4. **Reset** (`PATCH /api/auth/password-reset {token, newPassword}`): sign-up's password rule; a cheap validity check before hashing, then one transaction locks the user, then the link (the same order as a password change), checks the link again, saves the hash, deletes the link and **every** session, and records `auth.password_reset.completed`. Nobody is signed in afterwards: the page sends the user to login with the email filled in.

- A link works once, for 30 minutes. A newer request, a reset or a signed-in password change ends it.
- The link is built from `APP_ORIGIN`, never the request's `Host`. Tokens travel to the API only in JSON bodies. The page URL holds the token, so the 30-minute lifetime and single use limit what browser history can leak; pages send `strict-origin-when-cross-origin` referrers and load nothing from other sites, and Go's access log names the page's label (`page /auth/*`), never its path ([observability.md](observability.md#the-access-line)).
- **Delivery:** over SMTP (`MAIL_DRIVER=smtp`; Mailpit locally), or the development mailbox (`log`, refused in production since it holds working links) ([mail.md](mail.md)). With `MAIL_DRIVER=off` the request endpoint answers 503.

## Email verification

Code: `internal/httpapi/endpoints/account/registrations.go` (sign-up), `endpoints/account/email_verification.go` (signed in), `internal/store/registrations.go`, `email_verifications.go`, `internal/mail/registration.go` (both listeners), `internal/auth/email_code.go`, `internal/config/verification.go`. Plan: [plans/features/implemented/email-verification.md](../../plans/features/implemented/email-verification.md).

`EMAIL_VERIFICATION=on` (the default; refused with `MAIL_DRIVER=off`) proves every new account owns its email with a 6-digit code, and closes the last place that said whether an email has an account. `off` signs up as before: the account at once, unverified, and 409 "Unable to create this account…" for a taken email.

**Signing up** (`internal/httpapi/endpoints/account/registrations.go`):

1. **Register** (`POST /api/auth/register`, the same validation and limits, plus `register:per-email`): hashes the password, saves a row in `pending_registrations` (15 minutes), records `auth.registration.requested {email, ip}`, sets the **registration cookie** (`lewcontext_registration`, `__Host-` in production; a 256-bit token, HttpOnly, SameSite=Strict, 15 minutes) and answers 202 `{verificationRequired, email}`. **No account exists until the code is right**, so with verification on `users` only gains verified accounts.
2. **Listener** (`mail.registration`, after the response): makes a code and stores its HMAC on the row (found by the requested event's `evt_` ID). For a new email it sends `verify-email` with the code. For an email that has an account it stores the HMAC of a code **nobody is sent** and emails **"You already have an account"** instead, with Sign in (`/auth/login?notice=account-exists`) and Reset password links and no code: nothing in sign-up can change an existing account. That email is capped at 5 a day per account (`account-exists:per-account`), in the listener, silently ([mail.md](mail.md#a-limit-in-a-listener)).
3. **Code page** `/auth/register/verify`: `POST …/check` (cookie) tells it which email, so a reload resumes it; `POST …/resend` makes a new code for the same row (15 more minutes, tries reset, counted like a sign-up); `POST …/verify {code}` checks it.
4. **Verify**: one transaction locks the row, compares the HMAC in constant time and records `auth.registration.checked`. Wrong: 422 on `code`, and the 5th wrong deletes the row (401). Right: creates the user with `email_verified_at`, deletes **every** pending row for that email, records `auth.registered {verifiedBy: "code"}` and signs in (201). If an account took the email meanwhile, 409 and nothing is created.

- **Sign-up doesn't say whether an email has an account.** Register, check, resend and a wrong verify answer the same and run the same queries for a taken email as for a new one: none of them reads `users` (only the listener does, after the response), and a taken email's row holds a real-looking HMAC, so every code fails it exactly as a wrong one fails a new email's. Limits count every address alike. `TestSignUpLooksTheSameForATakenEmail` compares the answers, headers and the tables each request touches. Only the inbox can tell the two apart.
- **A code works only in the browser that started the sign-up.** Rows are found by the cookie, never by email, and one email can have several. Otherwise, if an owner and a stranger signed up the same new email at once, the owner could type the code from the stranger's sign-up and create the account with the stranger's password (`TestSignUpCodeBelongsToItsBrowser`).
- **Codes:** 6 digits from `crypto/rand`, 15 minutes, 5 tries, then `email-code:per-email` (10 checks a day per email, from anywhere), so guessing one is about 1 in 100,000 a day. Stored as HMAC-SHA256 under a key derived from `APP_KEY` (a plain hash of a million codes reverses instantly). Never in an event, log or URL; the email shows it spaced and never as a link, so it works on whichever device reads the mail.
- **A blocked address** signs up like any other and is sent no code; verify refuses it if it was blocked after its code went out ([Blocking an email address](#blocking-an-email-address)).
- **Invitation links don't verify an address.** The token proves someone has the link, not that they own the inbox, and links get forwarded ("just use my invitation link"). Invited people enter the code too, then return to the link and join.

**Existing accounts** (made while it was off, or before it existed) have `email_verified_at` null:

- **Account settings** (with verification on): the email's label says Verified or Not verified, and an unverified one offers **Verify email**: `POST /api/auth/email-verification` records `auth.email_verification.requested`, whose `mail.email_verification` listener emails a code (one per account in `email_verifications`, replacing the last); `PATCH … {code}` verifies (422 wrong, 410 ended, 409 already verified) and records `auth.email_verified {via: "code"}`. Limits: `email-verification:per-email` (3 an hour), `email-verification:global` (500 an hour), `email-code:per-email`.
- **`EMAIL_VERIFICATION_EXISTING=required`**: such an account signs in as usual (password, then MFA), but `RequireSession` refuses it every route with 403 `{verificationRequired: true}` except reading the session, signing out, signing out other sessions and the two verification routes, which `AllowUnverified` marks in `router/routes.go` (`TestOnlyFiveRoutesAllowUnverified`). The web app sends it to `/app/verify-email`. Codes go out only when it presses Send code.
- **A completed password reset verifies the email** (`auth.email_verified {via: "password_reset"}`): the link went only to that inbox, and whoever uses it controls the account anyway.
- **Admins' door isn't gated**; their app sessions are.

## Blocking an email address

Code: `internal/httpapi/endpoints/account/email_blocks.go`, `internal/store/email_blocks.go`, `internal/database/lock.go` (the email lock), `internal/mail` (the link and the check), `cmd/admin/blocks.go`. Plan: [plans/features/implemented/email-blocks.md](../../plans/features/implemented/email-blocks.md).

Invitations go to addresses an inviter types, so each one carries a **block link** (`/auth/email-blocks/<token>`, [mail.md](mail.md#blocked-addresses)). Its page checks the link (`POST /api/email-blocks/check`), then blocks (`POST /api/email-blocks {token}`) or unblocks (`DELETE`) the address it went to. A blocked address gets no email at all and can't become an account.

- **Only an address without an account can be blocked** (409 otherwise, and the page says to delete the account first): blocking a working account's address would cut it off from password reset. Only the inbox has the link (256 bits, stored as a digest, a year), and the same link unblocks it.
- **Nothing anyone types reveals a block.** "Blocked" would tell a prober that the inbox is read by a person without an account, and a harasser that their invitation was seen. So:
  - **Invitations** to a blocked address answer 201 and are listed with `sentAt` like any other; `Send` refuses the email and the listener records no `.sent`.
  - **Sign-up with verification on** answers 202 and opens the code page as for any address; no code is sent. The code page tells everyone "If you blocked this address, no code can reach it: unblock it from the link in our email first."
  - **Verify** checks again: a right code for an address blocked after it was sent ends the sign-up (401, "This address has been blocked…", its pending rows deleted). Only someone holding a code from the inbox gets there.
  - **Sign-up with verification off** answers a blocked address with the taken email's 409, which that mode already gives.
  - Only holders of a link or code from the inbox ever see the word.
- **Racing:** blocking needs "no account uses it" and creating an account "it isn't blocked", so both take a transaction-scoped advisory lock keyed by the email (`database.LockEmail`) before checking: `BlockEmail`, `CompleteRegistration` and `CreateUser` (`TestBlockRacingSignUp` fails without it).
- **Events:** `email.blocked` and `email.unblocked` (`by`: `link`, `cli` or `admin`), with the address, pruned after 90 days ([events.md](events.md#catalogue)).
- **The operator** can list and unblock addresses (`admin blocks`, `admin unblock -email … [-confirm]`), never block one ([admin.md](admin.md#admins-admins)). So can the admin dashboard: every admin sees the list on Email blocks (each read recorded), and an `operator` can unblock one there behind a fresh code (step-up) and a written reason, recorded as `admin.email.unblocked` ([admin.md](admin.md#email-blocks)). Nothing in the dashboard blocks.

## Two-factor authentication

Code: `internal/auth/totp.go`, `encrypt.go`, `recovery.go` (primitives), `internal/httpapi/endpoints/account/mfa.go` and `recovery_codes.go` (the controllers), `internal/httpapi/second_factor.go` (checking codes and setting MFA up, shared with the admin door), `internal/store/mfa.go`. Plan: [plans/features/implemented/mfa.md](../../plans/features/implemented/mfa.md).

Optional for every account (Account settings → Two-factor authentication → **Enable**): codes from an authenticator app (TOTP, RFC 6238), plus 10 single-use recovery codes.

1. **Setup** (`POST /api/auth/mfa/setup {password}`): checks the password like the other password routes (`auth.password_checked`, step `mfa_setup`), makes a 160-bit secret, stores it sealed and pending, and returns it once as `{secret, uri}` for the QR code. A new setup replaces a pending one; one left unconfirmed for 15 minutes is deleted by the cleanup.
2. **Enable** (`POST /api/auth/mfa {code}`): the first code from the app confirms the secret. Under the user's row lock it turns MFA on, stores 10 new recovery codes, signs out **every other session** (they never passed a second factor) and records `auth.mfa.enabled`. The codes are returned once.
3. **Sign in**: a right password with MFA on starts **no session**. It makes a challenge (a 256-bit token, digest in `mfa_challenges`, purpose `login`, 5 minutes) in the cookie `lewcontext_mfa` (`__Host-lewcontext_mfa` in production; HttpOnly, SameSite=Strict) and answers `{mfaRequired: true}`, recording `auth.login_attempted` with result `mfa_required`. `POST /api/auth/login/mfa {code}` then takes an authenticator code (6 digits, spaces allowed) or a recovery code; the right one deletes the challenge and starts the session as login does (`auth.logged_in`). A wrong one is 422 with `field: "code"`; the fifth wrong one deletes the challenge and answers 401, as does an expired or missing one, so the password is needed again.
4. **Turning it off** (`DELETE /api/auth/mfa {password, code}`) and **new recovery codes** (`POST /api/auth/mfa/recovery-codes {password, code}`) need the password **and** a second factor, so a stolen session can't do either.
5. **Changing the password** (`PATCH /api/auth/password {newPassword, code}`) takes a code **instead of** the current password ([Passwords](#passwords)): someone with a session and a phished password can't set a new one. Someone with the session and the second factor but not the password can (the unlocked laptop and phone, or a downloaded recovery-codes file beside the session), as with GitHub's sudo mode.

- **Codes:** HMAC-SHA1, 6 digits, 30-second steps, one step of drift either way, compared in constant time. A code's step must be after the last one used (`last_used_step`, checked and saved under the row lock), so each code works once.
- **Recovery codes:** 10 characters of Crockford base32 (50 bits), shown as `k7p2m-x9qfr`; case, hyphens and spaces don't matter. Each works once.
- **At rest:** the secret is sealed with AES-256-GCM under a key derived from `APP_KEY` (HKDF), bound to the user's internal ID (the GCM additional data), so a database dump alone, or a value copied to another row, is useless. Recovery codes are HMAC-SHA256 digests under a second derived key, so they can't be guessed offline either. **Changing `APP_KEY` locks out every account with MFA on**; there is no key rotation yet.
- **Limits:** `mfa:per-account` (5 code checks per 15 minutes, per user, from any IP) on every code check (changing the password and an admin's step-up included), plus `auth:per-ip` on `login/mfa` and 5 tries per challenge ([rate-limits.md](rate-limits.md)). Turning MFA off and new recovery codes are recovery routes: unmetered by the budget, like the password routes.
- **What doesn't change it:** a password reset or a password change leaves MFA on, so a reset link alone can't sign anyone in. Deleting the account deletes its rows (cascade).
- **Lost the app and the codes:** only an operator can help, in one of two ways, and either way checks who is asking out of band first: it's a way around their second factor.
  - **From the admin dashboard** ([admin.md](admin.md#actions)): Reset two-factor authentication on the user's page, by an admin with the `operator` role, behind a fresh code (step-up), a written reason and the tick "I've confirmed who is asking, outside the app" (`confirmed: true`). It turns MFA off (authenticator, recovery codes, sign-in challenges), signs out every session and records `auth.mfa.disabled` with reason `reset` **and the admin as actor**, beside `admin.user.mfa_reset` (the reason). Never on the admin's own account or another admin's; 409 when MFA is off or its setup was never confirmed.
  - **From the command line**, `make admin cmd=reset-mfa email=… CONFIRM=1` (`cmd/admin reset-mfa`, previews without `-confirm`), for any account, admins included. It turns MFA off, signs out every session (admin sessions too) and records `auth.mfa.disabled` with reason `reset` and no actor; for an admin it reopens the 24-hour window to set up a new authenticator at the admin door.
- **Admins** must keep MFA on: turning it off answers 409 "Admins must keep two-factor authentication on." ([admin.md](admin.md)).
- **Responses:** every response with the user carries `mfaEnabledAt` (null while off). Secrets and codes appear only in the one response that creates them, and never in an event or log.
- **Not stopped:** a phishing site relaying a password and a code in real time. Passkeys would; they're not built.

## Sessions

- A session token is 32 random bytes (base64url, 43 characters). Only its SHA-256 digest is stored in `sessions`.
- **Cookie:** `lewcontext_session` locally, `__Host-lewcontext_session` in production. HttpOnly, SameSite=Lax, Path=/, Secure in production.
- **Expiry:** 30 minutes idle (every request refreshes it) and 24 hours absolute. Expired sessions answer 401.
- **Last seen:** the same session check moves the account's `users.last_seen_at`, in the same statement, only when it's over an hour old, and records no event, so an active account costs one row write an hour. The monitoring pages count active accounts from it ([monitoring.md](monitoring.md)).
- **Login, register (or a sign-up's right code) and a password change rotate the session.** The previous one is deleted, and at most 5 sessions exist per user, enforced in a transaction with a row lock on the user.
- Logout deletes the session. `DELETE /api/auth/sessions` deletes all others.
- Session tokens never go in localStorage or URLs.
- **A suspended account has none** ([below](#suspended-accounts)).
- **Admin sessions are separate** ([admin.md](admin.md)): their own table and `lewcontext_admin` cookie (SameSite=Strict), 15 minutes idle and 8 hours at most, 2 per admin, started only by an admin sign-in with a code, and valid only while the user is an admin and the `ADMIN_ROUTE_KEY` they were made under is still accepted: the current key, or `ADMIN_ROUTE_KEY_PREVIOUS` while a rotation finishes, and only when the request comes with that same key. Each remembers the address and browser it signed in from, and its first use from another network is recorded (`admin.session.moved`). A password change, a password reset, an MFA reset, revoking admin, deleting the account and an operator signing them out (`admin.admin.signed_out`) end them. An app session never counts as one, nor the other way round.

## Suspended accounts

An operator can suspend an account from the admin dashboard, and the operator from the command line (`admin suspend`), never an admin's ([admin.md](admin.md#suspending-an-account)):

- **Every session ends** (app and admin), with its reset link and sign-in challenges, in one transaction with the user's row locked. `FindSession` and `FindAdminSession` never find a suspended account's session, and `CreateSession` and `CreateAdminSession` refuse one under the user's lock, so every session middleware (`RequireSession`, `RequireSessionUnmetered`, the `/app` page gate, `RequireAdminSession`) refuses it, however a session came to exist.
- **Sign-in says so only after the password and code:** a wrong password is the usual 401, as for any account or none, so a suspension can't be probed; the right password (MFA off) or the right code (MFA on; the password step asks for a code as for anyone) is `403 {"error": "This account has been suspended. Contact support.", "suspended": true}`, with no session, recorded as `auth.login_attempted` with `result: "suspended"`.
- **No reset link:** a reset request is answered as for anyone and nothing is sent; a link issued before stopped working. Email verification needs a session. Neither can undo a suspension; only unsuspending does.
- Its workspaces keep working for everyone else.

## CSRF and origin checks

For every mutation (anything except GET, HEAD and OPTIONS), the `security` middleware requires:

- an `Origin` exactly equal to `APP_ORIGIN`,
- no `Sec-Fetch-Site: cross-site`,
- `Content-Type: application/json`, except on a DELETE without a body (a DELETE that has one, such as deleting a workspace with its name and password, needs it too).

Signed-in mutations also need `X-CSRF-Token`: an independent random token returned by login, register and `/api/auth/me`, compared in constant time. CORS is not enabled.

## Rate limits

Sign-in, sign-up (and its codes) and password reset are limited per IP, per account or email, and for everyone, and every signed-in request has a budget per user, with tighter limits on creating workspaces and projects and quotas on how many can exist. All of it is in [rate-limits.md](rate-limits.md). What matters here:

- `auth:per-ip` (60 / 15 min) and `auth:per-account` (10 / 15 min) guard every password check: `LimitAuth` (the IP limit and a hashing slot) and `AccountLimit`. Both deletions and the signed-in password endpoints use the session user's email, so a stolen session can't guess the password faster than a login could.
- Logout, revoking other sessions, the password endpoints, turning MFA off and new recovery codes skip the signed-in budget, so a stolen session can't use it up to stop its owner.
- **The client IP** is `httpapi.ClientIP(r)` (`httpapi/client_ip.go`), worked out once per request by `ClientAddress`. It is the connection's address, unless `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` are set **and** the connection comes from a trusted proxy: then it is the header's address (`X-Forwarded-For` read from the right, skipping trusted proxies; the single-value headers only when they hold exactly one address). A header from any other connection is ignored, so a client can't forge one for fresh limits or fake audit IPs. Without the variables, everyone behind a proxy (and Vite's dev proxy) shares one IP. Per-IP limits count IPv6 clients by their /64. A scaled deployment still needs an edge limiter. Account lockout abuse is possible with this fixed-window design.

## Admin dashboard

`/admin/<ADMIN_ROUTE_KEY>`, behind the secret key on every page and API call (`X-Admin-Key`; wrong keys get the ordinary 404, and so does the right key from an address outside `ADMIN_ALLOWED_IPS` when it's set), then an admin's password and second factor and an admin session. Admins and their role (`viewer`, or `operator` for the actions) are granted only from the command line, must have MFA, and can only read, apart from the [actions](admin.md#actions) an operator takes behind a code from the last 5 minutes (`POST /api/admin/step-up`, `RequireAdminStepUp`) and a written reason: unblocking an email address, signing an account out, resetting its MFA, suspending it, revoking a workspace's invitations, retrying deliveries, blocking a client address or range ([IP blocks](admin.md#ip-blocks), refused 403 before anything else, never the admin door with its key nor the health checks) and signing another admin out. Every admin route that changes anything names the role it needs (`RequireAdminRole`, 403). A rotated key can stay accepted for the sessions made under it (`ADMIN_ROUTE_KEY_PREVIOUS`), never for a new sign-in. Everything is in [admin.md](admin.md).

## Audit trail

Sign-ups (requested, sent, each code checked, completed), email verifications, login attempts (with the email typed and the client IP), sign-ins, sign-outs, other sessions revoked, password checks and changes, two-factor authentication turned on or off, each second-factor check and new recovery codes, deleted accounts, reset links requested, sent and used, the first refusal of each rate-limit window (`rate_limit.reached`), and admins granted, their role changed, revoked, signing in and out, their sessions moving or signed out, the right key refused from outside the allowlist, everything they read and every action they take, with why (`admin.*`), accounts suspended and unsuspended, everyone signed out from the command line, data downloaded and accounts erased, and each confirmed prune run, are recorded in the `events` table; never passwords, hashes or tokens. See [events.md](events.md). The audit events among them (accounts and sign-in, admins, servers, retention) are kept a year, the rest 90 days, and are also written as log lines for the operator to ship off the server ([compliance.md](../compliance.md)).

## Transport and headers

- **API:** `Cache-Control: no-store`, `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`. Every response, pages too, has `X-Request-ID`; a 500 says only "Something went wrong" and the request's ID, a handler's panic included ([observability.md](observability.md)).
- **Limits:** request bodies 16 KiB, headers 16 KiB, 10 s per request context, and server read/write timeouts.
- **Web** (`internal/web`): a CSP per HTML file whose `script-src` allows only this origin plus the SHA-256 hashes of that file's own inline scripts (computed at startup; never `'unsafe-inline'` scripts), inline CSS allowed because Styletron needs it, self-hosted fonts, `frame-ancestors 'none'`, `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`, and HSTS when `APP_ENV=production`. The app shell is `no-store`; hashed assets are cached for a year, the admin build's (`/admin-assets/`) only by browsers (`private`), for the pass's 8 hours, and only with the assets pass an admin page sets ([admin.md](admin.md#the-assets-pass)).
- **Signed-in pages:** `GET /app` and `/app/*` without a valid session (missing, unknown, idle or expired) get a `302` to `/auth/login?next=<path>` before any HTML is sent.
- **Production:** HTTPS (`APP_ORIGIN`) and verified database TLS (`sslmode=verify-full`) are required.

## Workspaces and roles

Data belongs to workspaces; users reach a workspace through a `workspace_user` row with a role: `owner`, `admin`, `member` or `viewer`. What each role may do is a list of named permissions, `<resource>.<action>` after the route's CRUD action, granted in one table (`grants` in `internal/auth/permissions.go`):

| Permission | owner | admin | member | viewer | Guards |
| --- | --- | --- | --- | --- | --- |
| *(membership)* | ✓ | ✓ | ✓ | ✓ | `GET` the workspace and its projects |
| `workspace.update` | ✓ | ✓ | | | `PATCH /workspaces/{ws}` |
| `workspace.destroy` | ✓ | | | | `DELETE /workspaces/{ws}` |
| `workspace.invitation.index` / `.store` / `.destroy` | ✓ | ✓ | | | `GET`, `POST …/invitations`, `DELETE …/invitations/{inv}` ([Invitations](#invitations)) |
| `workspace.member.index` | ✓ | ✓ | | | `GET …/members` |
| `workspace.member.update` | ✓ | ✓ | | | `PATCH …/members/{usr}` |
| `workspace.member.destroy` | ✓ | ✓ | | | `DELETE …/members/{usr}` |
| `auth.AnyMember` | ✓ | ✓ | ✓ | ✓ | `DELETE …/membership` (leave) |
| `project.store` | ✓ | ✓ | ✓ | | `POST …/projects` |
| `project.update` | ✓ | ✓ | ✓ | | `PATCH …/projects/{prj}` |
| `project.destroy` | ✓ | ✓ | | | `DELETE …/projects/{prj}` |

A viewer reads everything except the member and invitation lists (which hold other people's emails) and changes nothing. For actions on another person's role, `Role.AssignableRoles()` also says which roles may be targeted: an owner any role, an admin only `member` and `viewer` (so only owners create admins or touch owners), others none. An action on a member needs the permission, and the target's current role (and, for a role change, the new one) in that list; otherwise 403. Acting on yourself goes through leaving, not `members/{usr}` (422).

- **`RequireWorkspace`** (after `RequireSession`) checks the `{workspace}` ID's shape, then loads it through the session user's membership in one query. A non-member, a missing workspace and a malformed ID all get the same 404, so workspace IDs can't be probed. The workspace and role go in the request context (`httpapi.Workspace(r)`).
- **`ws.Can(method, path, permission, handler)`** registers a workspace route behind `RequirePermission`, which answers 403 when `httpapi.Workspace(r).Role.Can(permission)` is false. The `ws` group panics at startup on any other mutation (`Handle` with a method other than GET or HEAD), so a mutation can't ship without naming its permission; one every member may do names `auth.AnyMember`. Unknown roles grant nothing.
- The workspace response carries `permissions` and `assignableRoles`, computed from the role, and the web app hides controls with `can(workspace, permission)` instead of keeping its own copy of the rules. The API is what enforces them.
- Creating a workspace makes the creator its `owner`, in the same transaction.
- **Members** (`internal/store/members.go`): changing a role, removing someone and leaving each run in one transaction that first locks the workspace row (`FOR UPDATE`), then re-reads the actor's role and the target's membership, so concurrent changes are serialised. A change that would leave no owner (demoting, removing or leaving as the only owner) is refused with 409 (`ErrLastOwner`); two owners demoting each other at once leave one. Promoting someone to owner, then stepping down, is how ownership moves. Workspace responses carry `canLeave` (false for the only owner) so the web app can say so before you try.
- **Removal** deletes only the `workspace_user` row: the user's next request to that workspace gets 404, their sessions and account stay, and the workspace's projects stay (projects have no owner). Only owners and admins can list members, so members and viewers never see other members' emails.
- **Transferring ownership** is promoting someone to owner, then leaving or stepping down; there is no separate transfer flow.
- Public IDs are unguessable (about 83 random bits), but access never depends on that: every workspace route checks membership.

## Invitations

Code: `internal/httpapi/endpoints/workspace/invitations.go`, `internal/store/invitations.go`, `internal/mail/workspace_invitation.go` (the listener). Plan: [plans/features/implemented/workspace-invitations.md](../../plans/features/implemented/workspace-invitations.md).

1. **Invite** (`POST …/invitations {email, role}`, `workspace.invitation.store`): the role must be in your `assignableRoles`, and never `owner` (invite as admin, then promote, so a link can't grant ownership). It first holds your user row and refuses a suspended account (401, as a dead session: the suspension came while the request was on its way), then under the workspace lock reads your role again, then refuses an existing member, a live invitation to the same email (an expired one is replaced), the 51st live one and the member quota (100 members plus live invitations). It records `workspace.invitation.created`, which queues the email. The answer is the same whether or not the email has an account.
2. **Listener** (`mail.workspace_invitation`, about a second later): makes a 256-bit token (`auth.Token`), stores only its digest and `sent_at` on the row (replacing any earlier link), emails `APP_ORIGIN + /auth/invitations/<token>`, and records `workspace.invitation.sent`. An invitation revoked, accepted or expired meanwhile gets nothing.
3. **Page** `/auth/invitations/<token>`: `POST /api/invitations/check {token}` (works signed out, `auth:per-ip`) returns the workspace's name, who invited you and the role.
4. **Accept** (`POST /api/invitations/accept {token}`, signed in): one transaction locks the workspace, then the invitation, checks the signed-in email is the invited one (403 otherwise, and the invitation is left as it was), adds the membership (an existing member keeps their role), deletes the invitation and records `workspace.invitation.accepted`. A second accept, or a revoke, waits and then finds nothing.
5. **Revoke** (`DELETE …/invitations/{inv}`, `workspace.invitation.destroy`, and the invitation's role in your `assignableRoles`): locks in the same order (workspace, then invitation; the other way round the two deadlock), deletes the row and records `workspace.invitation.revoked`. The link stops working.

- **Joining is always deliberate.** Registering or signing in never looks up invitations: accepting needs the token, which only the invited inbox has, and the signed-in email must match. With email verification on, registering the invited address also needs the code sent to it, so a leaked link alone can't create that account; with it off, someone with a leaked link who registers the invited address first could still join (single use, the 7-day lifetime and revoking bound that). A link never counts as verifying the address ([Email verification](#email-verification)).
- **The token** works once, for 7 days, and only its digest is stored. It is in the page's path and in `?next=` for login and register (AGENTS.md names this exception), never in an API URL. A crafted `?next=` can't make anyone join: the page joins at once only with navigation state from its own buttons, and otherwise shows **Join**.
- **The email** has a fixed subject, escapes every name ([mail.md](mail.md)), and shows the inviter's address so a misleading workspace name can be judged. With `MAIL_DRIVER=off` inviting answers 503.
- **Limits:** `invitation:per-user` (20 an hour), `invitation:per-email` (5 an hour to one address from any inviter or workspace, so the server can't flood an inbox; "This email address has had too many invitations recently."), `invitation:global` (200 an hour, 503), 50 live invitations and the member quota ([rate-limits.md](rate-limits.md)).
- Expired invitations stay listed, as Expired, for 30 days; then `Store.Cleanup` deletes them. Accepted and revoked ones are deleted; their events keep the history.

## Deleting

Plan: [plans/features/implemented/deletion.md](../../plans/features/implemented/deletion.md). What each deletion removes is in [endpoints.md](endpoints.md#deleting).

- **A project** needs only `project.destroy` (owners and admins) and the confirmation dialog: its blast radius is one project.
- **A workspace** needs `workspace.destroy` (owners only), its current name typed, and the owner's password, checked as sign-in checks one. `DeleteWorkspace` takes the workspace row lock, so a membership change in flight finishes first or finds the workspace gone. Other members' next request gets 404.
- **The account** needs the password. `DeleteAccount` locks every workspace the user owns, in `id` order, and refuses (409, `LastOwnerError`, which matches `ErrLastOwner`) while they're the only owner of any, so two co-owners deleting their accounts at once leave one owner, and two deletions can't deadlock. On success every session goes (other tabs get 401), the cookie is cleared, and the email can register again.
- **Events:** `project.deleted`, `workspace.deleted` and `auth.account_deleted` ([events.md](events.md)); each password check records `auth.password_checked` with step `destroy_workspace` or `destroy_account`.
- **Erasure:** in the same transaction, the account's email (as typed, in any case, or as its pseudonym once an event is minimised) and the `name` beside it become `"[deleted]"` in every event that outlives it: sign-in and sign-up attempts, reset requests, invitations to it, the admin door, admins' searches for exactly it. IPs stay. The operator does the same for someone who can't sign in with `admin erase` ([compliance.md](../compliance.md#erasure)).
- **Download my data** (Account & security, `POST /api/auth/export`) checks the password the same way, `auth.password_checked` with step `export_data`, then `export:per-user` (5 a day), and answers the account's data as JSON, recorded as `auth.data_exported`; never a hash, secret, token or anyone else's data ([compliance.md](../compliance.md#download-my-data)).

## Protecting a new endpoint

Register workspace routes in `workspaceRoutes` in `router/routes.go` (the `ws` group applies `RequireSession` and `RequireWorkspace`): reads with `ws.HandleFunc("GET", …)`, every mutation with `ws.Can(…, auth.<Permission>, …)`. A new permission goes in `internal/auth/permissions.go` (constant and `grants`) and in `permissions` in `apps/web/src/types/workspaces.ts`; tests check both. Add the route to the permission matrix (`workspaceRoutesMatrix`). Account routes use `RequireSession`. Read the user from `httpapi.Session(r).User.ID` and the workspace from `httpapi.Workspace(r).ID`, never from the request, and pass them into every store query.

## Not implemented

Passkeys (WebAuthn), OIDC, and changing an account's email. Two-factor authentication is authenticator-app codes and recovery codes only; email verification is a code at sign-up and, for older accounts, from Account settings. Don't describe the others as existing. Email delivery exists over SMTP, but bounces and complaints aren't handled ([mail.md](mail.md)). Before public use you'd also need the rest of observability (grouped errors and metrics; logs, request IDs and health checks exist: [observability.md](observability.md)), backups, abuse monitoring and load testing.

References: [OWASP password storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html), [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
