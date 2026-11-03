package store

import (
	"context"
	"github.com/uptrace/bun"
)

type apiKeyProject struct {
	bun.BaseModel `bun:"table:api_key_projects,alias:kp"`
	ID            int64 `bun:",pk,autoincrement"`
	WorkspaceID   int64
	ApiKeyID      int64
	ProjectID     int64
}

func replaceApiKeyProjects(ctx context.Context, tx bun.Tx, key apiKeyRecord, selected []string) error {
	if len(selected) > 100 {
		return ErrApiKeyGrants
	}
	seen := map[string]bool{}
	for _, id := range selected {
		if seen[id] {
			return ErrApiKeyGrants
		}
		seen[id] = true
	}
	var projects []Project
	if len(selected) > 0 {
		err := tx.NewSelect().Model(&projects).Where("p.workspace_id = ? AND p.public_id IN (?)", key.WorkspaceID, bun.In(selected)).OrderExpr("p.id").For("KEY SHARE").Scan(ctx)
		if err != nil {
			return err
		}
		if len(projects) != len(selected) {
			return ErrApiKeyGrants
		}
	}
	if _, err := tx.NewDelete().Model((*apiKeyProject)(nil)).Where("api_key_id = ?", key.ID).Exec(ctx); err != nil {
		return err
	}
	for _, p := range projects {
		selection := apiKeyProject{WorkspaceID: key.WorkspaceID, ApiKeyID: key.ID, ProjectID: p.ID}
		if _, err := tx.NewInsert().Model(&selection).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
