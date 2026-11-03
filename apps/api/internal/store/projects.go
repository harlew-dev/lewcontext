package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"strings"
)

// DefaultProjectSort lists the most recently updated projects first.
const DefaultProjectSort = "-updated"

// statusOrder sorts statuses in workflow order rather than alphabetically.
const statusOrder = "CASE p.status WHEN 'Planned' THEN 1 WHEN 'In progress' THEN 2 ELSE 3 END"

// projectSorts is the allowlist of sort keys ("-" prefix for descending). The
// values are fixed SQL and every order ends in the primary key, so equal values
// keep their position between pages. The web app mirrors the keys in
// src/types/projects.ts.
var projectSorts = map[string]string{
	"updated":  "p.updated_at ASC, p.id ASC",
	"-updated": "p.updated_at DESC, p.id DESC",
	"created":  "p.created_at ASC, p.id ASC",
	"-created": "p.created_at DESC, p.id DESC",
	"name":     "lower(p.name) ASC, p.id ASC",
	"-name":    "lower(p.name) DESC, p.id DESC",
	"status":   statusOrder + " ASC, p.updated_at DESC, p.id DESC",
	"-status":  statusOrder + " DESC, p.updated_at DESC, p.id DESC",
}

// ProjectSortable reports whether key is an allowed project sort.
func ProjectSortable(key string) bool {
	_, ok := projectSorts[key]
	return ok
}

// ProjectFilter narrows a project listing. Fields are validated by httpapi;
// empty Search or Status means no filter.
type ProjectFilter struct {
	Search string
	Status string
	Sort   string
}

// IndexProjects returns one page of the workspace's projects that match f.
func (s *Store) IndexProjects(ctx context.Context, workspaceID int64, f ProjectFilter, req PageRequest) (Page[Project], error) {
	order, ok := projectSorts[f.Sort]
	if !ok {
		order = projectSorts[DefaultProjectSort]
	}
	q := s.DB.NewSelect().Model((*Project)(nil)).
		Where("p.workspace_id = ?", workspaceID).
		OrderExpr(order)
	if f.Search != "" {
		pattern := containsPattern(f.Search)
		q = q.Where(`(p.name ILIKE ? ESCAPE '\' OR p.description ILIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if f.Status != "" {
		q = q.Where("p.status = ?", f.Status)
	}
	return Paginate[Project](ctx, q, req)
}

// containsPattern matches term anywhere, treating LIKE wildcards in it literally.
func containsPattern(term string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
	return "%" + escaped + "%"
}

// FindProject returns sql.ErrNoRows when the project does not exist or belongs to another workspace.
func (s *Store) FindProject(ctx context.Context, workspaceID int64, publicID string) (Project, error) {
	var p Project
	err := s.DB.NewSelect().Model(&p).
		Where("p.public_id = ? AND p.workspace_id = ?", publicID, workspaceID).
		Scan(ctx)
	return p, err
}

// CreateProject creates a project in the workspace and records
// project.created. It returns a QuotaError when the workspace already holds
// MaxWorkspaceProjects.
func (s *Store) CreateProject(ctx context.Context, w Workspace, in ProjectInput) (Project, error) {
	p := Project{WorkspaceID: w.ID, Name: in.Name, Description: in.Description, Status: in.Status, Color: in.Color}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockBrowserProjectWrite(ctx, tx, w.ID, auth.ProjectStore); err != nil {
			return err
		}
		if err := checkWorkspaceProjects(ctx, tx, w.ID); err != nil {
			return err
		}
		var err error
		p, err = s.createProject(ctx, tx, w, in)
		return err
	})
	return p, err
}

// UpdateProject saves the project's fields and records project.updated with
// the fields that changed, if any. It returns sql.ErrNoRows when the project
// does not exist or belongs to another workspace.
func (s *Store) UpdateProject(ctx context.Context, w Workspace, publicID string, in ProjectInput) (Project, error) {
	p := Project{Name: in.Name, Description: in.Description, Status: in.Status, Color: in.Color}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockBrowserProjectWrite(ctx, tx, w.ID, auth.ProjectUpdate); err != nil {
			return err
		}
		var before Project
		err := tx.NewSelect().Model(&before).
			Where("p.public_id = ? AND p.workspace_id = ?", publicID, w.ID).
			For("UPDATE").
			Scan(ctx)
		if err != nil {
			return err
		}
		p, err = s.updateProject(ctx, tx, w, before, in)
		return err
	})
	return p, err
}

// DeleteProject deletes the project and records project.deleted. It returns
// sql.ErrNoRows when the project does not exist or belongs to another
// workspace.
func (s *Store) DeleteProject(ctx context.Context, w Workspace, publicID string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockBrowserProjectWrite(ctx, tx, w.ID, auth.ProjectDestroy); err != nil {
			return err
		}
		var p Project
		err := tx.NewSelect().Model(&p).
			Where("p.public_id = ? AND p.workspace_id = ?", publicID, w.ID).
			For("UPDATE").Scan(ctx)
		if err != nil {
			return err
		}
		return s.deleteProject(ctx, tx, w, p)
	})
}

// lockBrowserProjectWrite shares v1's commit boundary without inventing a service
// membership. The trusted request actor is re-read after acquiring its row lock.
func lockBrowserProjectWrite(ctx context.Context, tx bun.Tx, workspaceID int64, permission auth.Permission) error {
	if err := database.LockWorkspaceGraph(ctx, tx, false); err != nil {
		return err
	}
	actor := events.Actor(ctx)
	if actor == 0 {
		return ErrApiKeyForbidden
	}
	var u User
	if err := tx.NewSelect().Model(&u).Where("u.id = ?", actor).For("NO KEY UPDATE").Scan(ctx); err != nil {
		return err
	}
	if err := checkNotSuspended(ctx, tx, actor); err != nil {
		return err
	}
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return err
	}
	role, err := memberRole(ctx, tx, workspaceID, actor)
	if err != nil {
		return err
	}
	if !role.Can(permission) {
		return ErrApiKeyForbidden
	}
	return nil
}

// projectChanges lists the editable fields that differ between before and after.
func projectChanges(before, after Project) events.Changes {
	changes := events.Changes{}
	changes.Add("name", before.Name, after.Name)
	changes.Add("description", before.Description, after.Description)
	changes.Add("status", before.Status, after.Status)
	changes.Add("color", before.Color, after.Color)
	return changes
}
