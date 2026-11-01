package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"strings"
)

// Erasure (docs/compliance.md#erasure): deleting an account deletes the
// events about it (ON DELETE CASCADE), and, in the same transaction, its
// email in every other event, with the name beside it: a sign-in attempt,
// a reset request or an invitation to it, an admin's search for it. IPs
// stay: they are minimised at 30 days anyway, and a record of an address's
// behaviour is a security interest that outlives the account.

// Redacted replaces an erased account's email and name in the events that
// outlive it.
const Redacted = "[deleted]"

// ErasedAccount is what EraseAccount did.
type ErasedAccount struct {
	User string // the account's usr_ ID
	// Workspaces is how many workspaces only the account belonged to,
	// deleted with it.
	Workspaces int
}

// accountToErase is the account's row, locked, as erasing reads it.
type accountToErase struct {
	ID       int64
	PublicID string
	Email    string
}

// EraseAccount is the operator's erasure (admin erase), for someone who
// can't sign in to delete their own account: the same deletion as
// DeleteAccount, with the same locks in the same order, for the account
// using email. It refuses an admin's account (ErrActionOnAdmin: revoke
// admin first) and, with a LastOwnerError, an account that is the only
// owner of a workspace someone else belongs to; a workspace only the
// account belongs to is deleted with it (workspace.deleted). Records
// auth.account_deleted and admin.user.erased, without an actor.
// sql.ErrNoRows when no account uses email.
func (s *Store) EraseAccount(ctx context.Context, email string, pseudonym func(string) string) (ErasedAccount, error) {
	var done ErasedAccount
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		target, err := lockAccount(ctx, tx, "lower(u.email) = ?", strings.ToLower(strings.TrimSpace(email)))
		if err != nil {
			return err
		}
		if admin, err := isAdmin(ctx, tx, target.ID); err != nil {
			return err
		} else if admin {
			return ErrActionOnAdmin
		}
		owned, err := lockOwnedWorkspaces(ctx, tx, target.ID)
		if err != nil {
			return err
		}
		// The workspaces only it belongs to go first, so the refusal names
		// only those someone else would be left in (a refusal rolls the
		// deletions back with everything else).
		done = ErasedAccount{User: target.PublicID}
		if done.Workspaces, err = s.deleteOwnWorkspaces(ctx, tx, target.ID, owned); err != nil {
			return err
		}
		if err := refuseLastOwner(ctx, tx, target.ID); err != nil {
			return err
		}
		if err := s.record(ctx, tx, events.AdminUserErased{User: target.PublicID, Workspaces: done.Workspaces}, EventScope{}); err != nil {
			return err
		}
		return s.eraseAccount(ctx, tx, target, pseudonym)
	})
	return done, err
}

// lockAccount locks the account matching where (u is users) and reads
// what erasing it needs. sql.ErrNoRows when there's none.
func lockAccount(ctx context.Context, tx bun.Tx, where string, args ...any) (accountToErase, error) {
	var target accountToErase
	err := tx.NewSelect().Model((*User)(nil)).
		Column("u.id", "u.public_id", "u.email").
		Where(where, args...).
		For("UPDATE").
		Scan(ctx, &target.ID, &target.PublicID, &target.Email)
	return target, err
}

// lockOwnedWorkspaces locks every workspace the user owns, in id order
// (so two deletions can't deadlock), and returns their IDs.
func lockOwnedWorkspaces(ctx context.Context, tx bun.Tx, userID int64) ([]int64, error) {
	var owned []int64
	err := tx.NewSelect().Model((*Workspace)(nil)).Column("w.id").
		Join("JOIN workspace_user AS m ON m.workspace_id = w.id").
		Where("m.user_id = ? AND m.role = ?", userID, auth.RoleOwner).
		OrderExpr("w.id").
		For("UPDATE OF w").
		Scan(ctx, &owned)
	return owned, err
}

// refuseLastOwner answers a LastOwnerError listing the workspaces the user
// can't leave (they're its only owner), once lockOwnedWorkspaces holds
// them.
func refuseLastOwner(ctx context.Context, tx bun.Tx, userID int64) error {
	var stuck []WorkspaceSummary
	err := tx.NewSelect().Model((*Workspace)(nil)).Column("w.public_id", "w.name").
		Join("JOIN workspace_user AS m ON m.workspace_id = w.id").
		Where("m.user_id = ?", userID).
		Where("NOT ?", canLeave(tx)).
		OrderExpr("w.id").
		Limit(maxLastOwnerWorkspaces).
		Scan(ctx, &stuck)
	if err != nil {
		return err
	}
	if len(stuck) > 0 {
		return LastOwnerError{Workspaces: stuck}
	}
	return nil
}

