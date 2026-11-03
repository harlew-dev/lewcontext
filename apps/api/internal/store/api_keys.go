package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/database"
)

var (
	ErrApiKeyInvalid   = errors.New("invalid credential")
	ErrApiKeyForbidden = errors.New("credential action is not allowed")
	ErrApiKeyEnded     = errors.New("credential has ended")
	ErrApiKeyGrants    = errors.New("invalid credential grants")
	ErrApiKeyExpiry    = errors.New("credential expiry cannot be extended")
	ErrApiKeyProof     = errors.New("incorrect account proof")
)

const (
	MaxWorkspaceApiKeys = 100
	MaxPersonalApiKeys  = 50
)

type apiKeyRecord struct {
	bun.BaseModel `bun:"table:api_keys,alias:ak"`
	ID            int64  `bun:",pk,autoincrement"`
	PublicID      string `bun:",nullzero"`
	WorkspaceID   int64
	Kind          auth.ApiKeyKind
	OwnerUserID   int64 `bun:",nullzero"`
	CreatedBy     int64 `bun:",nullzero"`
	Name          string
	TokenHash     string `bun:",nullzero"`
	ProjectAccess string
	Scopes        []string  `bun:",array"`
	CreatedAt     time.Time `bun:",nullzero,default:current_timestamp"`
	UpdatedAt     time.Time `bun:",nullzero,default:current_timestamp"`
	ExpiresAt     *time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
}

// ApiKeyPerson never includes an address or internal identifier.
type ApiKeyPerson struct {
	PublicID string `json:"id"`
	Name     string `json:"name"`
}

// ApiKey is management metadata only. Secrets and digests have no DTO field.
type ApiKey struct {
	PublicID        string           `json:"id"`
	Kind            auth.ApiKeyKind  `json:"kind"`
	Name            string           `json:"name"`
	Workspace       WorkspaceSummary `json:"workspace"`
	Owner           *ApiKeyPerson    `json:"owner"`
	CreatedBy       *ApiKeyPerson    `json:"createdBy"`
	ProjectAccess   string           `json:"projectAccess"`
	Projects        []ApiKeyPerson   `json:"projects"`
	Scopes          []string         `json:"scopes"`
	EffectiveScopes []string         `json:"effectiveScopes"`
	CreatedAt       time.Time        `json:"createdAt"`
	ExpiresAt       *time.Time       `json:"expiresAt"`
	LastUsedAt      *time.Time       `json:"lastUsedAt"`
	RevokedAt       *time.Time       `json:"revokedAt"`
}

// ApiKeyInput replaces the editable configuration; expiry is explicit, including null.
type ApiKeyInput struct {
	Name          string     `json:"name"`
	ProjectAccess string     `json:"projectAccess"`
	Projects      []string   `json:"projects"`
	Scopes        []string   `json:"scopes"`
	ExpiresAt     *time.Time `json:"expiresAt"`
}

// ApiKeyManager binds a management operation to its live initiating session.
// WorkspaceID zero means own-personal management; otherwise permissions apply.
type ApiKeyManager struct {
	UserID          int64
	SessionHash     string
	WorkspaceID     int64
	RequireVerified bool
}

// ApiKeyProof is checked against current locked account/MFA state, never a prior verify call.
type ApiKeyProof struct {
	PasswordConfirmation
	ExpectedMFA bool
	Checked     *bool
	Confirmed   *PasswordConfirmed
}

// ApiKeyFilter scopes metadata to one workspace or the person's own keys.
type ApiKeyFilter struct {
	WorkspaceID, OwnerID int64
	Kind, Search         string
	// Owner (usr_…) narrows a workspace's list to that member's personal keys.
	Owner string
}

func liveApiKeys(q *bun.SelectQuery) *bun.SelectQuery {
	return q.Where("ak.revoked_at IS NULL AND (ak.expires_at IS NULL OR ak.expires_at > clock_timestamp())")
}

