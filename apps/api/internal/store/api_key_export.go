package store

import (
	"context"
	"github.com/uptrace/bun"
	"time"
)

// ExportedApiKey lists only the account's personal metadata, never creator
// details, another person's credentials, or response snapshots/digests.
type ExportedApiKey struct {
	PublicID      string     `json:"id"`
	Workspace     string     `json:"workspace"`
	Name          string     `json:"name"`
	ProjectAccess string     `json:"projectAccess"`
	Scopes        []string   `bun:",array" json:"scopes"`
	Projects      []string   `bun:"-" json:"projects"`
	CreatedAt     time.Time  `json:"createdAt"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
}

func exportApiKeys(ctx context.Context, tx bun.Tx, userID int64) ([]ExportedApiKey, error) {
	keys := []ExportedApiKey{}
	err := tx.NewSelect().Model((*apiKeyRecord)(nil)).ColumnExpr("ak.public_id, w.public_id AS workspace, ak.name, ak.project_access, ak.scopes, ak.created_at, ak.expires_at, ak.last_used_at, ak.revoked_at").Join("JOIN workspaces AS w ON w.id = ak.workspace_id").Where("ak.owner_user_id = ? AND ak.kind = 'personal'", userID).OrderExpr("ak.created_at DESC, ak.id DESC").Scan(ctx, &keys)
	if err != nil {
		return keys, err
	}
	for i := range keys {
		keys[i].Projects = []string{}
		err := tx.NewSelect().Model((*apiKeyProject)(nil)).Column("p.public_id").Join("JOIN api_keys AS ak ON ak.id = kp.api_key_id").Join("JOIN projects AS p ON p.id = kp.project_id AND p.workspace_id = kp.workspace_id").Where("ak.public_id = ? AND ak.owner_user_id = ?", keys[i].PublicID, userID).OrderExpr("p.id").Scan(ctx, &keys[i].Projects)
		if err != nil {
			return keys, err
		}
	}
	return keys, nil
}
