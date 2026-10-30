package store

import (
	"context"
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

// ProjectPage returns one page of the workspace's projects that match f.
func (s *Store) ProjectPage(ctx context.Context, workspaceID int64, f ProjectFilter, req PageRequest) (Page[Project], error) {
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

// Project returns sql.ErrNoRows when the project does not exist or belongs to another workspace.
func (s *Store) Project(ctx context.Context, workspaceID int64, publicID string) (Project, error) {
	var p Project
	err := s.DB.NewSelect().Model(&p).
		Where("p.public_id = ? AND p.workspace_id = ?", publicID, workspaceID).
		Scan(ctx)
	return p, err
}
func (s *Store) CreateProject(ctx context.Context, workspaceID int64, in ProjectInput) (Project, error) {
	p := Project{WorkspaceID: workspaceID, Name: in.Name, Description: in.Description, Status: in.Status, Color: in.Color}
	_, err := s.DB.NewInsert().Model(&p).Returning("*").Exec(ctx)
	return p, err
}

// UpdateProject returns sql.ErrNoRows when the project does not exist or belongs to another workspace.
func (s *Store) UpdateProject(ctx context.Context, workspaceID int64, publicID string, in ProjectInput) (Project, error) {
	p := Project{Name: in.Name, Description: in.Description, Status: in.Status, Color: in.Color}
	err := s.DB.NewUpdate().Model(&p).
		Column("name", "description", "status", "color").
		Set("updated_at = now()").
		Where("public_id = ? AND workspace_id = ?", publicID, workspaceID).
		Returning("*").
		Scan(ctx)
	return p, err
}