func apiKeyMetadata(ctx context.Context, db bun.IDB, row apiKeyRecord) (ApiKey, error) {
	key := ApiKey{PublicID: row.PublicID, Kind: row.Kind, Name: row.Name, ProjectAccess: row.ProjectAccess, Scopes: slices.Clone(row.Scopes), EffectiveScopes: []string{}, Projects: []ApiKeyPerson{}, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, LastUsedAt: row.LastUsedAt, RevokedAt: row.RevokedAt}
	err := db.NewSelect().Model((*Workspace)(nil)).Column("w.public_id", "w.name").Where("w.id = ?", row.WorkspaceID).Scan(ctx, &key.Workspace)
	if err != nil {
		return key, err
	}
	for _, p := range []struct {
		id   int64
		into **ApiKeyPerson
	}{{row.OwnerUserID, &key.Owner}, {row.CreatedBy, &key.CreatedBy}} {
		if p.id == 0 {
			continue
		}
		var person ApiKeyPerson
		err := db.NewSelect().Model((*User)(nil)).Column("u.public_id", "u.name").Where("u.id = ?", p.id).Scan(ctx, &person)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return key, err
		}
		if err == nil {
			*p.into = &person
		}
	}
	err = db.NewSelect().Model((*Project)(nil)).Column("p.public_id", "p.name").
		Join("JOIN api_key_projects AS kp ON kp.project_id = p.id AND kp.workspace_id = p.workspace_id").
		Where("kp.api_key_id = ? AND p.workspace_id = ?", row.ID, row.WorkspaceID).OrderExpr("p.id").Scan(ctx, &key.Projects)
	if err != nil {
		return key, err
	}
	if row.Kind == auth.ApiKeyWorkspace {
		key.EffectiveScopes = slices.Clone(row.Scopes)
	} else {
		var role auth.Role
		err := db.NewSelect().Model((*membership)(nil)).Column("m.role").Where("m.user_id = ? AND m.workspace_id = ?", row.OwnerUserID, row.WorkspaceID).Scan(ctx, &role)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return key, err
		}
		for _, scope := range row.Scopes {
			if auth.ApiKeyRoleAllows(role, scope) {
				key.EffectiveScopes = append(key.EffectiveScopes, scope)
			}
		}
	}
	return key, nil
}

// IndexApiKeys returns bounded, newest-first metadata within the authorized scope.
func (s *Store) IndexApiKeys(ctx context.Context, f ApiKeyFilter, req PageRequest) (Page[ApiKey], error) {
	q := s.DB.NewSelect().Model((*apiKeyRecord)(nil)).OrderExpr("ak.created_at DESC, ak.id DESC")
	if f.WorkspaceID != 0 {
		q = q.Where("ak.workspace_id = ?", f.WorkspaceID)
	} else if f.OwnerID != 0 {
		q = q.Where("ak.owner_user_id = ? AND ak.kind = ?", f.OwnerID, auth.ApiKeyPersonal)
	} else {
		return Page[ApiKey]{}, ErrApiKeyForbidden
	}
	if f.Kind != "" {
		q = q.Where("ak.kind = ?", f.Kind)
	}
	if f.Search != "" {
		q = q.Where(`ak.name ILIKE ? ESCAPE '\'`, containsPattern(f.Search))
	}
	if f.Owner != "" {
		q = q.Where("ak.owner_user_id IN (?)", s.DB.NewSelect().Model((*User)(nil)).Column("u.id").Where("u.public_id = ?", f.Owner))
	}
	rows, err := Paginate[apiKeyRecord](ctx, q, req)
	page := Page[ApiKey]{Data: []ApiKey{}, Pages: rows.Pages}
	if err != nil {
		return page, err
	}
	for _, row := range rows.Data {
		key, err := apiKeyMetadata(ctx, s.DB, row)
		if err != nil {
			return page, err
		}
		page.Data = append(page.Data, key)
	}
	return page, nil
}

func lockApiKeyManager(ctx context.Context, tx bun.Tx, manager ApiKeyManager, workspaceID int64, permission auth.Permission) (account, Workspace, error) {
	a, err := lockEmailChangeAccount(ctx, tx, manager.UserID, manager.SessionHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrSuspended) {
			err = ErrApiKeyInvalid
		}
		return a, Workspace{}, err
	}
	if manager.RequireVerified && a.EmailVerifiedAt == nil {
		return a, Workspace{}, ErrApiKeyInvalid
	}
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return a, Workspace{}, err
	}
	if err := checkApiKeyManagementSession(ctx, tx, manager); err != nil {
		return a, Workspace{}, err
	}
	// A scalar MFA subquery in the initial locking SELECT can have been
	// evaluated before that SELECT waited. Read it again under the user lock.
	if err := tx.NewSelect().Model((*User)(nil)).ColumnExpr("(?)", mfaEnabledAt(tx)).Where("u.id = ?", manager.UserID).Scan(ctx, &a.MFAEnabledAt); err != nil {
		return a, Workspace{}, err
	}
	var w Workspace
	err = tx.NewSelect().Model(&w).Where("w.id = ?", workspaceID).Scan(ctx)
	if err == nil {
		w.Role, err = memberRole(ctx, tx, w.ID, manager.UserID)
	}
	if err == nil && (w.Role.Rank() == 0 || (manager.WorkspaceID != 0 && !w.Role.Can(permission))) {
		err = ErrApiKeyForbidden
	}
	return a, w, err
}

