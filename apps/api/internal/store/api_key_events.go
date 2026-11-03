package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
)

func (s *Store) recordApiKey(ctx context.Context, tx bun.Tx, row apiKeyRecord, workspace, fact, reason string, fields []string) error {
	data := events.ApiKeyDetails{Key: row.PublicID, Workspace: workspace, Scopes: row.Scopes, Reason: reason, Fields: fields}
	var e events.Event
	if row.Kind == auth.ApiKeyPersonal {
		switch fact {
		case "created":
			e = events.PersonalApiKeyCreated(data)
		case "updated":
			e = events.PersonalApiKeyUpdated(data)
		case "revoked":
			e = events.PersonalApiKeyRevoked(data)
		}
	} else {
		switch fact {
		case "created":
			e = events.WorkspaceApiKeyCreated(data)
		case "updated":
			e = events.WorkspaceApiKeyUpdated(data)
		case "revoked":
			e = events.WorkspaceApiKeyRevoked(data)
		}
	}
	return s.record(ctx, tx, e, EventScope{WorkspaceID: row.WorkspaceID, UserID: row.OwnerUserID})
}

func (s *Store) revokeApiKey(ctx context.Context, tx bun.Tx, row apiKeyRecord, workspace, reason string) error {
	if row.RevokedAt != nil {
		return nil
	}
	if _, err := tx.NewUpdate().Model((*apiKeyRecord)(nil)).Set("revoked_at = clock_timestamp(), token_hash = NULL, updated_at = clock_timestamp()").Where("id = ?", row.ID).Exec(ctx); err != nil {
		return err
	}
	return s.recordApiKey(ctx, tx, row, workspace, "revoked", reason, nil)
}

func (s *Store) revokePersonalApiKeys(ctx context.Context, tx bun.Tx, userID, workspaceID int64, reason string) error {
	var rows []apiKeyRecord
	q := tx.NewSelect().Model(&rows).Where("ak.owner_user_id = ? AND ak.revoked_at IS NULL", userID).OrderExpr("ak.id").For("UPDATE")
	if workspaceID != 0 {
		q = q.Where("ak.workspace_id = ?", workspaceID)
	}
	if err := q.Scan(ctx); err != nil {
		return err
	}
	for _, row := range rows {
		var workspace string
		if err := tx.NewSelect().Model((*Workspace)(nil)).Column("w.public_id").Where("w.id = ?", row.WorkspaceID).Scan(ctx, &workspace); err != nil {
			return err
		}
		if err := s.revokeApiKey(ctx, tx, row, workspace, reason); err != nil {
			return err
		}
	}
	return nil
}
