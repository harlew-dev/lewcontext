package store

import (
	"context"
	"errors"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
)

// ErrWrongName refuses deleting a workspace whose current name isn't the one
// typed to confirm it.
var ErrWrongName = errors.New("workspace name doesn't match")

// canLeave is whether the member in m (workspace_user) may leave its
// workspace: anyone but an owner with no other owner. The canLeave field, the
// canLeave filter and DeleteAccount's last-owner check all use it, so the
// three can't disagree.
func canLeave(db bun.IDB) schema.QueryWithArgs {
	otherOwners := db.NewSelect().TableExpr("workspace_user AS o").ColumnExpr("1").
		Where("o.workspace_id = m.workspace_id AND o.role = ? AND o.user_id <> m.user_id", auth.RoleOwner)
	return bun.SafeQuery("(m.role <> ? OR EXISTS (?))", auth.RoleOwner, otherOwners)
}

// memberWorkspaces selects the workspaces userID belongs to, with their role
// and whether they may leave.
func (s *Store) memberWorkspaces(userID int64) *bun.SelectQuery {
	return s.DB.NewSelect().Model((*Workspace)(nil)).
		ColumnExpr("w.*").
		ColumnExpr("m.role").
		ColumnExpr("? AS can_leave", canLeave(s.DB)).
		Join("JOIN workspace_user AS m ON m.workspace_id = w.id").
		Where("m.user_id = ?", userID)
}

// WorkspaceFilter narrows a workspace listing; a nil CanLeave lists them all.
type WorkspaceFilter struct {
	CanLeave *bool
}

// IndexWorkspaces returns one page of the user's workspaces that match f,
// newest first.
func (s *Store) IndexWorkspaces(ctx context.Context, userID int64, f WorkspaceFilter, req PageRequest) (Page[Workspace], error) {
	q := s.memberWorkspaces(userID).OrderExpr("w.created_at DESC, w.id DESC")
	if f.CanLeave != nil {
		if *f.CanLeave {
			q = q.Where("?", canLeave(s.DB))
		} else {
			q = q.Where("NOT ?", canLeave(s.DB))
		}
	}
	return Paginate[Workspace](ctx, q, req)
}

// FindWorkspace loads the workspace through the user's membership, with
// their role. It returns sql.ErrNoRows when the workspace does not exist or
// the user is not a member, so callers can't tell the two apart.
func (s *Store) FindWorkspace(ctx context.Context, userID int64, publicID string) (Workspace, error) {
	var w Workspace
	err := s.memberWorkspaces(userID).Where("w.public_id = ?", publicID).Scan(ctx, &w)
	return w, err
}

// CreateWorkspace creates a workspace with the user as its only owner, who
// therefore can't leave it. It returns a QuotaError when the user already
// owns MaxOwnedWorkspaces.
func (s *Store) CreateWorkspace(ctx context.Context, userID int64, in WorkspaceInput) (Workspace, error) {
	w := Workspace{Name: in.Name}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := checkOwnedWorkspaces(ctx, tx, userID); err != nil {
			return err
		}
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

// DeleteWorkspace deletes the workspace, with its projects and memberships
// (ON DELETE CASCADE), when name is its current name (ErrWrongName), and
// records workspace.deleted. Its events stay, with no workspace
// (events.workspace_id is SET NULL). It takes the workspace lock first, so a
// membership change in flight finishes before, or finds the workspace gone.
// Callers check the member's permission and password first.
func (s *Store) DeleteWorkspace(ctx context.Context, w Workspace, name string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var current string
		if err := tx.NewSelect().Model((*Workspace)(nil)).Column("w.name").Where("w.id = ?", w.ID).For("UPDATE").Scan(ctx, &current); err != nil {
			return err
		}
		if current != name {
			return ErrWrongName
		}
		projects, err := tx.NewSelect().Model((*Project)(nil)).Where("p.workspace_id = ?", w.ID).Count(ctx)
		if err != nil {
			return err
		}
		members, err := tx.NewSelect().Model((*membership)(nil)).Where("m.workspace_id = ?", w.ID).Count(ctx)
		if err != nil {
			return err
		}
		e := events.WorkspaceDeleted{Workspace: w.PublicID, Name: current, Projects: int(projects), Members: int(members)}
		if err := s.record(ctx, tx, e, EventScope{WorkspaceID: w.ID}); err != nil {
			return err
		}
		_, err = tx.NewDelete().Model((*Workspace)(nil)).Where("w.id = ?", w.ID).Exec(ctx)
		return err
	})
}
