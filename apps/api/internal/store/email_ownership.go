package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
)

// lockEmailVersion serialises authentication with email changes. A login
// that started under an earlier ownership generation must start again.
// Lock order: account, sessions, authenticator, challenge/pending change;
// workspace and destination-email locks are taken only after the account.
func lockEmailVersion(ctx context.Context, tx bun.Tx, userID, expected int64) error {
	var version int64
	err := tx.NewSelect().Model((*User)(nil)).Column("email_version").
		Where("id = ?", userID).For("UPDATE").Scan(ctx, &version)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && version != expected) {
		return ErrNoChallenge
	}
	return err
}
