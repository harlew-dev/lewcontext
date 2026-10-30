package store

import (
	"github.com/uptrace/bun"
	"time"
)

// Store models keep the internal bigint ID out of JSON: responses identify
// records only by their public ID (usr_…, wsp_…, prj_…), which PostgreSQL
// generates as a column default.
type Store struct{ DB *bun.DB }
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`
	ID            int64     `bun:",pk,autoincrement" json:"-"`
	PublicID      string    `bun:",nullzero" json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"createdAt"`
}

// account carries the password hash so it never travels inside a User response.
type account struct {
	User         `bun:",extend"`
	PasswordHash string
}
type Session struct {
	User                 User
	CSRF                 string
	CreatedAt, ExpiresAt time.Time
}

// Workspace is a workspace as one member sees it: Role is that member's role,
// read from workspace_user.
type Workspace struct {
	bun.BaseModel `bun:"table:workspaces,alias:w"`
	ID            int64     `bun:",pk,autoincrement" json:"-"`
	PublicID      string    `bun:",nullzero" json:"id"`
	Name          string    `json:"name"`
	Role          string    `bun:",scanonly" json:"role"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}
type WorkspaceInput struct {
	Name string `json:"name"`
}

// membership links a user to a workspace with a role.
type membership struct {
	bun.BaseModel `bun:"table:workspace_user,alias:m"`
	ID            int64 `bun:",pk,autoincrement"`
	UserID        int64 `bun:",notnull"`
	WorkspaceID   int64 `bun:",notnull"`
	Role          string
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}
type Project struct {
	bun.BaseModel `bun:"table:projects,alias:p"`
	ID            int64     `bun:",pk,autoincrement" json:"-"`
	PublicID      string    `bun:",nullzero" json:"id"`
	WorkspaceID   int64     `bun:",notnull" json:"-"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Color         string    `json:"color"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}
type ProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Color       string `json:"color"`
}
