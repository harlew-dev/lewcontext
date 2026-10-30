package store

import "context"

func (s *Store) CreateUser(ctx context.Context, name, email, hash string) (User, error) {
	var u User
	err := s.DB.QueryRow(ctx, "INSERT INTO users(name,email,password_hash) VALUES($1,$2,$3) RETURNING id,name,email,created_at", name, email, hash).Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
	return u, err
}
func (s *Store) FindUser(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	err := s.DB.QueryRow(ctx, "SELECT id,name,email,created_at,password_hash FROM users WHERE email=$1", email).Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt, &hash)
	return u, hash, err
}
