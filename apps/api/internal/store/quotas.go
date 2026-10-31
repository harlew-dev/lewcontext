package store

import (
	"context"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
)

// Quotas cap how many rows of a kind can exist at once: a rate limit slows
// growth, and only a quota stops it. Each is checked inside the insert's
// transaction under a row lock, so two creates at the cap can't both pass.
// Lowering one later keeps existing rows and refuses only new ones.
const (
	// MaxOwnedWorkspaces is how many workspaces a user may create and own.
	// Being made an owner by someone else isn't refused.
	MaxOwnedWorkspaces = 20
	// MaxWorkspaceProjects is how many projects a workspace may hold.
	MaxWorkspaceProjects = 1000
	// MaxWorkspaceMembers is how many members a workspace may have, counting
	// live invitations, which become members when accepted.
	MaxWorkspaceMembers = 100
)

// QuotaError refuses a create that would take What past Max. Callers answer
// 422 with a message of their own.
type QuotaError struct {
	What string
	Max  int
}

func (e QuotaError) Error() string { return fmt.Sprintf("at most %d %s", e.Max, e.What) }

// checkOwnedWorkspaces locks the user's row, so creates for one user count
// one at a time, then refuses when they already own MaxOwnedWorkspaces.
func checkOwnedWorkspaces(ctx context.Context, tx bun.Tx, userID int64) error {
	if _, err := tx.NewSelect().Model((*User)(nil)).Column("id").Where("id = ?", userID).For("UPDATE").Exec(ctx); err != nil {
		return err
	}
	owned, err := tx.NewSelect().Model((*membership)(nil)).Where("m.user_id = ? AND m.role = ?", userID, auth.RoleOwner).Count(ctx)
	if err != nil {
		return err
	}
	if owned >= MaxOwnedWorkspaces {
		return QuotaError{What: "owned workspaces", Max: MaxOwnedWorkspaces}
	}
	return nil
}

// checkWorkspaceProjects takes the workspace lock (as membership changes do),
// then refuses when the workspace already holds MaxWorkspaceProjects.
func checkWorkspaceProjects(ctx context.Context, tx bun.Tx, workspaceID int64) error {
	if _, err := tx.NewSelect().Model((*Workspace)(nil)).Column("w.id").Where("w.id = ?", workspaceID).For("UPDATE").Exec(ctx); err != nil {
		return err
	}
	projects, err := tx.NewSelect().Model((*Project)(nil)).Where("p.workspace_id = ?", workspaceID).Count(ctx)
	if err != nil {
		return err
	}
	if projects >= MaxWorkspaceProjects {
		return QuotaError{What: "projects in a workspace", Max: MaxWorkspaceProjects}
	}
	return nil
}

// checkWorkspaceMembers refuses when the workspace's members plus its live
// invitations, other than accepting (an invitation's ID, or 0), already reach
// MaxWorkspaceMembers. Accepting turns its invitation into a member, so it
// only fails for a workspace already over the quota. Call it under
// lockWorkspace.
func checkWorkspaceMembers(ctx context.Context, tx bun.Tx, workspaceID, accepting int64) error {
	members, err := tx.NewSelect().Model((*membership)(nil)).Where("m.workspace_id = ?", workspaceID).Count(ctx)
	if err != nil {
		return err
	}
	pending, err := tx.NewSelect().Model((*invitationRecord)(nil)).
		Where("i.workspace_id = ? AND i.expires_at > now() AND i.id <> ?", workspaceID, accepting).
		Count(ctx)
	if err != nil {
		return err
	}
	if members+pending >= MaxWorkspaceMembers {
		return QuotaError{What: "members and pending invitations in a workspace", Max: MaxWorkspaceMembers}
	}
	return nil
}
