package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// ApiProject is v1's stable boundary representation, independent of SPA DTOs.
type ApiProject struct {
	PublicID    string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func apiProject(p Project) ApiProject {
	return ApiProject{p.PublicID, p.Name, p.Description, p.Status, p.Color, p.CreatedAt, p.UpdatedAt}
}

// IndexApiProjects filters selection before counts, search, sort and pagination.
func (s *Store) IndexApiProjects(ctx context.Context, a ApiKeyAccess, f ProjectFilter, req PageRequest) (Page[ApiProject], error) {
	if !a.Allows("project.index") {
		return Page[ApiProject]{}, ErrApiKeyForbidden
	}
	order, ok := projectSorts[f.Sort]
	if !ok {
		order = projectSorts[DefaultProjectSort]
	}
	q := apiProjectQuery(s.DB, a).OrderExpr(order)
	if f.Search != "" {
		pattern := containsPattern(f.Search)
		q = q.Where(`(p.name ILIKE ? ESCAPE '\' OR p.description ILIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if f.Status != "" {
		q = q.Where("p.status = ?", f.Status)
	}
	rows, err := Paginate[Project](ctx, q, req)
	page := Page[ApiProject]{Data: []ApiProject{}, Pages: rows.Pages}
	for _, row := range rows.Data {
		page.Data = append(page.Data, apiProject(row))
	}
	return page, err
}

func (s *Store) FindApiProject(ctx context.Context, a ApiKeyAccess, id string) (ApiProject, error) {
	var p Project
	if !a.Allows("project.show") {
		return ApiProject{}, ErrApiKeyForbidden
	}
	err := apiProjectQuery(s.DB, a).Where("p.public_id = ?", id).Scan(ctx, &p)
	return apiProject(p), err
}

type apiIdempotencyRequest struct {
	bun.BaseModel                                    `bun:"table:api_idempotency_requests,alias:ir"`
	ID                                               int64 `bun:",pk,autoincrement"`
	WorkspaceID, ApiKeyID                            int64
	Operation, ValueHash, InputHash, ProjectPublicID string
	Response                                         json.RawMessage `bun:"type:jsonb"`
	CreatedAt, ExpiresAt                             time.Time
}

var (
	ErrApiReplayConflict = errors.New("retry conflicts with the original create")
	ErrApiReplayCapacity = errors.New("retry storage is busy")
)

// ApiProjectCreateLimit uses the caller's existing transaction and connection.
type ApiProjectCreateLimit struct {
	Key     string
	Maximum int
	Window  time.Duration
}
type ApiProjectLimited struct{ ResetsAt time.Time }

func (ApiProjectLimited) Error() string { return "project create limit reached" }

// CreateApiProject atomically authorizes, optionally replays, reserves allowance,
// creates and records its event. Header values are already digested by transport.
func (s *Store) CreateApiProject(ctx context.Context, a ApiKeyAccess, in ProjectInput, retryHash string, limit ApiProjectCreateLimit) (ApiProject, error) {
	var out ApiProject
	var limited *ApiProjectLimited
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		a, err = lockApiKeyAccess(ctx, tx, a, "project.store")
		if err != nil {
			return err
		}
		if a.row.ProjectAccess != "all" {
			return ErrApiKeyForbidden
		}
		canonical, err := json.Marshal(in)
		if err != nil {
			return err
		}
		inputHash := auth.Digest(string(canonical))
		if retryHash != "" {
			var replay apiIdempotencyRequest
			err := tx.NewSelect().Model(&replay).Where("ir.api_key_id = ? AND ir.workspace_id = ? AND ir.operation = ? AND ir.value_hash = ? AND ir.expires_at > clock_timestamp()", a.row.ID, a.workspace.ID, "project.store", retryHash).Scan(ctx)
			if err == nil {
				if replay.InputHash != inputHash {
					return ErrApiReplayConflict
				}
				exists, err := apiProjectQuery(tx, a).Where("p.public_id = ?", replay.ProjectPublicID).Exists(ctx)
				if err != nil {
					return err
				}
				if !exists {
					return ErrApiReplayConflict
				}
				return json.Unmarshal(replay.Response, &out)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			// Bounded indexed cleanup; an uncleaned full table cannot grow.
			ended := tx.NewSelect().Model((*apiIdempotencyRequest)(nil)).Column("ir.id").Where("ir.workspace_id = ? AND ir.expires_at <= clock_timestamp()", a.workspace.ID).OrderExpr("ir.id").Limit(5000)
			if _, err := tx.NewDelete().Model((*apiIdempotencyRequest)(nil)).Where("id IN (?)", ended).Exec(ctx); err != nil {
				return err
			}
			n, err := tx.NewSelect().Model((*apiIdempotencyRequest)(nil)).Where("ir.workspace_id = ?", a.workspace.ID).Count(ctx)
			if err != nil {
				return err
			}
			if n >= 5000 {
				return ErrApiReplayCapacity
			}
		}
		if err := checkWorkspaceProjects(ctx, tx, a.workspace.ID); err != nil {
			return err
		}
		if limit.Maximum > 0 {
			hits, end, err := countHit(ctx, tx, limit.Key, limit.Window)
			if err != nil {
				return err
			}
			if hits > limit.Maximum {
				limited = &ApiProjectLimited{end}
				if hits == limit.Maximum+1 {
					return s.record(ctx, tx, events.RateLimitReached{Limit: "api-key-project:per-workspace:block", Workspace: a.workspace.PublicID, Window: int(limit.Window.Seconds()), ResetsAt: end}, EventScope{WorkspaceID: a.workspace.ID})
				}
				return nil
			}
		}
		p, err := s.createProject(ctx, tx, a.workspace, in)
		if err != nil {
			return err
		}
		out = apiProject(p)
		if retryHash != "" {
			body, err := json.Marshal(out)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			replay := apiIdempotencyRequest{WorkspaceID: a.workspace.ID, ApiKeyID: a.row.ID, Operation: "project.store", ValueHash: retryHash, InputHash: inputHash, ProjectPublicID: p.PublicID, Response: body, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
			_, err = tx.NewInsert().Model(&replay).Exec(ctx)
			return err
		}
		return nil
	})
	if err == nil && limited != nil {
		return out, *limited
	}
	return out, err
}

func (s *Store) UpdateApiProject(ctx context.Context, a ApiKeyAccess, id string, in ProjectInput) (ApiProject, error) {
	var out Project
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		a, err = lockApiKeyAccess(ctx, tx, a, "project.update")
		if err != nil {
			return err
		}
		var before Project
		if err := apiProjectQuery(tx, a).Where("p.public_id = ?", id).For("UPDATE").Scan(ctx, &before); err != nil {
			return err
		}
		out, err = s.updateProject(ctx, tx, a.workspace, before, in)
		return err
	})
	return apiProject(out), err
}

func (s *Store) DeleteApiProject(ctx context.Context, a ApiKeyAccess, id string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		a, err = lockApiKeyAccess(ctx, tx, a, "project.destroy")
		if err != nil {
			return err
		}
		var p Project
		if err := apiProjectQuery(tx, a).Where("p.public_id = ?", id).For("UPDATE").Scan(ctx, &p); err != nil {
			return err
		}
		return s.deleteProject(ctx, tx, a.workspace, p)
	})
}

func (s *Store) createProject(ctx context.Context, tx bun.Tx, w Workspace, in ProjectInput) (Project, error) {
	p := Project{WorkspaceID: w.ID, Name: in.Name, Description: in.Description, Status: in.Status, Color: in.Color}
	if _, err := tx.NewInsert().Model(&p).Returning("*").Exec(ctx); err != nil {
		return p, err
	}
	err := s.record(ctx, tx, events.ProjectCreated{Project: p.PublicID, Workspace: w.PublicID, Name: p.Name, Description: p.Description, Status: p.Status, Color: p.Color}, EventScope{WorkspaceID: w.ID})
	return p, err
}

func (s *Store) updateProject(ctx context.Context, tx bun.Tx, w Workspace, before Project, in ProjectInput) (Project, error) {
	p := before
	p.Name, p.Description, p.Status, p.Color = in.Name, in.Description, in.Status, in.Color
	if _, err := tx.NewUpdate().Model(&p).Column("name", "description", "status", "color").Set("updated_at = clock_timestamp()").WherePK().Returning("*").Exec(ctx); err != nil {
		return p, err
	}
	changes := projectChanges(before, p)
	if len(changes) == 0 {
		return p, nil
	}
	return p, s.record(ctx, tx, events.ProjectUpdated{Project: p.PublicID, Workspace: w.PublicID, Changes: changes}, EventScope{WorkspaceID: w.ID})
}

func (s *Store) deleteProject(ctx context.Context, tx bun.Tx, w Workspace, p Project) error {
	if _, err := tx.NewDelete().Model(&p).WherePK().Exec(ctx); err != nil {
		return err
	}
	return s.record(ctx, tx, events.ProjectDeleted{Project: p.PublicID, Workspace: w.PublicID, Name: p.Name}, EventScope{WorkspaceID: w.ID})
}
