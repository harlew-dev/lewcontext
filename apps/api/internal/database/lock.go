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
