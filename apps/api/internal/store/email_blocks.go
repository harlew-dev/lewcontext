package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"time"
)

// EmailBlockLinkLifetime is how long a block link works after its email was
// sent: long enough to unblock an address blocked by mistake.
const EmailBlockLinkLifetime = 365 * 24 * time.Hour

// emailBlockLink is the block link in one blockable email, found by the
// digest of its token. Email is the address the email went to.
type emailBlockLink struct {
	bun.BaseModel `bun:"table:email_block_links,alias:ebl"`
	ID            int64     `bun:",pk,autoincrement"`
	TokenHash     string    `bun:",notnull"`
	Email         string    `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

// emailBlock is an address on the block list.
type emailBlock struct {
	bun.BaseModel `bun:"table:email_blocks,alias:eb"`
	ID            int64     `bun:",pk,autoincrement"`
	Email         string    `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// EmailBlock is a blocked address and when it was blocked.
type EmailBlock struct {
	Email     string
	CreatedAt time.Time
}

// EmailBlockLink is what a block link's page shows: the address, whether an
// account uses it, and whether it's blocked.
type EmailBlockLink struct {
	Email      string `json:"email"`
	Registered bool   `json:"registered"`
	Blocked    bool   `json:"blocked"`
}

var (
	// ErrBlockLinkInvalid means a block link is unknown or expired.
	ErrBlockLinkInvalid = errors.New("email block link is invalid")
	// ErrEmailRegistered refuses blocking an address an account uses: it
	// would cut that account off from password reset.
	ErrEmailRegistered = errors.New("an account uses this email")
	// ErrEmailBlocked refuses creating an account with a blocked address.
	ErrEmailBlocked = errors.New("this email is blocked")
	// ErrEmailNotBlocked means an address isn't on the block list.
	ErrEmailNotBlocked = errors.New("this email isn't blocked")
)

// CheckEmailBlocked reports whether email is on the block list.
func (s *Store) CheckEmailBlocked(ctx context.Context, email string) (bool, error) {
	return emailBlocked(ctx, s.DB, email)
}

func emailBlocked(ctx context.Context, db bun.IDB, email string) (bool, error) {
	return db.NewSelect().Model((*emailBlock)(nil)).Where("email = ?", email).Exists(ctx)
}

// IssueEmailBlockLink stores the digest hash of a new block link for email,
// working for EmailBlockLinkLifetime.
func (s *Store) IssueEmailBlockLink(ctx context.Context, email, hash string) error {
	link := emailBlockLink{TokenHash: hash, Email: email, ExpiresAt: time.Now().Add(EmailBlockLinkLifetime)}
	_, err := s.DB.NewInsert().Model(&link).Exec(ctx)
	return err
}

// CheckEmailBlockLink is what the live block link hash shows, or
// ErrBlockLinkInvalid.
func (s *Store) CheckEmailBlockLink(ctx context.Context, hash string) (EmailBlockLink, error) {
	email, err := blockLinkEmail(ctx, s.DB, hash)
	if err != nil {
		return EmailBlockLink{}, err
	}
	link := EmailBlockLink{Email: email}
	if link.Registered, err = s.DB.NewSelect().Model((*User)(nil)).Where("email = ?", email).Exists(ctx); err != nil {
		return link, err
	}
	link.Blocked, err = emailBlocked(ctx, s.DB, email)
	return link, err
}

// blockLinkEmail is the address the live block link hash was sent to.
func blockLinkEmail(ctx context.Context, db bun.IDB, hash string) (string, error) {
	var email string
	err := db.NewSelect().Model((*emailBlockLink)(nil)).Column("email").
		Where("token_hash = ? AND expires_at > now()", hash).
		Scan(ctx, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrBlockLinkInvalid
	}
	return email, err
}

// BlockEmail puts the address the live block link hash went to on the block
// list and records email.blocked; an address already blocked changes
// nothing. It takes the email's lock first, as creating an account does, so
// an account can't appear between the check and the block:
// ErrEmailRegistered when one uses the address.
func (s *Store) BlockEmail(ctx context.Context, hash string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		email, err := blockLinkEmail(ctx, tx, hash)
		if err != nil {
			return err
		}
		if err := database.LockEmail(ctx, tx, email); err != nil {
			return err
		}
		registered, err := tx.NewSelect().Model((*User)(nil)).Where("email = ?", email).Exists(ctx)
		if err != nil {
			return err
		}
		if registered {
			return ErrEmailRegistered
		}
		res, err := tx.NewInsert().Model(&emailBlock{Email: email}).On("CONFLICT (email) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return s.record(ctx, tx, events.EmailBlocked{Email: email}, EventScope{})
	})
}

