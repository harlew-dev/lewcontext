package store

import (
	"context"
	"errors"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

var (
	// ErrLastOwner refuses a change that would leave a workspace without an owner.
	ErrLastOwner = errors.New("a workspace needs at least one owner")
	// ErrNotAssignable refuses acting on a member, or giving a role, outside
	// the actor's auth.Role.AssignableRoles.
	ErrNotAssignable = errors.New("role not assignable")
)

// Member is a user in a workspace, as owners and admins see them. ID is the
// user's public ID.
type Member struct {
	UserID   int64     `bun:"user_id" json:"-"`
	PublicID string    `bun:"public_id" json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     auth.Role `json:"role"`
	AddedAt  time.Time `bun:"added_at" json:"addedAt"`
}

// MemberFilter narrows a member listing; an empty Search lists everyone.
type MemberFilter struct {
	Search string
}

// membersOf selects the workspace's members with their accounts, scoped to
// the workspace first.
func membersOf(db bun.IDB, workspaceID int64) *bun.SelectQuery {
	return db.NewSelect().Model((*membership)(nil)).
		ColumnExpr("m.user_id, m.role, m.created_at AS added_at").
		ColumnExpr("u.public_id, u.name, u.email").
		Join("JOIN users AS u ON u.id = m.user_id").
		Where("m.workspace_id = ?", workspaceID)
}

// IndexMembers returns one page of the workspace's members, by role (most
// access first, in auth.Roles order), then name.
func (s *Store) IndexMembers(ctx context.Context, workspaceID int64, f MemberFilter, req PageRequest) (Page[Member], error) {
	roles := make([]string, len(auth.Roles))
	for i, role := range auth.Roles {
		roles[i] = string(role)
	}
	q := membersOf(s.DB, workspaceID).
		OrderExpr("array_position(?::text[], m.role), lower(u.name), u.id", pgdialect.Array(roles))
	if f.Search != "" {
		pattern := containsPattern(f.Search)
		q = q.Where(`(u.name ILIKE ? ESCAPE '\' OR u.email ILIKE ? ESCAPE '\')`, pattern, pattern)
	}
	return Paginate[Member](ctx, q, req)
}

// UpdateMemberRole gives the member with the user public ID memberID a new
// role, on behalf of actorID, and records workspace.member.updated when it
// changed. The actor's role is read again under the workspace lock; both the
// member's role and the new one must be among the roles it may assign
// (ErrNotAssignable), and the last owner can't be demoted (ErrLastOwner).
// sql.ErrNoRows: the member, or the actor, isn't in the workspace.
func (s *Store) UpdateMemberRole(ctx context.Context, w Workspace, actorID int64, memberID string, role auth.Role) (Member, error) {
	var member Member
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		actor, target, err := lockMembers(ctx, tx, w.ID, actorID, memberID)
		if err != nil {
			return err
		}
		member = target
		if !actor.Manages(target.Role) || !actor.Manages(role) {
			return ErrNotAssignable
		}
		if target.Role == role {
			return nil
		}
		if err := keepAnOwner(ctx, tx, w.ID, target.Role); err != nil {
			return err
		}
		_, err = tx.NewUpdate().Model((*membership)(nil)).
			Set("role = ?", role).
			Where("workspace_id = ? AND user_id = ?", w.ID, target.UserID).
			Exec(ctx)
		if err != nil {
			return err
		}
		member.Role = role
		changes := events.Changes{}
		changes.Add("role", string(target.Role), string(role))
		e := events.WorkspaceMemberUpdated{Workspace: w.PublicID, User: target.PublicID, Changes: changes}
		return s.record(ctx, tx, e, EventScope{WorkspaceID: w.ID, UserID: target.UserID})
	})
	return member, err
}

