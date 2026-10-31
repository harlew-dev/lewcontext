package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
)

// memberWorkspaces selects the workspaces userID belongs to, with their role
// and whether they may leave: anyone but an owner with no other owner.
func (s *Store) memberWorkspaces(userID int64) *bun.SelectQuery {
	otherOwners := s.DB.NewSelect().TableExpr("workspace_user AS o").ColumnExpr("1").
		Where("o.workspace_id = w.id AND o.role = ? AND o.user_id <> m.user_id", auth.RoleOwner)
	return s.DB.NewSelect().Model((*Workspace)(nil)).
		ColumnExpr("w.*").
		ColumnExpr("m.role").
		ColumnExpr("m.role <> ? OR EXISTS (?) AS can_leave", auth.RoleOwner, otherOwners).
		Join("JOIN workspace_user AS m ON m.workspace_id = w.id").
		Where("m.user_id = ?", userID)
}

// IndexWorkspaces returns one page of the user's workspaces, newest first.
func (s *Store) IndexWorkspaces(ctx context.Context, userID int64, req PageRequest) (Page[Workspace], error) {
	q := s.memberWorkspaces(userID).OrderExpr("w.created_at DESC, w.id DESC")
	return Paginate[Workspace](ctx, q, req)
}

// MemberWorkspace returns sql.ErrNoRows when the workspace does not exist or
// the user is not a member, so callers can't tell the two apart.
func (s *Store) MemberWorkspace(ctx context.Context, userID int64, publicID string) (Workspace, error) {
	var w Workspace
	err := s.memberWorkspaces(userID).Where("w.public_id = ?", publicID).Scan(ctx, &w)
	return w, err
}

// CreateWorkspace creates a workspace with the user as its only owner, who
// therefore can't leave it.
func (s *Store) CreateWorkspace(ctx context.Context, userID int64, in WorkspaceInput) (Workspace, error) {
	w := Workspace{Name: in.Name}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&w).Returning("*").Exec(ctx); err != nil {
			return err
		}
		m := membership{UserID: userID, WorkspaceID: w.ID, Role: auth.RoleOwner}
		if _, err := tx.NewInsert().Model(&m).Exec(ctx); err != nil {
			return err
		}
		e := events.WorkspaceCreated{Workspace: w.PublicID, Name: w.Name}
		return s.record(ctx, tx, e, EventScope{ActorID: userID, WorkspaceID: w.ID})
	})
	w.Role = auth.RoleOwner
	return w, err
}

// RenameWorkspace updates a workspace's name and records workspace.renamed
// when it changed. Callers check the member's permission first; the returned
// workspace has no Role or CanLeave.
func (s *Store) RenameWorkspace(ctx context.Context, id int64, in WorkspaceInput) (Workspace, error) {
	w := Workspace{Name: in.Name}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var before string
		if err := tx.NewSelect().Model((*Workspace)(nil)).Column("name").Where("id = ?", id).For("UPDATE").Scan(ctx, &before); err != nil {
			return err
		}
		err := tx.NewUpdate().Model(&w).
			Column("name").
			Set("updated_at = now()").
			Where("id = ?", id).
			Returning("*").
			Scan(ctx)
		if err != nil {
			return err
		}
		changes := events.Changes{}
		changes.Add("name", before, w.Name)
		if len(changes) == 0 {
			return nil
		}
		return s.record(ctx, tx, events.WorkspaceRenamed{Workspace: w.PublicID, Changes: changes}, EventScope{WorkspaceID: id})
	})
	return w, err
}
