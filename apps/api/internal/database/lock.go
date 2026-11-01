package database

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"github.com/uptrace/bun"
)

// LockEmail takes a transaction-scoped advisory lock on an email address,
// held until tx commits or rolls back. Blocking an address and creating an
// account with it each take it before they check the other, so the two
// can't both pass their check at once (plans/features/implemented/email-blocks.md). The
// key is the first 8 bytes of the address's SHA-256; two addresses sharing
// one only wait for each other.
func LockEmail(ctx context.Context, tx bun.Tx, email string) error {
	sum := sha256.Sum256([]byte(email))
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(?)", int64(binary.BigEndian.Uint64(sum[:8])))
	return err
}

// ipBlocksLock names the lock creating an IP block takes. The key is a hash
// of this and the current schema: every server of one deployment shares a
// schema, so they share the lock, while integration tests, each in a
// schema of their own, don't wait on each other.
const ipBlocksLock = "lewcontext:ip-blocks:"

// LockIPBlocks takes the IP blocks' advisory lock for tx, waiting for it,
// held until tx commits or rolls back: creating a block takes it before
// counting the live ones, so two admins blocking at once can't both pass
// the cap (store.MaxIPBlocks).
func LockIPBlocks(ctx context.Context, tx bun.Tx) error {
	var locked string
	return tx.NewSelect().
		ColumnExpr("pg_advisory_xact_lock(hashtextextended(? || current_schema(), 0))::text", ipBlocksLock).
		Scan(ctx, &locked)
}

// metricsRollupLock names the lock the hourly metrics rollup takes. The key
// is a hash of this and the current schema: every server of one deployment
// shares a schema, so they share the lock, while integration tests, each in
// a schema of their own, don't wait on each other.
const metricsRollupLock = "lewcontext:metrics-rollup:"

// TryLockMetricsRollup tries to take the metrics rollup's advisory lock for
// tx, held until it commits or rolls back (or its connection drops, if the
// server dies). It never waits: false means another server is rolling up,
// so this one skips its turn. PostgreSQL's advisory locks are exclusive
// across every connection to the database, so only one server at a time
// sums the hours and deletes old minutes.
func TryLockMetricsRollup(ctx context.Context, tx bun.Tx) (bool, error) {
	var locked bool
	err := tx.NewSelect().
		ColumnExpr("pg_try_advisory_xact_lock(hashtextextended(? || current_schema(), 0))", metricsRollupLock).
		Scan(ctx, &locked)
	return locked, err
}

// tableStatsLock names the lock the daily table sizes take
// (store.RecordTableStats), hashed with the current schema like the
// rollup's.
const tableStatsLock = "lewcontext:table-stats:"

// TryLockTableStats tries to take the daily table sizes' advisory lock for
// tx, held until it commits or rolls back, never waiting: false means
// another server is writing them, so this one skips its turn.
func TryLockTableStats(ctx context.Context, tx bun.Tx) (bool, error) {
	var locked bool
	err := tx.NewSelect().
		ColumnExpr("pg_try_advisory_xact_lock(hashtextextended(? || current_schema(), 0))", tableStatsLock).
		Scan(ctx, &locked)
	return locked, err
}

// alertsLock names the session lock the server evaluating alerts holds,
// and alertWritesLock the transaction lock each change to an alert takes.
// Each key is a hash of the name and the current schema, as above.
const (
	alertsLock      = "lewcontext:alerts:"
	alertWritesLock = "lewcontext:alert-writes:"
)

// LockAlertWrites takes the alerts' write lock for tx, waiting for it,
// held until tx commits or rolls back: every change to an alert takes it
// before it reads what it decides by (an open alert of the signal, the
// openings of the last 10 minutes), so even two evaluators at once can't
// both open, or both tell about, the same thing.
func LockAlertWrites(ctx context.Context, tx bun.Tx) error {
	var locked string
	return tx.NewSelect().
		ColumnExpr("pg_advisory_xact_lock(hashtextextended(? || current_schema(), 0))::text", alertWritesLock).
		Scan(ctx, &locked)
}

// Leader is a session advisory lock held on a connection of its own,
// taken from the pool for as long as it's held: the server holding the
// alerts' lock is the one that evaluates them (TryLeadAlerts). PostgreSQL
// releases it when Release unlocks it or the connection ends, so a server
// that dies, or loses its connection, hands it over: another server takes
// it at its next try.
type Leader struct {
	conn bun.Conn
}

// TryLeadAlerts tries to take the alerts' session lock on a connection of
// its own. It never waits: nil (and no error) means another server holds
// it.
func TryLeadAlerts(ctx context.Context, db *bun.DB) (*Leader, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	err = conn.NewSelect().
		ColumnExpr("pg_try_advisory_lock(hashtextextended(? || current_schema(), 0))", alertsLock).
		Scan(ctx, &locked)
	if err != nil || !locked {
		_ = conn.Close()
		return nil, err
	}
	return &Leader{conn: conn}, nil
}

// Check says whether the lock is still held: its connection still
// answers, and holds it (a session lock ends only with Release or the
// session). An error means it isn't, and the leader must stop: Release it.
func (l *Leader) Check(ctx context.Context) error {
	var held bool
	err := l.conn.NewSelect().
		TableExpr("pg_locks").
		ColumnExpr("count(*) > 0").
		Where("locktype = 'advisory' AND granted AND pid = pg_backend_pid()").
		Where("objid = (hashtextextended(? || current_schema(), 0) & 4294967295)::bigint::oid", alertsLock).
		Scan(ctx, &held)
	if err == nil && !held {
		err = errors.New("the alerts' lock is no longer held")
	}
	return err
}

// Release unlocks and returns the connection to the pool, closing it
// rather than returning it if unlocking fails, so a lock is never left on
// a pooled connection.
func (l *Leader) Release(ctx context.Context) {
	var unlocked bool
	err := l.conn.NewSelect().
		ColumnExpr("pg_advisory_unlock(hashtextextended(? || current_schema(), 0))", alertsLock).
		Scan(ctx, &unlocked)
	if err != nil || !unlocked {
		// Raw closes the driver connection, so PostgreSQL ends the session
		// and its locks with it.
		_ = l.conn.Raw(func(driverConn any) error { return driver.ErrBadConn })
	}
	_ = l.conn.Close()
}
