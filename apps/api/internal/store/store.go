package store

import (
	"github.com/uptrace/bun"
	"time"
)

type Store struct{ DB *bun.DB }
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`
	ID            int64     `bun:",pk,autoincrement" json:"id"`
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
type Project struct {
	bun.BaseModel `bun:"table:projects,alias:p"`
	ID            int64     `bun:",pk,autoincrement" json:"id"`
	UserID        int64     `bun:",notnull" json:"-"`
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
