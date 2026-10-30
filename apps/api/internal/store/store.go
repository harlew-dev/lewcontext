package store

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct{ DB *pgxpool.Pool }
type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}
type Session struct {
	User                 User
	CSRF                 string
	CreatedAt, ExpiresAt time.Time
}
type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
type ProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Color       string `json:"color"`
}