// RemoveMember takes the member with the user public ID memberID out of the
// workspace on behalf of actorID, with the same checks as UpdateMemberRole,
// and records workspace.member.removed. Their account and projects stay.
// Callers refuse actorID removing themselves (that's leaving).
func (s *Store) RemoveMember(ctx context.Context, w Workspace, actorID int64, memberID string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		actor, target, err := lockMembers(ctx, tx, w.ID, actorID, memberID)
		if err != nil {
			return err
		}
		if !actor.Manages(target.Role) {
			return ErrNotAssignable
		}
		if err := keepAnOwner(ctx, tx, w.ID, target.Role); err != nil {
			return err
		}
		if err := deleteMembership(ctx, tx, w.ID, target.UserID); err != nil {
			return err
		}
		e := events.WorkspaceMemberRemoved{Workspace: w.PublicID, User: target.PublicID, Role: string(target.Role)}
		return s.record(ctx, tx, e, EventScope{WorkspaceID: w.ID, UserID: target.UserID})
	})
}

// LeaveWorkspace removes userID's own membership and records
// workspace.member.left, unless they are its last owner (ErrLastOwner).
func (s *Store) LeaveWorkspace(ctx context.Context, w Workspace, userID int64) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockWorkspace(ctx, tx, w.ID); err != nil {
			return err
		}
		role, err := memberRole(ctx, tx, w.ID, userID)
		if err != nil {
			return err
		}
		if err := keepAnOwner(ctx, tx, w.ID, role); err != nil {
			return err
		}
		if err := deleteMembership(ctx, tx, w.ID, userID); err != nil {
			return err
		}
		e := events.WorkspaceMemberLeft{Workspace: w.PublicID, Role: string(role)}
		return s.record(ctx, tx, e, EventScope{WorkspaceID: w.ID, UserID: userID})
	})
}

// lockWorkspace locks the workspace row for the transaction. Every change to
// a workspace's memberships takes this lock first, so two of them can't both
// see "another owner exists" and leave none.
func lockWorkspace(ctx context.Context, tx bun.Tx, workspaceID int64) error {
	var id int64
	return tx.NewSelect().Model((*Workspace)(nil)).Column("w.id").Where("w.id = ?", workspaceID).For("UPDATE").Scan(ctx, &id)
}

// lockMembers locks the workspace, then reads the actor's current role and
// the member being acted on.
func lockMembers(ctx context.Context, tx bun.Tx, workspaceID, actorID int64, memberID string) (auth.Role, Member, error) {
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return "", Member{}, err
	}
	actor, err := memberRole(ctx, tx, workspaceID, actorID)
	if err != nil {
		return "", Member{}, err
	}
	var target Member
	err = membersOf(tx, workspaceID).Where("u.public_id = ?", memberID).Scan(ctx, &target)
	return actor, target, err
}

// memberRole is userID's role in the workspace, or sql.ErrNoRows.
func memberRole(ctx context.Context, tx bun.Tx, workspaceID, userID int64) (auth.Role, error) {
	var role auth.Role
	err := tx.NewSelect().Model((*membership)(nil)).Column("m.role").
		Where("m.workspace_id = ? AND m.user_id = ?", workspaceID, userID).
		Scan(ctx, &role)
	return role, err
}

// keepAnOwner refuses taking a member with role out of the owners when they
// are the only one. Call it under lockWorkspace.
func keepAnOwner(ctx context.Context, tx bun.Tx, workspaceID int64, role auth.Role) error {
	if role != auth.RoleOwner {
		return nil
	}
	owners, err := tx.NewSelect().Model((*membership)(nil)).
		Where("m.workspace_id = ? AND m.role = ?", workspaceID, auth.RoleOwner).
		Count(ctx)
	if err != nil {
		return err
	}
	if owners < 2 {
		return ErrLastOwner
	}
	return nil
}
func deleteMembership(ctx context.Context, tx bun.Tx, workspaceID, userID int64) error {
	_, err := tx.NewDelete().Model((*membership)(nil)).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Exec(ctx)
	return err
}