func checkApiKeyManagementSession(ctx context.Context, tx bun.Tx, manager ApiKeyManager) error {
	valid, err := tx.NewSelect().Model((*sessionRecord)(nil)).Where("s.user_id = ? AND s.token_hash = ? AND s.expires_at > clock_timestamp() AND s.last_seen_at > clock_timestamp() - interval '30 minutes'", manager.UserID, manager.SessionHash).Exists(ctx)
	if err != nil {
		return err
	}
	if !valid {
		return ErrApiKeyInvalid
	}
	return nil
}

func checkApiKeyProof(ctx context.Context, tx bun.Tx, a account, proof ApiKeyProof) error {
	if (a.MFAEnabledAt != nil) != proof.ExpectedMFA {
		return ErrApiKeyProof
	}
	if proof.Checked != nil {
		*proof.Checked = true
	}
	used, err := useConfirmedFactor(ctx, tx, a.ID, proof.Factor)
	if proof.Confirmed != nil {
		proof.Confirmed.MFA = !errors.Is(err, ErrMFANotEnabled)
		if used != nil {
			proof.Confirmed.FactorUsed = *used
		}
	}
	if errors.Is(err, ErrMFANotEnabled) {
		if proof.Password == nil {
			return ErrApiKeyProof
		}
		return proof.Password(a.PasswordHash)
	}
	return err
}

func checkApiKeyScopes(in ApiKeyInput, kind auth.ApiKeyKind, role auth.Role) error {
	if len(in.Scopes) == 0 || len(in.Scopes) > len(auth.ApiKeyScopes) {
		return ErrApiKeyGrants
	}
	seen := map[string]bool{}
	for _, scope := range in.Scopes {
		if seen[scope] || !slices.Contains(auth.ApiKeyScopes, scope) || (kind == auth.ApiKeyPersonal && !auth.ApiKeyRoleAllows(role, scope)) {
			return ErrApiKeyGrants
		}
		seen[scope] = true
	}
	if in.ProjectAccess != "all" && in.ProjectAccess != "selected" {
		return ErrApiKeyGrants
	}
	if in.ProjectAccess == "all" && len(in.Projects) != 0 {
		return ErrApiKeyGrants
	}
	if in.ProjectAccess == "selected" && (len(in.Projects) == 0 || len(in.Projects) > 100 || seen["project.store"]) {
		return ErrApiKeyGrants
	}
	return nil
}

// CreateApiKey issues a creation-only secret with atomic proof, quota and grants.
func (s *Store) CreateApiKey(ctx context.Context, manager ApiKeyManager, workspace string, in ApiKeyInput, proof ApiKeyProof) (ApiKey, string, error) {
	var out ApiKey
	var secret string
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockWorkspaceGraph(ctx, tx, false); err != nil {
			return err
		}
		// Resolve only the ID before locking; current authority is checked below.
		var wid int64
		if err := tx.NewSelect().Model((*Workspace)(nil)).Column("w.id").Where("w.public_id = ?", workspace).Scan(ctx, &wid); err != nil {
			return err
		}
		if manager.WorkspaceID != 0 && manager.WorkspaceID != wid {
			return sql.ErrNoRows
		}
		a, w, err := lockApiKeyManager(ctx, tx, manager, wid, auth.WorkspaceApiKeyStore)
		if err != nil {
			return err
		}
		kind, owner := auth.ApiKeyWorkspace, int64(0)
		if manager.WorkspaceID == 0 {
			kind, owner = auth.ApiKeyPersonal, a.ID
		}
		if err := checkApiKeyScopes(in, kind, w.Role); err != nil {
			return err
		}
		now := time.Now()
		if in.ExpiresAt != nil && (in.ExpiresAt.Before(now.Add(time.Hour)) || in.ExpiresAt.After(now.Add(365*24*time.Hour))) {
			return ErrApiKeyExpiry
		}
		count, err := liveApiKeys(tx.NewSelect().Model((*apiKeyRecord)(nil)).Where("ak.workspace_id = ?", w.ID)).Count(ctx)
		if err != nil {
			return err
		}
		if count >= MaxWorkspaceApiKeys {
			return QuotaError{What: "live API keys per workspace", Max: MaxWorkspaceApiKeys}
		}
		if owner != 0 {
			count, err = liveApiKeys(tx.NewSelect().Model((*apiKeyRecord)(nil)).Where("ak.owner_user_id = ?", owner)).Count(ctx)
			if err != nil {
				return err
			}
			if count >= MaxPersonalApiKeys {
				return QuotaError{What: "live personal API keys", Max: MaxPersonalApiKeys}
			}
		}
		if err := checkApiKeyProof(ctx, tx, a, proof); err != nil {
			return err
		}
		// A random temporary digest is unobservable outside this transaction.
		temporary, err := auth.Token()
		if err != nil {
			return err
		}
		row := apiKeyRecord{WorkspaceID: w.ID, Kind: kind, OwnerUserID: owner, CreatedBy: a.ID, Name: in.Name, TokenHash: auth.ApiKeyDigest(temporary), ProjectAccess: in.ProjectAccess, Scopes: in.Scopes, CreatedAt: now, UpdatedAt: now, ExpiresAt: in.ExpiresAt}
		if _, err := tx.NewInsert().Model(&row).Returning("*").Exec(ctx); err != nil {
			return err
		}
		secret, row.TokenHash, err = auth.NewApiKey(kind, row.PublicID)
		if err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model(&row).Column("token_hash").WherePK().Exec(ctx); err != nil {
			return err
		}
		if err := replaceApiKeyProjects(ctx, tx, row, in.Projects); err != nil {
			return err
		}
		if err := s.recordApiKey(ctx, tx, row, w.PublicID, "created", "", nil); err != nil {
			return err
		}
		out, err = apiKeyMetadata(ctx, tx, row)
		return err
	})
	if err != nil {
		secret = ""
	}
	return out, secret, err
}