// deleteOwnWorkspaces deletes the workspaces among owned (locked) that
// nobody but the user belongs to, each recording workspace.deleted, and
// counts them. Nobody can join one meanwhile: every membership change
// takes the workspace's lock.
func (s *Store) deleteOwnWorkspaces(ctx context.Context, tx bun.Tx, userID int64, owned []int64) (int, error) {
	if len(owned) == 0 {
		return 0, nil
	}
	var alone []Workspace
	others := tx.NewSelect().Model((*membership)(nil)).ColumnExpr("1").
		Where("m.workspace_id = w.id AND m.user_id <> ?", userID)
	err := tx.NewSelect().Model(&alone).Column("w.id", "w.public_id", "w.name").
		Where("w.id IN (?)", bun.In(owned)).
		Where("NOT EXISTS (?)", others).
		OrderExpr("w.id").
		Scan(ctx)
	if err != nil {
		return 0, err
	}
	for _, w := range alone {
		projects, err := tx.NewSelect().Model((*Project)(nil)).Where("p.workspace_id = ?", w.ID).Count(ctx)
		if err != nil {
			return 0, err
		}
		e := events.WorkspaceDeleted{Workspace: w.PublicID, Name: w.Name, Projects: int(projects), Members: 1}
		if err := s.record(ctx, tx, e, EventScope{WorkspaceID: w.ID}); err != nil {
			return 0, err
		}
		if _, err := tx.NewDelete().Model((*Workspace)(nil)).Where("w.id = ?", w.ID).Exec(ctx); err != nil {
			return 0, err
		}
	}
	return len(alone), nil
}

// eraseAccount records auth.account_deleted, erases the account's email
// from the events that outlive it, and deletes it (the cascades do the
// rest).
func (s *Store) eraseAccount(ctx context.Context, tx bun.Tx, target accountToErase, pseudonym func(string) string) error {
	// No user scope, or the event would be deleted with the account; its
	// actor, the account, becomes null when the account is deleted.
	if err := s.record(ctx, tx, events.AccountDeleted{User: target.PublicID}, EventScope{}); err != nil {
		return err
	}
	if err := redactEmail(ctx, tx, target.Email, pseudonym); err != nil {
		return err
	}
	_, err := tx.NewDelete().Model((*account)(nil)).Where("id = ?", target.ID).Exec(ctx)
	return err
}

// redactEmail replaces email (as typed, lower-cased, or as its pseudonym,
// for events already minimised) with Redacted wherever an event keeps an
// address (redactions).
func redactEmail(ctx context.Context, tx bun.Tx, email string, pseudonym func(string) string) error {
	for _, q := range redactions(tx, email, pseudonym) {
		if _, err := q.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// redactions are erasure's updates, one per place an event keeps an
// address: the payload's email, with the name beside it; an admin's Events
// search (filters.email); an admin's search of a list (q), when it is the
// whole address. They run inside the deletion's transaction, under the
// request's timeout, so each finds its rows through a partial expression
// index (events_email_created_idx, events_search_email_idx,
// events_search_q_idx; TestErasureUsesItsIndexes) rather than reading
// every admin read in the trail.
func redactions(db bun.IDB, email string, pseudonym func(string) string) []*bun.UpdateQuery {
	matches := bun.In([]string{strings.ToLower(email), pseudonym(email)})
	searches := bun.In([]string{events.AdminEventListed{}.EventName(), events.AdminEventExported{}.EventName()})
	return []*bun.UpdateQuery{
		db.NewUpdate().Model((*eventRecord)(nil)).
			Set(`payload = e.payload || jsonb_build_object('email', ?::text) || CASE WHEN e.payload \? 'name' THEN jsonb_build_object('name', ?::text) ELSE '{}'::jsonb END`, Redacted, Redacted).
			Where(`e.payload \? 'email' AND lower(e.payload->>'email') IN (?)`, matches),
		db.NewUpdate().Model((*eventRecord)(nil)).
			Set(`payload = jsonb_set(e.payload, '{filters,email}', to_jsonb(?::text))`, Redacted).
			Where("e.name IN (?)", searches).
			Where(`e.payload->'filters' \? 'email' AND lower(e.payload #>> '{filters,email}') IN (?)`, matches),
		db.NewUpdate().Model((*eventRecord)(nil)).
			Set(`payload = jsonb_set(e.payload, '{q}', to_jsonb(?::text))`, Redacted).
			Where("e.name IN (?)", bun.In(events.AdminReadNames())).
			Where(`e.payload \? 'q' AND lower(e.payload->>'q') IN (?)`, matches),
	}
}
