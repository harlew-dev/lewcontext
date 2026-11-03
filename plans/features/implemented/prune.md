# Prune command

Status: **implemented 2026-10-30** with [events](events.md) step 3, then restructured to one file per target with Bun queries (below). See [docs/api/database.md](../../../docs/api/database.md#pruning).

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

## Restructure: one file per target, Bun only (implemented 2026-10-30)

### Decisions

| # | Question | Decision |
| --- | --- | --- |
| 1 | Prune inactive records (projects, workspaces, accounts)? | Not now. |
| 2 | What counts as "inactive"? | Decide later, with its own plan. |
| 3 | Delete or archive first? | Delete. |
| 4 | Ages | Each target has its own default age. `-older-than` is only allowed with a target (a single run); a run over every target uses each target's default. |
| 5 | Raw SQL | Write every query with Bun's builder (AGENTS.md updated); the prune and delivery-claim queries move to it. |

### Files

```
apps/api/
  cmd/prune/main.go                   flags; -older-than requires a target
  internal/prune/
    prune.go                          Pruner, register, Pruners() (sorted), Find, Run, ParseAge/FormatAge
    events.go                         the events target: name, description, default age; registers itself
    prune_test.go, prune_integration_test.go
  internal/store/
    prune.go                          Prunable, plus CountPrunable and Prune for any target, with Bun
    prune_events.go                   PrunableEvents: model, age column, which rows to keep
    deliveries.go                     Claim rewritten with Bun (NewUpdate + With)
```

Adding a target is two new files (`internal/store/prune_<target>.go` and `internal/prune/<target>.go`) and its tests; nothing shared changes. A target needs a bigint `id` primary key.

### Code

```go
// internal/store/prune.go — shared by every target.

// Prunable is a table the prune command can delete old rows from.
type Prunable struct {
	Model any    // the table's model, e.g. (*eventRecord)(nil)
	Age   string // the column whose age counts, e.g. "created_at"
	// Keep narrows a query to old rows that must stay (optional).
	Keep func(q *bun.SelectQuery) *bun.SelectQuery
}

// old selects the target's rows older than before.
func (s *Store) old(p Prunable, before time.Time) *bun.SelectQuery {
	return s.DB.NewSelect().Model(p.Model).Where("?TableAlias.? < ?", bun.Ident(p.Age), before)
}

// prunable is old minus the rows Keep holds back.
func (s *Store) prunable(p Prunable, before time.Time) *bun.SelectQuery {
	q := s.old(p, before)
	if p.Keep != nil {
		q = q.WhereGroup(" AND NOT ", p.Keep)
	}
	return q
}

// CountPrunable counts the old rows a run would delete, and those it keeps.
func (s *Store) CountPrunable(ctx context.Context, p Prunable, before time.Time) (prunable, kept int, err error) {
	if prunable, err = s.prunable(p, before).Count(ctx); err != nil || p.Keep == nil {
		return prunable, 0, err
	}
	kept, err = s.old(p, before).WhereGroup(" AND ", p.Keep).Count(ctx)
	return prunable, kept, err
}

// Prune deletes up to limit prunable rows in one short statement.
// SKIP LOCKED leaves rows another run holds rather than waiting for them.
func (s *Store) Prune(ctx context.Context, p Prunable, before time.Time, limit int) (int, error) {
	batch := s.prunable(p, before).
		ColumnExpr("?TableAlias.id").
		OrderExpr("?TableAlias.id").
		Limit(limit).
		For("UPDATE SKIP LOCKED")
	result, err := s.DB.NewDelete().Model(p.Model).Where("?TableAlias.id IN (?)", batch).Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}
```

```go
// internal/store/prune_events.go — the events target's queries.

// PrunableEvents keeps events whose listeners haven't run yet; delivered
// and failed deliveries go with their event (ON DELETE CASCADE).
var PrunableEvents = Prunable{
	Model: (*eventRecord)(nil),
	Age:   "created_at",
	Keep: func(q *bun.SelectQuery) *bun.SelectQuery {
		pending := q.NewSelect().Model((*deliveryRecord)(nil)).
			ColumnExpr("1").
			Where("d.event_id = e.id").
			Where("d.delivered_at IS NULL AND d.failed_at IS NULL")
		return q.Where("EXISTS (?)", pending)
	},
}
```

```go
// internal/prune/events.go — the events target as the command lists it.

func init() {
	register(Pruner{
		Name:        "events",
		Description: "the audit trail, keeping events whose listeners haven't run yet",
		DefaultAge:  90 * day,
		Table:       store.PrunableEvents,
		KeptReason:  "with pending deliveries",
	})
}
```

```go
// internal/prune/prune.go — shared: the list and the run (abridged).

type Pruner struct {
	Name, Description, KeptReason string
	DefaultAge                    time.Duration
	Table                         store.Prunable
}

var pruners = map[string]Pruner{}

// register adds a target; each target's file calls it from init.
func register(p Pruner) {
	if _, taken := pruners[p.Name]; taken || p.DefaultAge < MinAge {
		panic("prune: bad or duplicate target " + p.Name)
	}
	pruners[p.Name] = p
}

// Pruners lists the targets by name.
func Pruners() []Pruner { … sorted copy … }

// Run: count with s.CountPrunable(ctx, p.Table, before); when confirmed,
// delete with s.Prune(ctx, p.Table, before, batch) until a batch comes back
// short (unchanged from today).
```

```go
// cmd/prune/main.go — -older-than only with a target.
if target == "" && *olderThan != "" {
	log.Fatal("-older-than needs a target; a run over every target uses each one's default age")
}
```

### The worker's claim, with Bun

```go
due := s.DB.NewSelect().Model((*deliveryRecord)(nil)).
	Column("event_id", "listener").
	Where("delivered_at IS NULL AND failed_at IS NULL AND next_attempt_at <= now()").
	OrderExpr("next_attempt_at, event_id").
	Limit(limit).
	For("UPDATE SKIP LOCKED")
err := s.DB.NewUpdate().
	With("due", due).
	Model((*deliveryRecord)(nil)).
	TableExpr("due, events AS e").
	Set("attempts = d.attempts + 1").
	Set("next_attempt_at = now() + ? * interval '1 millisecond'", lease.Milliseconds()).
	Where("d.event_id = due.event_id AND d.listener = due.listener AND e.id = d.event_id").
	Returning("d.event_id, d.listener, d.attempts, e.public_id, e.name, e.payload, e.created_at").
	Scan(ctx, &rows)
```

Built as proposed. Also: `SELECT current_schema()` in `internal/database` moved to Bun, test schemas are created and dropped with `bun.Ident` placeholders, and the migration test now checks `events` and `event_deliveries` are dropped on rollback.

### Tests and other changes

- Existing prune tests keep passing unchanged in behaviour; new: `-older-than` without a target is refused; `register` panics on a duplicate name; `Pruners()` is sorted.
- Integration test lookups (`idOf`, `emailOf`, `eventsNamed`, `storedPasswordIs`, …) move from SQL strings to Bun, per the new rule.
- Docs: `docs/api/database.md` (Queries: Bun only; Pruning: adding a target is two files).
