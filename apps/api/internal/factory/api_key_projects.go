package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
)

type ApiKeyProject struct {
	bun.BaseModel                    `bun:"table:api_key_projects,alias:kp"`
	ID                               int64 `bun:",pk,autoincrement"`
	WorkspaceID, ApiKeyID, ProjectID int64
}

var ApiKeyProjects = Define(func(*gofakeit.Faker) ApiKeyProject { return ApiKeyProject{} })
