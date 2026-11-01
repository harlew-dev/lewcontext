# Fake data: factories and seeders

Factories build fake records for every table, and seeders use them to fill a development database. Tests use the same factories. Values are random [gofakeit v7](https://pkg.go.dev/github.com/brianvoe/gofakeit/v7) data.

## Seeding a database

```sh
make seed                                   # demo account test@example.com, its "Acme" workspace + 6 projects + 4 members (skipped if it exists)
make seed only=volume count=500             # projects in the demo account's newest workspace
make seed only=volume email=you@example.com # …or another existing account's (it needs a workspace)
make seed only=users count=25 projects=20   # 25 fake accounts, each owning a workspace with 0–20 projects
make seed only=monitoring                   # fake attacks for the admin Monitoring pages (stuffing, a takeover, the admin door, …)
make seed only=events count=1000000         # fake audit trail events, to check the admin Events page at volume
```

| Seeder | Default count | Idempotent |
| --- | --- | --- |
| `demo` | The "Acme" workspace, 6 named projects, and 4 fake members (admin, two members, viewer) who can't sign in | Yes |
| `volume` | 120 projects | No (insert-only; refuses unknown accounts and accounts without a workspace) |
| `users` | 10 users, each owning a workspace with up to 60 projects; every third never verified its email (as if made while `EMAIL_VERIFICATION` was off), to try the Not verified badge and `EMAIL_VERIFICATION_EXISTING=required` | No (insert-only) |
| `monitoring` | A few hundred fake events planting what the Security and Rate limits pages look for: a day of ordinary sign-ins, then in the last hour credential stuffing from `203.0.113.66`, a password guessed from six addresses, a sign-in after six wrong passwords, failed codes, a reset flood, `register:global` reached, the admin door, and one fake account (an unusable password) taken over ([monitoring.md](monitoring.md#demo-data-make-seed-onlymonitoring)). Documentation-range addresses, `@example.test` emails, every payload `"seeded": true`; never `rate_limits`, metrics or errors. `count` is ignored | No (insert-only) |
| `events` | 10,000 fake events (at most 2,000,000): most in the last 7 days, a thin tail back 90, sign-ins weighted heaviest, and bursts of failed sign-ins from three addresses. Payloads look like the store's but name accounts and workspaces that don't exist, and every one holds `"seeded": true`, so fake events can be told apart (`payload->>'seeded'`). For `EXPLAIN` at volume ([testing.md](../testing.md#volume-check-for-the-events-page)), not for demos | No (insert-only) |

- Accounts created by `demo` and `users` sign in with `DEMO_PASSWORD` from `.env` (`password` in `.env.example`). It may be 1–128 bytes: sign-up's 12-character minimum doesn't apply to seeded development accounts.
- Each seeder runs in one transaction and prints its fake data seed. Seeders never delete or truncate. Production is refused before connecting, for every seeder (`TestSeedingIsRefusedInProduction`, `cmd/seed`).
- Counts are 0–100 000 (`events`: 0–2 000 000); 0 means the default. Run `cd apps/api && go run ./cmd/seed -h` to list seeders.
- After a schema change, rebuild the local database first: `make migrate-fresh CONFIRM=1 && make seed` ([database.md](database.md#migrations)).
- **`make` always uses the `DATABASE_URL` from `.env`.** To seed another database or schema, run `cd apps/api && DATABASE_URL=… go run ./cmd/seed -only volume`.

## Factories (`internal/factory`)

| Factory | Model | Notes |
| --- | --- | --- |
| `Users` | `User` | Unique lowercase emails at `example.test`; password `factory.DefaultPassword` (one shared hash per process); email verified (`Unverified` for one that never was); no policy agreement (`AgreedToPolicies(terms, privacy)` for one, accepted when created); never seen (`LastSeenAt` nil) unless a test sets it |
| `Sessions` | `Session` | Real random token, only the digest stored; `Cookie()`, `CSRFToken`. Needs `UserID`; use `SessionFor` |
| `Workspaces` | `Workspace` | Fake company names (≤ 80 characters), created in the last year. Use `WorkspaceFor` to add an owner |
| `WorkspaceUsers` | `WorkspaceUser` | A membership with a random role (any of `auth.Roles`, viewers included). Needs `UserID` and `WorkspaceID`; use `MemberOf` |
| `Projects` | `Project` | Valid name, description, status and color; created in the last 90 days. Needs `WorkspaceID`; use `ProjectsIn` |
| `RateLimits` | `RateLimit` | Counter in the current 15-minute window, keyed like the `auth:per-ip` limiter |
| `Events` | `Event` | `rate_limit.reached` events from the last 90 days (no other records needed); set `UserID`/`WorkspaceID` with a state. Only the `events` seeder creates events, and only when asked for by name: fake history is for checking volume, never demos |
| `WorkspaceInvitations` | `WorkspaceInvitation` | A live invitation, not yet sent, to a unique `example.test` address as admin, member or viewer. Needs `WorkspaceID`; use `InvitationsTo(workspaceID)`. `Sent` gives it a link and keeps the raw `Token` |
| `MFATOTPs` | `MFATOTP` | A confirmed authenticator (MFA on), its secret sealed with `factory.Keys`; `Plain` holds it and `Code()` makes the current code. Use `TOTPFor(userID)`, which sets the user and seals it; `PendingTOTP` leaves setup pending |
| `RecoveryCodes` | `RecoveryCode` | An unused recovery code, stored as its keyed digest; `Code` keeps it as shown. Use `RecoveryCodesFor(userID)` |
| `PendingRegistrations` | `PendingRegistration` | A live sign-up waiting for its code (15 minutes) with the code already sent: `Token` and `Cookie()` (the browser's), `Code` (stored as its HMAC under `factory.Keys`). `RegistrationFor(email)`, `ExpiredRegistration`; no policy versions unless `RegistrationAgreedTo(terms, privacy)` |
| `EmailVerifications` | `EmailVerification` | A live code for an account verifying its email; `Code`. Use `VerificationFor(userID)` |
| `MFAChallenges` | `MFAChallenge` | A live sign-in challenge (purpose `login`, 5 minutes); `Token` and `Cookie()`. Use `ChallengeFor(userID)`; `ExpiredChallenge` |
| `Admins` | `Admin` | A viewer with no setup window (as when MFA is on). Use `AdminFor(userID)`; `AdminRole(auth.AdminOperator)` for an operator; `SetupWindow` opens the 24-hour window, `ClosedSetupWindow` one that ended. Seeders make no admins: only `admin grant` does |
| `AdminSessions` | `AdminSession` | A live admin session under `factory.AdminRouteKey` (random per process, the integration server's key), signed in from `127.0.0.1` (the tests' requests' address) with a fake browser; `Cookie()`, `CSRFToken`. Use `AdminSessionFor(userID)` for an admin; `AdminIdle`, `AdminExpired`, `UnderKey(key)`, `SignedInFrom(ip)` |
| `EmailBlockLinks` | `EmailBlockLink` | A live block link (a year) to a unique `example.test` address; `Token` keeps the raw token. `BlockLinkFor(email)`, `ExpiredBlockLink` |
| `EmailBlocks` | `EmailBlock` | A blocked `example.test` address. `Blocked(email)` for a given one. Seeders block nothing |
| `EventDeliveries` | `EventDelivery` | A pending delivery, due now. Needs `EventID`; use `DeliveryOf(eventID, listener)` |
| `AppErrors` | `AppError` | An open, already-scrubbed `request` error group with a fingerprint of its own, first seen in the last 30 days and counted 1–500 times. `LastSeen(at)` sets when it last happened, `Resolved` marks it resolved. Seeders make none: fake failures would be misleading |
| `RequestMinutes`, `MetricMinutes` | `RequestMinute`, `MetricMinute` | A server's minute in the last hour (`ServerID(f)`, a real route; a `rate_limit_refused_total` counter). Give several for one server their own minute or route (`Sequence`). Seeders make none: made-up traffic would be misleading |
| `RequestHours`, `MetricHours` | `RequestHour`, `MetricHour` | An hour's sums in the last 90 days, each with a route or labels of its own |
| `ServerInstances` | `ServerInstance` | A server started in the last day and seen in the last minute; `LastSeenAt(at)` |

```go
f := factory.TestFaker(t) // or factory.NewFaker() outside tests
user, _ := factory.Users.CreateOne(ctx, db, f)
session, _ := factory.SessionFor(ctx, db, f, user.ID) // session.Cookie(), session.CSRFToken
workspace, _ := factory.WorkspaceFor(ctx, db, f, user.ID) // owned by user; factory.Named("Acme") sets the name
factory.MemberOf(ctx, db, f, other.ID, workspace.ID, "member")

factory.ProjectsIn(workspace.ID).
	State(func(p *factory.Project) { p.Status = "Completed" }).    // every value
	Sequence(func(i int, p *factory.Project) { p.Name = fmt.Sprint("P", i) }). // by position
	Create(ctx, db, f, 45)                                          // batched INSERT … RETURNING
```

- **Methods:** `Make` (build only), `Create`, `CreateOne`, `State`, `Sequence`. Factories are immutable, so each state returns a copy.
- **Named states:** `Expired` and `Idle` (sessions), `ForAccount`, `ForIP`, `ForLimit(name, subject)` and `Exhausted(limit)` (rate limits), `WithPasswordHash` and `Unverified` (users; every factory user is verified otherwise), `RegistrationFor`, `ExpiredRegistration` and a registration's `Cookie()` (pending sign-ups, with their `Code`), `VerificationFor` (an account's emailed code), `Named` (workspaces), `Sent`, `ExpiredInvitation` and `InvitedAs(email, role)` (invitations), `TOTPFor`, `PendingTOTP`, `RecoveryCodesFor`, `ChallengeFor`, `ExpiredChallenge` and `Purpose` (two-factor authentication; `AdminCookie()` is a challenge's admin door cookie), `AdminFor`, `AdminRole`, `SetupWindow`, `ClosedSetupWindow`, `AdminSessionFor`, `AdminIdle`, `AdminExpired`, `UnderKey` and `SignedInFrom` (admins), `BlockLinkFor`, `ExpiredBlockLink` and `Blocked` (the block list), `LastSeen` and `Resolved` (grouped errors).
- **Encryption key:** MFA factories seal and digest with `factory.AppKey`, random per process. A server reads them only when given that key (`config.Config{AppKey: factory.AppKey}`, as `integrationServer` does). Seeders create no MFA rows.
- **Public IDs** are left empty and filled by PostgreSQL's default; `Create` reads them back (`workspace.PublicID`).
- **Randomness:** fresh every run. When a test fails it logs `fake data seed N`, and `FAKER_SEED=N` replays it. Seeders print the seed too.

## Rules

- **Every table has a factory, with its own Bun model** (separate from `store`'s on purpose). `TestEveryTableHasAFactory` fails for a table without one. Every factory's output is inserted into a freshly migrated schema, so a new required column also needs a factory value.
- Factory values must pass API validation (`TestFactoriesMatchValidation`).
- When a migration changes a table, update the store model, the factory and any seeder in the same change.
- The API server binary doesn't include `factory` or `seed`.
