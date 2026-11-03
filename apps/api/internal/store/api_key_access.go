package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/database"
	"slices"
	"time"
)

// ApiKeyAccess is a validated credential principal, never a session/membership.
// Its private fields prevent callers constructing an unscoped zero-value grant.
type ApiKeyAccess struct {
	row             apiKeyRecord
	workspace       Workspace
	owner           User
	requireVerified bool
}

func (a ApiKeyAccess) PublicID() string          { return a.row.PublicID }
func (a ApiKeyAccess) WorkspacePublicID() string { return a.workspace.PublicID }
func (a ApiKeyAccess) WorkspaceID() int64        { return a.workspace.ID }
func (a ApiKeyAccess) OwnerID() int64            { return a.row.OwnerUserID }
func (a ApiKeyAccess) OwnerPublicID() string     { return a.owner.PublicID }
func (a ApiKeyAccess) Allows(scope string) bool {
	if a.row.ID == 0 || !slices.Contains(auth.ApiKeyScopes, scope) || !slices.Contains(a.row.Scopes, scope) {
		return false
	}
	switch a.row.Kind {
	case auth.ApiKeyWorkspace:
		return true
	case auth.ApiKeyPersonal:
		return auth.ApiKeyRoleAllows(a.workspace.Role, scope)
	default:
		return false
	}
}

func checkApiKeyOwner(ctx context.Context, db bun.IDB, row apiKeyRecord, required, lock bool) (User, auth.Role, error) {
	var u User
	if row.Kind != auth.ApiKeyPersonal {
		return u, "", nil
	}
	q := db.NewSelect().Model(&u).Where("u.id = ?", row.OwnerUserID)
	if lock {
		q = q.For("NO KEY UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return u, "", err
	}
	if err := checkNotSuspended(ctx, db, u.ID); err != nil {
		return u, "", err
	}
	if required && u.EmailVerifiedAt == nil {
		return u, "", ErrApiKeyInvalid
	}
	var role auth.Role
	err := db.NewSelect().Model((*membership)(nil)).Column("m.role").Where("m.workspace_id = ? AND m.user_id = ?", row.WorkspaceID, u.ID).Scan(ctx, &role)
	if err == nil && role.Rank() == 0 {
		err = ErrApiKeyInvalid
	}
	return u, role, err
}

// FindApiKeyAccess authenticates once from PostgreSQL; there is no positive cache.
func (s *Store) FindApiKeyAccess(ctx context.Context, value string, requireVerified bool) (ApiKeyAccess, error) {
	kind, id, valid := auth.ParseApiKey(value)
	if !valid {
		auth.CheckApiKey(value, "")
		return ApiKeyAccess{}, ErrApiKeyInvalid
	}
	var row apiKeyRecord
	err := s.DB.NewSelect().Model(&row).Where("ak.public_id = ?", id).Scan(ctx)
	matched := auth.CheckApiKey(value, row.TokenHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ApiKeyAccess{}, err
	}
	if err != nil || !matched || row.Kind != kind || row.RevokedAt != nil || (row.ExpiresAt != nil && !row.ExpiresAt.After(time.Now())) || (row.ProjectAccess != "all" && row.ProjectAccess != "selected") {
		return ApiKeyAccess{}, ErrApiKeyInvalid
	}
	for _, scope := range row.Scopes {
		if !slices.Contains(auth.ApiKeyScopes, scope) {
			return ApiKeyAccess{}, ErrApiKeyInvalid
		}
	}
	if len(row.Scopes) == 0 {
		return ApiKeyAccess{}, ErrApiKeyInvalid
	}
	var w Workspace
	if err := s.DB.NewSelect().Model(&w).Where("w.id = ?", row.WorkspaceID).Scan(ctx); err != nil {
		return ApiKeyAccess{}, err
	}
	u, role, err := checkApiKeyOwner(ctx, s.DB, row, requireVerified, false)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrSuspended) {
			err = ErrApiKeyInvalid
		}
		return ApiKeyAccess{}, err
	}
	w.Role = role
	return ApiKeyAccess{row: row, workspace: w, owner: u, requireVerified: requireVerified}, nil
}

// UpdateApiKeyLastUse is deliberately conditional and outside business transactions.
func (s *Store) UpdateApiKeyLastUse(ctx context.Context, a ApiKeyAccess) error {
	if a.row.LastUsedAt != nil && time.Since(*a.row.LastUsedAt) < time.Hour {
		return nil
	}
	_, err := s.DB.NewUpdate().Model((*apiKeyRecord)(nil)).Set("last_used_at = clock_timestamp()").
		Where("id = ? AND revoked_at IS NULL AND (last_used_at IS NULL OR last_used_at < clock_timestamp() - interval '1 hour')", a.row.ID).Exec(ctx)
	return err
}

func apiProjectQuery(db bun.IDB, a ApiKeyAccess) *bun.SelectQuery {
	q := db.NewSelect().Model((*Project)(nil)).Where("p.workspace_id = ?", a.workspace.ID)
	if a.row.ID == 0 {
		return q.Where("FALSE")
	}
	switch a.row.ProjectAccess {
	case "all":
		return q
	case "selected":
		selected := db.NewSelect().Model((*apiKeyProject)(nil)).ColumnExpr("1").Where("kp.api_key_id = ? AND kp.workspace_id = p.workspace_id AND kp.project_id = p.id", a.row.ID)
		return q.Where("EXISTS (?)", selected)
	default:
		return q.Where("FALSE")
	}
}

// CheckApiProjectScope is checked before action-specific details are returned.
func (s *Store) CheckApiProjectScope(ctx context.Context, a ApiKeyAccess, id string) (bool, error) {
	return apiProjectQuery(s.DB, a).Where("p.public_id = ?", id).Exists(ctx)
}

func lockApiKeyAccess(ctx context.Context, tx bun.Tx, a ApiKeyAccess, scope string) (ApiKeyAccess, error) {
	if err := database.LockWorkspaceGraph(ctx, tx, false); err != nil {
		return a, err
	}
	if a.row.ID == 0 {
		return a, ErrApiKeyInvalid
	}
	// Owner is immutable, so the admitted identity determines the lock order.
	if a.row.Kind == auth.ApiKeyPersonal {
		if _, _, err := checkApiKeyOwner(ctx, tx, a.row, a.requireVerified, true); err != nil {
			return a, ErrApiKeyInvalid
		}
	}
	if err := lockWorkspace(ctx, tx, a.workspace.ID); err != nil {
		return a, err
	}
	if err := tx.NewSelect().Model(&a.row).Where("ak.id = ? AND ak.workspace_id = ?", a.row.ID, a.workspace.ID).For("UPDATE").Scan(ctx); err != nil {
		return a, ErrApiKeyInvalid
	}
	if a.row.RevokedAt != nil || a.row.TokenHash == "" || (a.row.ExpiresAt != nil && !a.row.ExpiresAt.After(time.Now())) {
		return a, ErrApiKeyInvalid
	}
	u, role, err := checkApiKeyOwner(ctx, tx, a.row, a.requireVerified, false)
	if err != nil {
		return a, ErrApiKeyInvalid
	}
	a.owner, a.workspace.Role = u, role
	if !a.Allows(scope) {
		return a, ErrApiKeyForbidden
	}
	return a, nil
}
