# Changing an account's email

Status: **implemented 2026-11-02** with the confirmed decisions below; see [How the build differs](#how-the-build-differs). Docs updated: [auth.md](../../../docs/api/auth.md), [endpoints.md](../../../docs/api/endpoints.md), [rate-limits.md](../../../docs/api/rate-limits.md), [events.md](../../../docs/api/events.md), [mail.md](../../../docs/api/mail.md), [database.md](../../../docs/api/database.md), [testing.md](../../../docs/api/testing.md), [README.md](../../../docs/api/README.md), [admin.md](../../../docs/api/admin.md), [compliance.md](../../../docs/compliance.md) (data map, former-address erasure, export exclusion, privacy policy), [ui/api.md](../../../docs/ui/api.md), [ui/pages.md](../../../docs/ui/pages.md), [ui/design.md](../../../docs/ui/design.md), [ui/forms-and-actions.md](../../../docs/ui/forms-and-actions.md), [ui/testing.md](../../../docs/ui/testing.md), [checks.md](../../../docs/checks.md), [testing.md](../../../docs/testing.md), and AGENTS.md (the "Do not claim changing an account's email" line replaced by the email-change invariants).

Builds on [email verification](email-verification.md), [password changes with MFA](password-change-mfa.md), [email blocks](email-blocks.md) and [compliance](admin/compliance.md). Names follow [naming.md](../../../docs/naming.md); the resource names and narrow convention-check registrations below are confirmed.

## Why

The account's email is its sign-in and recovery address, but it cannot currently change. Correcting a typo or leaving an employer means deleting the account and signing up again, losing its identity and memberships. The required-verification page also strands someone who knows their credentials but can no longer receive mail at their old address.

Keep the same user ID, password, MFA, memberships, ownership and policy acceptance. Confirm account access, verify the replacement inbox when `EMAIL_VERIFICATION=on`, invalidate credentials associated with the previous address, and notify the owner when mail is configured.

## Investigation

These are findings from the current implementation, rather than assumptions about a future flow:

| Area | Current implementation | Consequence for this feature |
| --- | --- | --- |
| Profile | `endpoints/account/me.go` accepts only a name; `store/users.go`'s `UpdateProfile` updates only that column; `pages/app/settings.tsx` locks and blurs the email | A dedicated security flow, separate from Save profile |
| Address validation | Addresses are trimmed and lowercased; `users.email` has a unique constraint | Reuse the existing parser and canonicalisation; no provider-specific dot or plus-address rules |
| Email codes | `store/email_verifications.go` verifies the account's current address; codes are HMACs under `APP_KEY`, 15 minutes, five tries | Reuse the primitives and code field, but use a separate pending resource bound to the replacement address and initiating session |
| Async mail | `mail/registration.go` correlates sign-up deliveries with the latest requested event ID; ordinary signed-in verification correlates only by user | Use explicit request correlation so a delayed delivery cannot overwrite a replacement request |
| Password reset | `IssuePasswordReset` selects by email and then upserts by user ID without an account lock | Deleting existing links alone is insufficient: issuance racing the change could otherwise recreate a working link sent to the old inbox |
| Invitation acceptance | `AcceptInvitation` compares the invitation address with the `User` passed from middleware, after taking the workspace lock | Re-read and lock the account before the workspace: an in-flight request must not accept using a stale address |
| Sessions | App and admin sessions are separate; password changes revoke both and then start a new app session | End both session types and pending sign-ins; keep this browser signed in with a rotated app session |
| Verification gate | Exactly five routes allow an unverified account when verification is required | Helping someone without the old inbox requires a deliberate, tested expansion of that allowlist |
| Blocks | Account creation and blocking share `database.LockEmail` | Claiming the replacement address must use the same lock and recheck the block list |
| Erasure | `eraseAccount` redacts only the current email from surviving events | Track former addresses for erasure; the audit trail alone is not a durable address registry |
| UI reachability | Account settings requires a workspace; onboarding and the verification gate have separate layouts | Make the flow reachable from all three places |

Relevant current standards: [auth](../../../docs/api/auth.md), [endpoints](../../../docs/api/endpoints.md), [database](../../../docs/api/database.md), [mail](../../../docs/api/mail.md), [rate limits](../../../docs/api/rate-limits.md), [events](../../../docs/api/events.md), [compliance](../../../docs/compliance.md), [UI API](../../../docs/ui/api.md), [pages](../../../docs/ui/pages.md) and [forms and actions](../../../docs/ui/forms-and-actions.md).

## Proposed flow

### Request and confirm

The numbered code flow below applies with `EMAIL_VERIFICATION=on`. With verification off, use the direct flow immediately below it; fresh account proof is required in both modes.

1. Open **Change email address**. Enter the new email and confirm account access: **current password with MFA off; an authenticator or recovery code with MFA on**, matching the existing password-change policy. The server chooses from locked database state, never from a client flag.
2. The API creates or replaces this account's pending change, tied to the initiating app session. It leaves `users.email` and its verification status untouched and answers 202 with the pending email and expiry.
3. A listener generates a six-digit code, stores only its HMAC and sends it to the new inbox. This message explicitly says it authorises changing an existing account's sign-in email; use a dedicated template rather than the generic sign-up wording. Do not include the account's name, old address or workspace information.
4. Type the emailed code into the initiating browser. The request takes only the code; the destination comes from the pending record, never from this request's body. The code is valid for 15 minutes from the request, with five attempts. A right code finishes only this pending change.
5. In one transaction, update the address and set `email_verified_at` to now, record the previous address for erasure, delete the pending change and obsolete credentials, and record `auth.email_changed` with mail deliveries queued in the same transaction.
6. Rotate this browser's app session and CSRF token. Every previous app/admin session is revoked. Renew the client session, invalidate affected cached account/member data, and show **Email address changed. Other sessions were signed out.** If creating the replacement session fails after commit, return to sign-in: the email change remains committed and the new address signs in.
7. Two independent listeners send completion notices to the old and new inboxes using the addresses captured by the committed event, never by looking up the user's current address later. One failing delivery must not cause the other to resend. The old-inbox notice does not reveal the new address; neither message contains a reversal token.

**Confirmed old-inbox policy:** notify it after completion when mail is configured, without requiring approval from it. This supports people who lost that inbox. With MFA off, a stolen session plus the password can change the email, just as it can currently change the password; the new inbox alone or the session alone cannot. With MFA on, the password alone is insufficient. Neither notice contains a reversal link.

### Verification disabled

- `EMAIL_VERIFICATION=off` skips replacement-inbox proof, matching registration in that mode. POST takes the destination and fresh password/MFA proof, checks the same request/completion limits, and completes the change directly with a 200 session response. It creates no pending code request, and works with `MAIL_DRIVER=off`.
- Set `email_verified_at` to null for the replacement: proof of the previous inbox does not verify a different address. The same transaction, address lock, uniqueness/block checks, history recording, credential invalidation and session rotation apply as in code completion.
- An unavailable destination returns the same generic 409 for taken and blocked addresses. This mode accepts an availability signal, as registration with verification off already does; it must not distinguish blocking or expose the owning account.
- After successful fresh account proof and the request limits, record `auth.email_change.requested` with the verification mode, including when an unavailable destination is refused. This allows the async listener to notify an existing destination account when mail is configured. Commit that event on a conflict without consuming a recovery code, replacing an existing pending request or changing the account; a refused limit queues nothing.
- Completion notices are queued only when mail is configured. Mail delivery is not a prerequisite for success. GET/DELETE can still inspect/cancel an earlier pending request, but PATCH must not turn it into a bypass: with verification now off, return 410 and require a fresh direct POST.

### Resume, resend and cancel

- Reload or reopen in the same session: GET returns the pending email, fixed expiry and `sending`/`code` state. It never exposes a digest, internal ID or another session's destination. Another session sees no pending change through GET/PATCH; an explicit new request with a fresh factor may replace the account's pending change.
- Before starting another request in the initiating session, show **You already have an active change request** with **Continue** and **Cancel**. Continue resumes code entry; Cancel explicitly deletes the request. One active request is a simultaneous-row limit, not a one-request-per-15-minutes cooldown. Replacements/resends are allowed within the hourly limits and require fresh account proof.
- **Send a new code** restarts POST with the same destination and a fresh password/MFA confirmation. It replaces the request ID, clears the old code and starts another 15 minutes. No generic reusable step-up window is introduced. Factor values are cleared after each request and on close, never persisted in the browser.
- **Cancel change** deletes the pending record. Closing the dialog merely dismisses it, so a phone switching apps or reloading can resume. Explain this distinction in the pending state.
- A code not issued yet is not a guess: answer 409 with `pending: true`, without spending a code attempt. A wrong issued code is 422 on `code`; the fifth wrong ends the request with 410. Expired, cancelled, replaced or missing changes are 410 on PATCH. GET returns `{pending: null}` when none is live.
- POST replaces only after successful fresh authentication and all request limits; a refused new request must leave the existing one usable.

### Address availability and privacy

- Reject the unchanged canonical address with 422 on `email`; it is the account's own value, not another account's status.
- With verification on, do not look up destination account availability in POST or GET. New, taken and blocked addresses receive the same response shape and execute the same destination-independent request flow. Limits count all addresses alike in both modes.
- The listener sends no code to an already-registered or blocked destination; with verification on, store a random code's HMAC as with sign-up. An already-registered destination instead receives the dedicated **Email change requested** notice below, never a usable code. A blocked destination gets no email. The UI says **If this address is available and can receive our email, a code will arrive**, and offers the same availability/block-link troubleshooting to everyone.
- If the address becomes taken or blocked after a code was sent, a right code gets a generic 409 **This email address cannot be used. Choose another address.** End that pending change without altering the account. This is reachable only with inbox proof; do not expose another account's details or distinguish blocking.
- The pending request does not reserve an address. The unique constraint and final email lock decide who claims it. Never merge accounts.

### Already-registered destination notice

- Send a dedicated **Email change requested** email from the request listener, not the handler. It explains that someone requested changing another account's sign-in email to this address, but this address already belongs to an account and the request cannot complete while it remains in use.
- Suggested body: **A request was made to change another account's email address to this one. This email address is already associated with an account, so the change cannot be completed. If you made this request and want to use this address for the other account, you must first free it by changing the existing account's email address or deleting that account. Deleting an account permanently removes its data; accounts and their data will not be merged. Once the address is available, start a new email-change request. If you did not make this request, you can ignore this email. This request has not changed your account or given anyone access to it.**
- No code, approval, automatic deletion, reversal link or information about the requesting account. Use only fixed trusted sign-in/account-settings links. Do not claim an attack occurred, that the account cannot be compromised for another reason, or that deleting an account automatically completes the waiting request.
- Classify it as asked-for informational mail, like the existing account-exists sign-up notice, rather than an unlimited security warning. The ordinary destination/request/global limits apply before queuing; share the existing `account-exists:per-account` five-per-day listener cap with sign-up notices, counted only on a delivery's first attempt. This remains the same privacy-motivated exception: an account-only limit in the API would reveal availability. It must never suppress real completion security notices or codes.
- Recheck destination ownership before sending: do not reroute a notice to an account that acquired the address after the request. Use the account's creation/ownership timestamps, respect blocks and erasure, and do not expose destination-account identity in the initiating account's responses or data export. With verification on, also require the original pending request to remain live and correlated. With verification off, only the informational branch applies; no code is issued.

## API and naming

Singleton controller `account.EmailChange` in `endpoints/account/email_change.go`; registry stays in `httpapi/router/routes.go`.

| Method | Path | Input | Success |
| --- | --- | --- | --- |
| GET | `/api/auth/email-change` | Session | 200 `{pending: null}` or `{pending: {email, expiresAt, state}}` for this session |
| POST | `/api/auth/email-change` | `{email, currentPassword}` or `{email, code}` with MFA on | Verification on: 202 `{pending: {email, expiresAt, state: "sending"}}`; a new request/resend. Verification off: 200 complete session response after direct completion |
| PATCH | `/api/auth/email-change` | `{code}` (the emailed code, not a second factor) | 200 with the ordinary complete session response and a rotated cookie |
| DELETE | `/api/auth/email-change` | No body | 204; cancel this session's pending change, idempotent |

All four need an app session, exact Origin/content-type and CSRF on mutations, `no-store`, and the ordinary user budgets. POST additionally uses `LimitAuth` for the existing IP bound and hashing concurrency; no CAPTCHA. Bad current password/MFA is 422 on its field, not 401. Unknown input fields are 400. A genuine expired/revoked session is 401. Limits are 429/503 with `Retry-After`.

Register these routes regardless of `EMAIL_VERIFICATION`. The server's configuration selects code verification or direct completion; never accept a client-selected bypass. Session responses expose the verification requirement so the UI selects the right steps, without exposing SMTP details. `MAIL_DRIVER=off` requires verification off as today and does not disable POST; GET/DELETE still permit inspecting/cancelling an earlier request. PATCH only completes code requests while verification is on; otherwise it returns 410 as described above. No separate mail-availability capability is needed to disable email change.

**Confirmed gate change:** explicitly `AllowUnverified` on these four routes, so there are nine allowed routes, with an exact-set test replacing the five-route assertion. The verification page offers the flow; with verification on, a successful change verifies the replacement and returns through the existing safe `next` handling. It grants no workspace or other API access before completion. With verification off, the ordinary verification gate is inactive and direct completion leaves the address unverified.

Typed client: `api.auth().emailChange().show()`, `.store(input)`, `.update(code)`, `.destroy()` and matching typed paths. Store candidates use existing verbs: `FindEmailChange`, `CreateEmailChange`, `IssueEmailChangeCode`, `UpdateEmailChange`, `DeleteEmailChange`. The confirmed event resource `auth.email_change` corresponds to the new stored resource and needs an explicit naming-check entry with that reason; do not relax the checker.

## Data changes

Two create-migration pairs, numbered after the current migrations when implementation starts. Each table gets its own factory, `store.DataTables` entry and [data-map](../../../docs/compliance.md#data-map) row. Neither is workspace-scoped or publicly addressable, so neither needs a public ID.

The squashed users migration also gains `email_version bigint NOT NULL DEFAULT 0 CHECK (email_version >= 0)` and nullable `email_changed_at`. Increment the version and set the timestamp from the database clock after acquiring the user lock on every successful change. The version is an internal concurrency guard, not an API field; capture it with the email/password when sign-in starts and compare it again before creating sessions/challenges. The timestamp lets anonymous reset events be rejected when they predate the present address ownership, without adding an account lookup to reset requests. Update the user factory and explicit store projections in the same change.

### `email_changes`

One pending change per user (`user_id` primary key, cascade on user deletion):

- `session_hash`: foreign key to the initiating app session's `token_hash`, cascade on session deletion. It is an existing stored digest, never a new browser token. Validate that session and user belong together when creating/using the row.
- `previous_email`, `email`, `email_version`: canonical current and proposed addresses and the internal version at the fresh authentication check. Completion requires the locked user's original email and version still to match.
- `request_id`: unique `evt_` ID of the latest `auth.email_change.requested` event, as for pending registrations; never a request's `req_` ID.
- `code_hash`: nullable only while awaiting issuance; HMAC using the existing emailed-code key and constant-time comparison.
- `attempts`, `created_at`, `expires_at`: checks for zero through four retained failed attempts, expiry after creation and a maximum 15-minute lifetime; indexes on expiry and destination for bounded cleanup/integrity queries.

No password or MFA material is stored here. The successful POST is the account-access confirmation for this one destination, session and bounded lifetime; it cannot authorise other actions.

### `user_email_history`

An erasure registry, not a login-alias list: internal primary key, `user_id` (cascade), canonical former `email`, and `released_at`. Index by user for erasure/export and by address/time for retention. Store only addresses actually relinquished by a successful change, never unsuccessful candidate destinations. Repeated ownership periods may have separate rows.

- Purpose: find raw and pseudonymised address-only references that account deletion currently finds by the current email. Keeping only an HMAC here would not support the existing indexed lookup of raw addresses when event minimisation has not run.
- Keep a history row only while retained events at or before its release still reference that address (including pending deliveries and the change's own audit event). The ordinary maximum event retention is a year; pending deliveries or overdue pruning may extend it. Cleanup must check for remaining references rather than deleting history blindly after a year. Batch the work and use the event lookup indexes.
- This deliberately retains a former raw address beyond the events' 30-day minimisation for a separate erasure purpose; this data-minimisation tradeoff is confirmed in decision 7. It is never an authentication credential, availability reservation, forwarding address or basis for admin lookup of an account.
- On erasure, use the current address and all retained history rows before deleting them. Former-address redaction is bounded by `released_at`, so erasing an old owner does not redact later events belonging to a new owner of that address. Account-linked events still cascade regardless of address. Later address-only searches cannot reliably be attributed to the former owner; document that limit.
- Explicit reuse example: account A releases `admin@company.com` when changing to `admin@new-company.com`; account B later registers `admin@company.com`. Deleting A must leave B, its sessions, memberships and all of B's later raw/pseudonymised events and address-only records unchanged. Never cascade by address or apply an unbounded former-address match. Protect events explicitly linked to a different user even when recorded before the release boundary (for example, a request another user made to claim this then-taken address); redact matching personal-data fields without deleting that other user's event. Test A's erasure while B remains active.
- Export the account's own pending destination/expiry and retained former addresses/release times through explicit export structs, without session/code/request digests or another account's history. Do not start exporting anonymous events by address.
- Exclude `auth.email_change.sent` from the initiating account's export in both modes: its `existingAccount` outcome and whether an informational send was suppressed can disclose destination availability. Keep these delivery outcomes in the admin-only audit trail. Requested/checked/completed events remain exportable with their explicit safe fields; never expose destination-account identity.

The rate limits below bound request creation and successful history growth. Apply expiry in every query, not just cleanup. Account deletion cascades both tables. No demo seed is necessary; factories cover pending, expired, issued and historical states.

## Transactions and interactions

### Locking and completion

- Mutations use the account row first. Where needed, take session locks before the pending row (session deletion cascades into it), the authenticator row before any MFA challenge, and workspace locks only after the account. Document and test the combined order rather than copying the old verification transaction blindly.
- A request rechecks its initiating session is still live and belongs to the locked account. This matters when middleware passed before sign-out, suspension or a concurrent credential change. POST validates the fresh factor against locked state. Its MFA consumption and pending-row replacement commit together; failed validation/limits never consume it.
- Completion locks the user, originating session and pending change; compares the code; then takes `database.LockEmail` on the destination, rechecks uniqueness and blocking, and writes the address. Do not lock a second user's row to check availability: simultaneous A→B/B→A attempts must fail cleanly rather than deadlock. A taken address is never swapped in place.
- Reconcile registration's lock order: completion currently locks pending sign-ups before the email advisory lock. Email change must not hold that email lock and then delete those sign-up rows. Leave them to expire; their existing completion checks refuse an address that is now taken. Test this race explicitly.
- A wrong emailed code increments attempts and records its check in a committed transaction. Do not return an error that rolls that increment back. A successful code followed by a now-unavailable address commits the ended pending state and the checked event, without an email-changed event.
- Successful completion removes reset links, old email-verification codes, MFA login/admin challenges and any unconfirmed MFA setup, revokes every app and admin session, and records the change. Confirmed MFA and remaining recovery codes survive. The initiating pending row may be deleted explicitly before revoking its session to keep the cascade order clear.
- A password change/reset, MFA enable/disable/reset or recovery-code replacement invalidates pending email changes. Session revocation also cancels them through the foreign key. A suspended/deleted account cannot issue, confirm or receive a newly issued change code.

### Existing flows that need adjustment

1. **Reset issuance:** make `IssuePasswordReset` transactional under the user lock; recheck the requested email, suspension and that the request event was not made before `email_changed_at` (null means the original address). A reset queued before an address moved away and back must not issue against its later ownership. Serialize issuance with completion, so a token mailed to the former address is either deleted by completion or never issued. SMTP may still deliver a now-useless message after commit; do not hold DB locks across sending.
2. **Existing-email verification:** correlate requested codes with the actual email/ownership generation at request time, lock the user while issuing, and refuse stale events. A code for the old address can never verify a replacement. Completion deletes the old row, and the replacement is already verified.
3. **Invitation acceptance:** lock/re-read the accepting account before the workspace and invitation, then compare the current email. Keep existing memberships and invitation rows unchanged; no automatic joins or retargeting. Old-address invitations no longer match, new-address invitations require their own deliberate acceptance.
4. **Sign-in in flight:** app/admin login can load a user/hash before completion and create a session afterwards; MFA challenge creation has a similar window. Carry the expected `email_version` through session/challenge creation and recheck under the user lock. Persist that version on MFA challenges and recheck at completion too: a challenge created before the change must never create a session after it. A mismatch ends the sign-in with the ordinary expired-sign-in answer. Update the squashed challenge migration/factory. Do not silently retry a stale authentication against the new generation.
5. **Mail retries:** new code issuance matches the pending row's latest event ID, session, original address and fixed expiry. A retry replaces only that request's code and never extends its deadline. Old deliveries cannot recreate a cancelled/replaced/completed request. Notification retries use immutable committed destinations, respect the block list and erasure, and skip an account erased before sending. Do not reroute an old notice to the current address.
6. **Admin identity:** admin grants/roles and alert preferences stay attached to user ID. Self-service uses the app session and MFA like any other account; all admin sessions end and future admin sign-in uses the new email. Alert listeners resolve the current verified account address. Changing somebody else's email from the dashboard or CLI is out of scope.

## Events and mail

| Event | Proposed payload | Purpose |
| --- | --- | --- |
| `auth.email_change.requested` | `user`, `email` (destination), `verificationRequired` | Queues `mail.email_change` when mail is configured; a pending row correlates its `evt_` ID in code mode, while direct mode permits only the registered-destination notice |
| `auth.email_change.sent` | `user`, `request`, `existingAccount` | Code or registered-destination notice sent, no code/digest or destination-account identity |
| `auth.email_change.checked` | `user`, `ok`, `attempt` | Each issued emailed code checked |
| `auth.email_change.cancelled` | `user` | Explicit cancellation, only when a row existed |
| `auth.email_changed` | `user`, `email` (new), `previousEmail`, `verifiedBy`: `code` / `none`, app/admin session counts | Committed account change; queues separate old/new notice listeners when mail is configured |

All are account-scoped (`user_id`) and audit events with one-year retention and payload-free audit log lines. Authentication checks reuse `auth.password_checked`/`auth.mfa.checked` with a proposed `email_change` step; the latter records recovery-code use normally. Credential checks, wrong codes and cancelled requests must be distinguishable without storing any secret.

The dedicated code template has a fixed subject, both escaped HTML and plain-text parts, and only the code as variable content. It is asked-for mail, limited on POST, and follows sign-up codes' non-blockable policy. Completion notices are security mail: no per-recipient send cap. Every template goes through `mailer.Send` and honours blocks. Notices describe sign-in/recovery using fixed trusted links from `APP_ORIGIN`; the old notice explains that unexpected changes need support, without promising an unbuilt one-click recovery flow.

The already-registered destination template also has escaped HTML/plain-text parts and a fixed subject, **Email change requested**. It is non-blockable while its recipient has an account, like the sign-up account-exists template, and shares that informational notice's listener cap; extend the documented use of the existing exception without adding a new unbounded mail path. Never pass the requester’s name, old address, workspace or other identifying data into it.

Adding `previousEmail` requires explicit support in event minimisation, indexed erasure, admin email filtering/recipient aggregation and tests. It must not remain readable after 30-day event minimisation just because it is not named `email`. Recorded notice destinations are personal data; deletion must remove them before a listener can use them.

## Rate limits

Proposed initial bounds, all named and tested; no new challenge action:

| Limit | Bound | Where |
| --- | --- | --- |
| Existing user read/write budgets | Unchanged | All four endpoints |
| Existing `auth:per-ip`, hashing slots | Unchanged | POST through `LimitAuth` |
| Existing `auth:per-account` | 10 / 15 minutes | POST password confirmation, using the current account email |
| Existing `mfa:per-account` | 5 / 15 minutes | POST factor confirmation with MFA on, keyed by user ID |
| `email-change:per-user` | 6 / hour | Every POST, including resends/replacements across sessions |
| `email-change:per-email` | 3 / hour | Every destination alike, before queuing mail |
| `email-change:global` | 200 / hour | After subject limits; block-only, 503 |
| Existing `email-code:per-email` | 10 / day | PATCH, using the stored destination; shared with the other emailed-code flows |
| `email-change-complete:per-user` | 1 / day | After a right issued email code, or fresh account proof in direct mode, before completion; bounds successful churn/history growth |

Spend feature and factor budgets before side effects, and check the completion cap before committing the address. Refused limits leave the pending request intact and use the existing `rate_limit.reached` mechanism. Security notices are not held behind a recipient budget. DELETE remains under the ordinary write budget; logout/revoking sessions/password recovery remain the existing unmetered ways to cancel a stolen session and its pending change.

The existing `account-exists:per-account` five-per-day listener allowance is shared across sign-up account-exists notices and email-change registered-destination notices. It never applies to replacement codes or completed-change security notices. Code-check limits apply only in verification mode; request, destination, global and completion bounds remain in force in direct mode even when mail is off.

## Web app

- Account settings: keep the current email read-only and blurred, but replace **Your email can't be changed** with **Use Change email address to update this address**. Add a `TextButton` in its description, following the existing action-on-a-field pattern. Profile Save continues to save only the name.
- Put a shared `ChangeEmailDialog` in `components/app/account/change-email-dialog.tsx`: it is reused by settings, onboarding's account menu, and the required-verification page. Page-specific explanations remain in their pages. No new page URL, token cookie, localStorage record or dependency is needed.
- Dialog steps with verification on: destination → current password or MFA → emailed code. With verification off: destination → current password or MFA → completed change, without an emailed-code step. Only POST checks the factor, and its value is cleared immediately afterwards. PATCH checks only the emailed code. Resend returns to fresh factor confirmation. Pending state loads through the typed GET with a skeleton, empty state and Retry on failure; never replace available pending data with an empty state after a refresh error. A live request shows **You already have an active change request** with **Continue** and **Cancel** before starting another request.
- Explain that the current address stays in use until completion, the old inbox receives a notice when mail is configured and other sessions end. In direct mode, make clear that submitting fresh account proof completes the change immediately. Show the newly typed destination for confirmation; do not reveal the old blurred address inside the dialog by default.
- Use existing `Dialog`, `PasswordField`, `AppOrRecoveryCode`, `CodeField`, `Notice`, `useAction`, `Button loading` and `DialogActions`; a code-only step may auto-submit. Field errors remain on their fields, 410 returns to the request step, 409 availability asks for another destination, and rate/network failures retain the pending state. Disable duplicate submissions, retain keyboard/focus/reduced-motion behaviour, and clear entered secrets on close.
- The verification page and onboarding can mount the same dialog without workspace data; completion renews their session and navigates through their normal loader/return flow. Follow their existing success-message convention where no Toaster is mounted.
- Revalidate pending state and cached member/account representations after completion. Other browsers discover revocation through their next API request; do not promise immediate cross-browser push updates.

## Security risks and how they're avoided

| Risk | Mitigation / accepted boundary |
| --- | --- |
| Stolen session changes the recovery address | Fresh password or MFA confirmation; email proof alone cannot authorise an account change |
| Password alone bypasses configured MFA | Locked state requires the second factor, as for password changes; recovery codes are single-use |
| A code from somebody else's request changes the wrong account | Separate pending row bound to account, session, destination and latest request; same-session confirmation only |
| Code guessing or replay | HMAC, constant-time compare, 15-minute deadline, five tries, shared per-email daily budget, row locks and deletion on use |
| Probing whether an address has an account or is blocked | With verification on, no destination lookup during request/status, same responses/query shape and no usable code for unavailable destinations; an existing-account notice goes only to its inbox. With verification off, accept registration's availability signal but use the same generic conflict for taken/blocked addresses |
| A former inbox can still reset or verify the account | Serialized issuance, ownership-generation checks, removal of reset/verification material and pending sign-ins |
| Old sessions regain access during the change | Transactional revocation plus expected-generation checks at session/challenge creation |
| Recycled email inherits workspace access | Memberships remain keyed by user ID; no aliases, no merges, invitation acceptance rechecks the current user and still requires its token |
| Blocking races claiming an address | Shared email advisory lock and final uniqueness/block recheck |
| Queued work resurrects old requests or sends to a later address | Latest-event correlation, fixed expiry, session binding and immutable notification destinations |
| Former addresses escape erasure/minimisation | History registry, explicit previous-address JSON handling and time-bounded redaction for reused addresses |
| Owner loses the old inbox | No old-inbox approval; fresh account authentication plus new-inbox proof when verification is on. Losing both credentials and inbox still needs a separately designed recovery procedure |
| Verification off incorrectly marks an unproven inbox verified | Direct completion sets the replacement's verification timestamp to null; configuration selects the mode, not a browser flag |
| Compromised session plus the required factor completes the change | Accepted account-access boundary, matching password change; old-inbox warning does not itself undo a change |

## Tests

Use the existing [Go integration](../../../docs/api/testing.md) and [Vitest/Playwright](../../../docs/ui/testing.md) patterns, with factories and real PostgreSQL; prove concurrency with barriers/held locks, not timing sleeps.

- **API boundaries:** all four routes require app sessions, CSRF where applicable, exact Origin, bounded JSON and known fields; an admin-only session cannot use them. Invalid address, unchanged canonical address, wrong/missing password/factor; MFA state changed after middleware; no numeric IDs or secrets in responses/events/logs.
- **Main persistence:** request leaves the old email active; correct code changes only address/verification, preserves identity/workspaces/MFA/policies, rotates cookie/CSRF and ends every prior app/admin session. New-email login/reset works; old-email login fails. No implicit workspace join.
- **Codes:** not issued yet, wrong, fifth wrong, expiry, cancelled, superseded, double completion, replay, code from another account/session, guessed candidate destination, older listener out of order and retry without extending expiry. Factor replay/recovery use and transactional rollback on failed POST.
- **Privacy:** with verification on, compare POST/GET and wrong-code responses/query shapes for available, taken and blocked destinations; listener sends the informational notice rather than a code for a registered address and nothing for a blocked one; no unrelated account data or availability difference through status. A right code after availability changes gives the generic conflict and changes nothing. With verification off, taken/blocked share a generic conflict, and the request cannot identify the other account or bypass fresh authentication.
- **Limits/config:** each limit with `Retry-After`, one reached event, no queued mail or lost pending row on refusal; verification off completes directly, leaves the replacement unverified and works with mail off; no notification listener when mail is off; a previously pending request cannot bypass fresh proof after switching modes; exact nine-route unverified allowlist, with unrelated routes still gated. Prove the shared five-per-day account-exists listener allowance across both notice types, with retries counted once and no effect on real security notices/codes.
- **Races:** two users claiming one address, block versus change, register versus change, resend/cancel versus issue/complete, sign-out/suspension/deletion versus request/complete, password/MFA security changes versus pending changes; old reset issuance in both lock orders; old queued reset after A→B→A; invitation acceptance versus email change; password login and app/admin MFA steps started before completion. Assert no deadlock and no stale credential remaining usable.
- **Mail:** code, both completion notices and the registered-destination notice render safely with fixed subjects; no requesting-account information in destination code/informational mail, no code or automatic action in the informational notice, accurate account-deletion/no-merge guidance and no unsupported claims of an attack or complete account safety. Destinations are committed snapshots, old notice hides the new address, independent notice retries, blocked destinations, erased accounts and scrubbed delivery errors. Late informational mail cannot reach a later owner or resurrect a request; a late invalid reset email is harmless, not a live credential.
- **Compliance:** minimising both `email` and `previousEmail`; erasing current/former raw/pseudonymised addresses with index-backed queries, including A→B→C and a later reuse of A by somebody else. Explicitly delete the former owner while the later owner remains active and assert all of the later owner's events, address-only references, account, sessions and memberships survive. Preserve events linked to another user while redacting a former owner's address field where applicable. History survives while references/pending deliveries remain, then cleanup removes it. Export only the account's own explicit non-secret fields; pending tables cascade on session/account deletion. Table factory/data-map/registry checks.
- **UI:** request/confirm/resend/cancel/resume, active-request Continue/Cancel prompt, wrong-factor and wrong-email-code errors, pending/duplicate prevention, 410/409/429/503/network failures, session renewal/cache revalidation, fresh dialog state, and reachable entry points with/without a workspace and at the verification gate. Verification-off flow skips the code step and handles direct completion with/without mail.
- **Playwright:** real Mailpit code and old/new notices, old/new sign-in, second browser revoked, one MFA/recovery-code flow, and required-verification recovery without the old inbox. Save desktop/mobile screenshots of the actual flow; check keyboard behaviour and CSP.

Before shipping: `make test`, `make check`, `make test-e2e E2E_PORT=auto`, `make build` and the auth/dependency audits required by AGENTS.md. Check other test processes and machine resources first; run heavy suites sequentially in the background.

## Implementation order

1. Apply the confirmed decisions below and map the lock order across password login, MFA challenges and async issuance. Add the proposed internal `email_version` and ownership timestamp to the squashed migrations/factories, and ensure every session/challenge creation path checks the expected generation, including registration, password change and password reset.
2. Add pending/history migrations, models, factories, cleanup/registry/data-map entries and transaction-level store tests. Add typed events and privacy handling together with their fields.
3. Implement reset, verification, sign-in and invitation race protections plus cancellation on credential changes. Cover the boundaries before exposing completion.
4. Add controller, limits, exact allowlist/session-verification-mode changes, listeners and templates (including the registered-destination notice and shared informational cap); integration tests prove code and direct flows through the real router/worker.
5. Add typed client/shared dialog and the three entry points; Vitest and real-mail end-to-end tests, including the session rotation and gate return.
6. Update the docs below, run required checks, record build differences, mark implemented with the date and move this plan under `implemented/`, fixing links.

## Confirmed decisions

1. **Old inbox: notify, no approval.** Confirmed. Notify after completion when mail is configured; no automatic rollback link.
2. **Fresh account proof: password without MFA, factor instead of password with MFA.** Confirmed, matching password changes and accepting single-use recovery codes. An existing session alone cannot authorise the change.
3. **Allow at the required-verification gate and before creating a workspace.** Confirmed: four explicitly allowed routes, reachable from gate/onboarding/settings; other gated APIs remain inaccessible.
4. **Respect `EMAIL_VERIFICATION`.** User changed the original recommendation: verification on requires the new-inbox code; verification off completes after fresh account proof without SMTP, leaves the address unverified and accepts the same availability signal as verification-off registration. Notifications depend on mail being configured, not success of the address change.
5. **Session-bound, 15-minute request, five tries; resend needs fresh account proof.** Confirmed for code mode: one active pending change per account, not a 15-minute creation cooldown. Show **You already have an active change request** with **Continue** and **Cancel**. No reusable elevation or new cookie. Direct mode needs no waiting/code stage.
6. **Revoke all app/admin sessions and pending sign-ins, then rotate this browser's app session.** Confirmed, preserving confirmed MFA and remaining recovery codes; account identity and memberships survive.
7. **Former-address registry for erasure, with reference-driven retention and a release-time boundary.** Confirmed subject to the clarified guarantee: deleting a former owner must not erase a later owner's events or account data. Retain former raw addresses solely while indexed retained references need them; address-field redaction does not delete another user's event.
8. **Initial limits:** six requests/hour per account, three/hour per destination, 200/hour globally, one completed change/day per account, existing code/factor limits shared. Confirmed; user-facing refusals explain the wait. Registered-destination informational mail uses request limits and the shared existing five-per-day account-exists listener cap.
9. **Self-service for admins through the app, with MFA and admin-session revocation; no staff override.** Confirmed: grants remain attached to the same account ID. No transfer to an existing account, dashboard override or CLI email-change action.
10. **Resource/event/client names, internal `users.email_version`, and `email_changed_at` ownership timestamp.** Confirmed as above; explicitly register the stored event resource/new singleton path in convention checks where necessary, with reasons, without weakening any rule. The counter prevents pre-change sign-ins from creating post-change sessions; the timestamp rejects delayed work from an earlier ownership period, including A→B→A.

**Additional confirmed direction:** notify an already-registered destination with **Email change requested**, explaining that the address must first be freed and the request retried. Do not send it a code, merge accounts, delete anything automatically, or identify the requesting account. The wording distinguishes the harmless refused request from an unsupported guarantee about all account security.

## How the build differs

- **Completion limit is reserved in the completing transaction** (`store.EmailChangeCompletionLimit`, `reserveEmailChangeCompletion`), not counted beforehand by the controller, so a refused or rolled-back change never spends the daily allowance, and a single database connection can't deadlock on a nested checkout (`TestEmailChangeWithSingleConnection`).
- **The pending request's deadline is fixed on the database clock** when it is created; a delivery retry never extends it.
- **The event `auth.email_change.sent` carries a `kind`** (`code`, `existing_account`, `previous_notice`, `current_notice`). The two completion notices are retried independently, use the address snapshot committed with the change, and are left out of the admin "requested vs sent" counts and of the account's data export. Admin mail gained the kinds `email_change_code`, `email_change_exists`, `email_changed_previous` and `email_changed_current`.
- **A queued password reset or verification mail is checked against the ownership timestamp** under the account lock, so work queued for an earlier owner of an address never reaches a later one (A to B to A).
- **A failed code attempt of the new kind `email_change_code`** is its own failure kind in the admin charts, grouped with sign-up codes as "Email codes" in the web chart.
- **An MFA change between the middleware and the lock** is caught by `ErrEmailChangeProofChanged`; a session that can't be started after the commit answers 401 and sends the user to sign in with the new address.
- **Credential changes cancel a pending request:** changing the password, turning MFA off or renewing recovery codes removes it.
- **The former-address cleanup sweep is not capped per run.** A fixed cap would let rows kept by retained references starve later unused rows; the table is bounded by the retention rules, and each batch is small.
- **No "active request" screen; a new address replaces the old request silently.** The plan's decision 5 showed **You already have an active change request** with Continue and Cancel before starting another. Replaced by a predictable Save: it always opens at account access and confirming it replaces the pending request (the API already overwrites it, only after proof is accepted; a refused attempt leaves it usable, and `TestEmailChangeNewAddressReplacesThePendingRequest` covers this). The only trace of a waiting request is the settings field's description, **You have an email change request. Continue**, which resumes it at its code. Onboarding's menu and the gate resume it the same way. No cancel event is recorded for a replaced request; its replacement's `auth.email_change.requested` is the record.
- **Settings has no separate "Change email address" button.** The email field is editable and `guarded` (a new `Field` state: a shield with the tooltip "Protected" on hover and focus, and nothing is refused on typing). Save saves a changed name first and at once, then opens the dialog for a changed email at confirming account access; a failed name save opens no dialog. Onboarding's menu and the verification gate keep their entries and the destination step.
- **The dialog has no description under its title, and Cancel change ends on its own screen** (**Your change has been cancelled**, "Your email address was not changed.", a **Close** button only) instead of returning to the destination step. The "current address stays in use / other sessions signed out / old inbox notified" explanation from the plan's UI section is no longer shown before the change; the success screen states the session and notice effects afterwards.
- **Completion keeps the dialog open on a success screen** (title "Email address changed", the new address, **Done**). Done, Escape and the backdrop run `onChanged(newEmail)` and then close, so the gate's return through `next` and settings' field reset happen only after it is dismissed; the success toasts were dropped.
- **UI:** the workflow lives in `use-change-email.ts` and the rendering in `change-email-dialog.tsx`; the settings link is disabled while the profile form is saving so an unsaved name is never lost.

## Docs to update when implemented

[auth.md](../../../docs/api/auth.md), [endpoints.md](../../../docs/api/endpoints.md), [rate-limits.md](../../../docs/api/rate-limits.md), [events.md](../../../docs/api/events.md), [mail.md](../../../docs/api/mail.md), [database.md](../../../docs/api/database.md), [api/testing.md](../../../docs/api/testing.md), [api/README.md](../../../docs/api/README.md) (verification mode and mail-off support), [admin.md](../../../docs/api/admin.md) (self-service identity/session effects), [compliance.md](../../../docs/compliance.md) (former-address erasure and delivery-outcome export exclusion), [UI API](../../../docs/ui/api.md), [UI pages](../../../docs/ui/pages.md), [UI design](../../../docs/ui/design.md) (locked-email example/shared dialog), [forms and actions](../../../docs/ui/forms-and-actions.md), [UI testing](../../../docs/ui/testing.md), [checks.md](../../../docs/checks.md) (confirmed resource/check registrations), [testing.md](../../../docs/testing.md), [plans index](../../README.md), and AGENTS.md's current “email change not implemented” statement. Update the privacy policy's data description if it describes email retention. Historical implemented plans remain historical.

## Not included

- Account merging, alternate sign-in addresses, email forwarding, provider-specific address equivalence, changing somebody else's email, or moving workspace memberships between users.
- Recovery after losing the password/second factor and old inbox; an operator bypass or proof-of-identity procedure needs its own confirmed design.
- One-click reversal, an old-inbox veto/cooling-off period, or guarantees that an old inbox can recover access after completion.
- Passkeys, OIDC, SMTP bounce/complaint processing, generic application step-up sessions, or unrelated password-change notifications.
