# Prune command

Status: **implemented 2026-10-30** with [events](events.md) step 3. See [docs/api/database.md](../../docs/api/database.md#pruning).

Built as planned, except: a pruner's `count` and `delete` take the store first (`(*store.Store).CountOldEvents` as a method expression); `Options.Batch` lets tests use small batches; `make prune CONFIRM=1` without a target says confirming needs a target; and output uses plain numbers (`12408`).

## Why

Some tables grow without limit and are only useful for a while: the `events` audit trail first, other logs later. A prune command deletes their old rows on demand, like `php artisan model:prune`, so retention is a choice made when running it (by hand or from cron) rather than built into the server.

`sessions` and `auth_attempts` are already cleaned by the server's 15-minute cleanup loop (`store.Cleanup`) and don't need a target.

## Usage

```sh
make prune                                          # previews every target with its default age; deletes nothing
make prune target=events                            # preview: "would delete 12 408 events older than 2026-08-01 (90d)"
make prune target=events older-than=7d              # preview with a different age
make prune target=events older-than=7d CONFIRM=1    # deletes
cd apps/api && go run ./cmd/prune events -older-than 7d -confirm
cd apps/api && go run ./cmd/prune -h                # lists targets and their default ages
```

## Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Naming | `prune` + target (`go run ./cmd/prune events`), following the `prune:{target}` idea. Make targets can't contain `:` easily, so the Make form takes `target=`. |
| 2 | Age flag | `-older-than` with `d` or `h` units (`7d`, `36h`), like `journalctl --vacuum-time`: "delete what's older than this", not limited to whole days. Each target has a default age. |
| 3 | Safety | Preview unless confirmed, like `migrate-rollback`: without `CONFIRM=1` (`-confirm`) it prints the count and cutoff and deletes nothing. |
| 4 | No target | A bare `make prune` previews every target with its default age and never deletes, even with `CONFIRM=1`. |
| 5 | Production | Allowed (it previews unless confirmed), unlike `seed`. A deployment would run it from cron. |

## Design

### Registry (`internal/prune`)

Like `internal/seed`'s `Seeders` and `seed.Find`:

```go
type Pruner struct {
	Name       string        // "events"
	DefaultAge time.Duration // 90 * 24 * time.Hour
	Describe   string        // shown in -h
	Count      func(ctx context.Context, db bun.IDB, before time.Time) (Result, error)
	Delete     func(ctx context.Context, db bun.IDB, before time.Time, batch int) (Result, error)
}
type Result struct {
	Deleted int // or "would delete" in a preview
	Kept    int // old rows a target refuses to delete, with a reason in Describe
}

var Pruners = []Pruner{events}
func Find(name string) (Pruner, bool)
```

- Each pruner's queries live in `internal/store` (AGENTS rule 5: store owns Bun queries); `internal/prune` holds only the registry and the batching loop.
- Adding a target is one entry in `Pruners` plus its two store queries.

### `cmd/prune`

- Composition only: load config, parse flags, open the database, find the pruner, call `Count` or `Delete`, print the summary. Same shape as `cmd/seed`.
- Output: `events: would delete 12 408 older than 2026-08-01 00:00 UTC (90d); kept 3 with pending deliveries`, then on confirm `events: deleted 12 408 …`.
- Exit codes: non-zero for an unknown target, a bad `-older-than`, or a database error; usage lists the targets.

### Safety and batching

- **Refuses ages under 1 hour,** so a typo like `older-than=0d` can't wipe a table. Also rejects negatives and anything that isn't `<number>d` or `<number>h`.
- **The cutoff is computed once** at start (`now() - age`), so rows created while it runs are never in scope.
- **Deletes in batches** (5 000 rows per statement, each its own short transaction) so a large prune doesn't hold one long transaction or lock many rows at once. Each batch selects ids with `ORDER BY id LIMIT 5000 FOR UPDATE SKIP LOCKED`, so it skips rows another prune run has locked rather than waiting. It doesn't compete with the events worker: events with pending deliveries are excluded anyway, and the worker's row locks last only for its short claim statement.
- Ctrl-C stops between batches; what's deleted stays deleted, and the summary says how many.
- A per-run timeout (e.g. 30 minutes) like `cmd/seed`'s.

### Targets (v1)

| Target | Default age | Deletes | Keeps |
| --- | --- | --- | --- |
| `events` | 90 days | events created before the cutoff (their `event_deliveries` go with them, `ON DELETE CASCADE`) | events with a pending delivery, counted as "kept" so they're retried first; failed deliveries don't block pruning |

## Makefile

```make
# make prune [target=events] [older-than=7d] [CONFIRM=1]: previews unless CONFIRM=1.
prune:
	cd apps/api && go run ./cmd/prune $(target) $(if $(older-than),-older-than '$(older-than)') $(if $(CONFIRM),-confirm)
```

`make` loads `.env`, so it always prunes the `DATABASE_URL` there. For another database, run `go run ./cmd/prune` directly (as with seeding).

## Tests

- Unit: `-older-than` parsing (`7d`, `36h`; rejects `0`, under 1 hour, negatives, `7`, `7days`, junk); `Find` for known and unknown targets; a bare run never calls `Delete`.
- Integration (real PostgreSQL), with factories building old and new rows:
  - preview deletes nothing and reports the right count;
  - confirm deletes exactly the rows older than the cutoff;
  - events with a pending delivery are kept and counted; events whose deliveries all succeeded or failed are deleted;
  - batching: more rows than one batch are all deleted.

## Docs

- `docs/api/database.md`: a Pruning section (usage, targets, defaults, safety).
- `AGENTS.md` rule 5: `cmd` contains composition, process startup/shutdown, migration, seed **and prune** entrypoints. The Database section: logs like `events` are pruned, not kept forever.
- `docs/README.md` / `docs/testing.md`: mention `make prune` next to `make seed`.
