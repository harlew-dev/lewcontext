package store

import "context"

func (s *Store) CreateUser(ctx context.Context, name, email, hash string) (User, error) {
	a := account{User: User{Name: name, Email: email}, PasswordHash: hash}
	_, err := s.DB.NewInsert().Model(&a).Returning("*").Exec(ctx)
	return a.User, err
}

// FindUser returns sql.ErrNoRows when no account uses the email.
func (s *Store) FindUser(ctx context.Context, email string) (User, string, error) {
	var a account
	err := s.DB.NewSelect().Model(&a).Where("email = ?", email).Scan(ctx)
	return a.User, a.PasswordHash, err
}
