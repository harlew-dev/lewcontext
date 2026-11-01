# Monitoring

Plan: [monitoring](../../plans/features/admin/monitoring.md) (A4), shipped page by page.

## Active accounts: `users.last_seen_at`

`users.last_seen_at` is when the account last used a session, to the hour. The session check (`store.FindSession`, behind `RequireSession` and the `/app/*` page gate) moves it **in the same statement** that refreshes the session's idle time, and only when it's null or over an hour old (`seenEvery`), so an active account costs one row write an hour and the check no extra round trip. It never records an event: it isn't something the account did, and an event an hour per account would fill the audit trail. It's null until the account's first signed-in request; an expired or idle session moves nothing. A partial index (`users_last_seen_idx`) serves counting the accounts seen since a time. `TestSessionCheckMovesLastSeenHourly` (`internal/store`) checks it.