// UnblockEmail takes the address the live block link hash went to off the
// block list, recording email.unblocked when it was on it.
func (s *Store) UnblockEmail(ctx context.Context, hash string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		email, err := blockLinkEmail(ctx, tx, hash)
		if err != nil {
			return err
		}
		_, err = s.unblock(ctx, tx, email, events.UnblockedByLink, EventScope{})
		return err
	})
}

// UnblockEmailAddress takes email off the block list by its address, or
// returns ErrEmailNotBlocked, recording nothing. With admin nil it's the
// operator's command line: email.unblocked (by: cli), with no actor. With
// an admin it's the dashboard's Unblock: email.unblocked (by: admin) and
// admin.email.unblocked (the admin, the address and the reason), both with
// the admin as actor, in the same transaction as the delete.
func (s *Store) UnblockEmailAddress(ctx context.Context, email string, admin *AdminAction) error {
	by, scope := events.UnblockedByCLI, EventScope{}
	if admin != nil {
		by, scope = events.UnblockedByAdmin, EventScope{ActorID: admin.Admin.ID}
	}
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		removed, err := s.unblock(ctx, tx, email, by, scope)
		if err == nil && !removed {
			return ErrEmailNotBlocked
		}
		if err != nil || admin == nil {
			return err
		}
		e := events.AdminEmailUnblocked{Admin: admin.Admin.PublicID, Email: email, Reason: admin.Reason}
		return s.record(ctx, tx, e, scope)
	})
}

// unblock deletes email's block and, when there was one, records
// email.unblocked by by, with scope.
func (s *Store) unblock(ctx context.Context, tx bun.Tx, email, by string, scope EventScope) (bool, error) {
	res, err := tx.NewDelete().Model((*emailBlock)(nil)).Where("email = ?", email).Exec(ctx)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	return true, s.record(ctx, tx, events.EmailUnblocked{Email: email, By: by}, scope)
}

// FindEmailBlock is email's block, or ErrEmailNotBlocked.
func (s *Store) FindEmailBlock(ctx context.Context, email string) (EmailBlock, error) {
	var b EmailBlock
	err := s.DB.NewSelect().Model((*emailBlock)(nil)).Column("email", "created_at").
		Where("email = ?", email).
		Scan(ctx, &b.Email, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrEmailNotBlocked
	}
	return b, err
}

// IndexEmailBlocks lists every blocked address, oldest first, for the
// command line.
func (s *Store) IndexEmailBlocks(ctx context.Context) ([]EmailBlock, error) {
	var rows []emailBlock
	if err := s.DB.NewSelect().Model(&rows).Order("created_at", "id").Scan(ctx); err != nil {
		return nil, err
	}
	blocks := make([]EmailBlock, len(rows))
	for i, row := range rows {
		blocks[i] = EmailBlock{Email: row.Email, CreatedAt: row.CreatedAt}
	}
	return blocks, nil
}

// refuseBlockedEmail takes email's lock for the rest of tx and returns
// ErrEmailBlocked when it's on the block list: every account creation calls
// it before inserting the user, as BlockEmail checks for an account.
func refuseBlockedEmail(ctx context.Context, tx bun.Tx, email string) error {
	if err := database.LockEmail(ctx, tx, email); err != nil {
		return err
	}
	blocked, err := emailBlocked(ctx, tx, email)
	if err == nil && blocked {
		return ErrEmailBlocked
	}
	return err
}
