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