func findManagedApiKey(ctx context.Context, db bun.IDB, manager ApiKeyManager, id string) (apiKeyRecord, error) {
	var row apiKeyRecord
	q := db.NewSelect().Model(&row).Where("ak.public_id = ?", id)
	if manager.WorkspaceID != 0 {
		q = q.Where("ak.workspace_id = ?", manager.WorkspaceID)
	} else {
		q = q.Where("ak.kind = ? AND ak.owner_user_id = ?", auth.ApiKeyPersonal, manager.UserID)
	}
	err := q.Scan(ctx)
	return row, err
}

// UpdateApiKey changes editable grants, requiring proof for every broadening.
func (s *Store) UpdateApiKey(ctx context.Context, manager ApiKeyManager, id string, in ApiKeyInput, proof ApiKeyProof) (ApiKey, error) {
	var out ApiKey
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockWorkspaceGraph(ctx, tx, false); err != nil {
			return err
		}
		row, err := findManagedApiKey(ctx, tx, manager, id)
		if err != nil {
			return err
		}
		a, w, err := lockApiKeyManager(ctx, tx, manager, row.WorkspaceID, auth.WorkspaceApiKeyUpdate)
		if err != nil {
			return err
		}
		if err := tx.NewSelect().Model(&row).Where("ak.public_id = ?", id).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		if manager.WorkspaceID != 0 && row.Kind != auth.ApiKeyWorkspace {
			return ErrApiKeyForbidden
		}
		if err := checkApiKeyManagementSession(ctx, tx, manager); err != nil {
			return err
		}
		if row.RevokedAt != nil || (row.ExpiresAt != nil && !row.ExpiresAt.After(time.Now())) {
			return ErrApiKeyEnded
		}
		if err := checkApiKeyScopes(in, row.Kind, w.Role); err != nil {
			return err
		}
		if row.ExpiresAt != nil && (in.ExpiresAt == nil || in.ExpiresAt.After(*row.ExpiresAt)) {
			return ErrApiKeyExpiry
		}
		if in.ExpiresAt != nil && (!in.ExpiresAt.After(time.Now()) || in.ExpiresAt.Before(row.CreatedAt.Add(time.Hour)) || in.ExpiresAt.After(row.CreatedAt.Add(365*24*time.Hour))) {
			return ErrApiKeyExpiry
		}
		before, err := apiKeyMetadata(ctx, tx, row)
		if err != nil {
			return err
		}
		broadens := row.ProjectAccess == "selected" && in.ProjectAccess == "all"
		for _, scope := range in.Scopes {
			broadens = broadens || !slices.Contains(row.Scopes, scope)
		}
		if row.ProjectAccess == "selected" && in.ProjectAccess == "selected" {
			for _, project := range in.Projects {
				broadens = broadens || !slices.ContainsFunc(before.Projects, func(p ApiKeyPerson) bool { return p.PublicID == project })
			}
		}
		if broadens {
			if err := checkApiKeyManagementSession(ctx, tx, manager); err != nil {
				return err
			}
			if err := checkApiKeyProof(ctx, tx, a, proof); err != nil {
				return err
			}
		}
		fields := []string{}
		if row.Name != in.Name {
			fields = append(fields, "name")
		}
		if row.ProjectAccess != in.ProjectAccess {
			fields = append(fields, "projectAccess")
		}
		if !sameApiKeyValues(row.Scopes, in.Scopes) {
			fields = append(fields, "scopes")
		}
		oldProjects := []string{}
		for _, p := range before.Projects {
			oldProjects = append(oldProjects, p.PublicID)
		}
		if !sameApiKeyValues(oldProjects, in.Projects) {
			fields = append(fields, "projects")
		}
		if (row.ExpiresAt == nil) != (in.ExpiresAt == nil) || (row.ExpiresAt != nil && in.ExpiresAt != nil && !row.ExpiresAt.Equal(*in.ExpiresAt)) {
			fields = append(fields, "expiresAt")
		}
		if len(fields) == 0 {
			out = before
			return nil
		}
		row.Name, row.ProjectAccess, row.Scopes, row.ExpiresAt = in.Name, in.ProjectAccess, in.Scopes, in.ExpiresAt
		row.UpdatedAt = time.Now()
		if _, err := tx.NewUpdate().Model(&row).Column("name", "project_access", "scopes", "expires_at", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		if err := replaceApiKeyProjects(ctx, tx, row, in.Projects); err != nil {
			return err
		}
		out, err = apiKeyMetadata(ctx, tx, row)
		if err != nil {
			return err
		}
		return s.recordApiKey(ctx, tx, row, w.PublicID, "updated", "", fields)
	})
	return out, err
}

