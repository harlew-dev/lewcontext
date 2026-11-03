package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

// ApiKey factories are revoked by default: seeders never issue usable credentials.
type ApiKey struct {
	bun.BaseModel                    `bun:"table:api_keys,alias:ak"`
	ID                               int64  `bun:",pk,autoincrement"`
	PublicID                         string `bun:",nullzero"`
	WorkspaceID                      int64
	Kind                             string
	OwnerUserID                      int64 `bun:",nullzero"`
	CreatedBy                        int64 `bun:",nullzero"`
	Name                             string
	TokenHash                        string `bun:",nullzero"`
	ProjectAccess                    string
	Scopes                           []string `bun:",array"`
	CreatedAt, UpdatedAt             time.Time
	ExpiresAt, LastUsedAt, RevokedAt *time.Time
}

var ApiKeys = Define(func(f *gofakeit.Faker) ApiKey {
	now := time.Now().UTC()
	return ApiKey{Kind: "workspace", Name: truncate(f.AppName(), 80), ProjectAccess: "all", Scopes: []string{"project.index", "project.show"}, CreatedAt: now, UpdatedAt: now, RevokedAt: &now}
})

// ApiKeyNever removes finite expiry without changing revocation.
func ApiKeyNever(k *ApiKey) { k.ExpiresAt = nil }

// ApiKeyExpired has a valid creation-relative finite expiry in the past.
func ApiKeyExpired(k *ApiKey) {
	k.CreatedAt = time.Now().Add(-48 * time.Hour)
	end := k.CreatedAt.Add(24 * time.Hour)
	k.ExpiresAt = &end
}
