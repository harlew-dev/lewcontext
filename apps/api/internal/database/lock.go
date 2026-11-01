package database

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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