func sameApiKeyValues(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// RevokeApiKey clears the digest atomically, including when effective access was lost.
func (s *Store) RevokeApiKey(ctx context.Context, manager ApiKeyManager, id string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockWorkspaceGraph(ctx, tx, false); err != nil {
			return err
		}
		row, err := findManagedApiKey(ctx, tx, manager, id)
		if err != nil {
			return err
		}
		a, err := lockEmailChangeAccount(ctx, tx, manager.UserID, manager.SessionHash)
		if err != nil || (manager.RequireVerified && a.EmailVerifiedAt == nil) {
			return ErrApiKeyInvalid
		}
		if err := lockWorkspace(ctx, tx, row.WorkspaceID); err != nil {
			return err
		}
		if manager.WorkspaceID != 0 {
			role, err := memberRole(ctx, tx, row.WorkspaceID, a.ID)
			if err != nil {
				return err
			}
			if !role.Can(auth.WorkspaceApiKeyDestroy) {
				return ErrApiKeyForbidden
			}
		}
		if err := tx.NewSelect().Model(&row).Where("ak.public_id = ?", id).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		if err := checkApiKeyManagementSession(ctx, tx, manager); err != nil {
			return err
		}
		var workspace string
		if err := tx.NewSelect().Model((*Workspace)(nil)).Column("w.public_id").Where("w.id = ?", row.WorkspaceID).Scan(ctx, &workspace); err != nil {
			return err
		}
		return s.revokeApiKey(ctx, tx, row, workspace, "manual")
	})
}

// FindApiKey returns one key's metadata within the manager's scope: either
// kind in the workspace for an owner or admin (workspace.api_key.show), or
// the signed-in account's own personal key. sql.ErrNoRows for any other.
func (s *Store) FindApiKey(ctx context.Context, manager ApiKeyManager, id string) (ApiKey, error) {
	row, err := findManagedApiKey(ctx, s.DB, manager, id)
	if err != nil {
		return ApiKey{}, err
	}
	return apiKeyMetadata(ctx, s.DB, row)
}

// FindOwnApiKeyWorkspace returns the internal ID of the workspace of the
// personal key id when userID owns it and is still a member of it, for the
// key's own log; sql.ErrNoRows otherwise (a key of someone else's, or one
// whose owner has left, are alike).
func (s *Store) FindOwnApiKeyWorkspace(ctx context.Context, userID int64, id string) (int64, error) {
	var workspaceID int64
	err := s.DB.NewSelect().Model((*apiKeyRecord)(nil)).Column("ak.workspace_id").
		Where("ak.public_id = ? AND ak.kind = ? AND ak.owner_user_id = ?", id, auth.ApiKeyPersonal, userID).
		Where("EXISTS (?)", s.DB.NewSelect().Model((*membership)(nil)).ColumnExpr("1").Where("m.user_id = ak.owner_user_id AND m.workspace_id = ak.workspace_id")).
		Scan(ctx, &workspaceID)
	return workspaceID, err
}
