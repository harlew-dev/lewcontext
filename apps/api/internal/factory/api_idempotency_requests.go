package factory

import (
	"encoding/json"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"time"
)

type ApiIdempotencyRequest struct {
	bun.BaseModel                                    `bun:"table:api_idempotency_requests,alias:ir"`
	ID                                               int64 `bun:",pk,autoincrement"`
	WorkspaceID, ApiKeyID                            int64
	Operation, ValueHash, InputHash, ProjectPublicID string
	Response                                         json.RawMessage `bun:"type:jsonb"`
	CreatedAt, ExpiresAt                             time.Time
}

var ApiIdempotencyRequests = Define(func(f *gofakeit.Faker) ApiIdempotencyRequest {
	now := time.Now().UTC()
	return ApiIdempotencyRequest{Operation: "project.store", ValueHash: auth.Digest(f.UUID()), InputHash: auth.Digest(f.UUID()), ProjectPublicID: "prj_00000000000000", Response: json.RawMessage(`{}`), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
})
