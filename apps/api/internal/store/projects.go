package store

import "context"

func (s *Store) Projects(ctx context.Context, userID int64) ([]Project, error) {
	projects := []Project{}
	err := s.DB.NewSelect().Model(&projects).
		Where("user_id = ?", userID).
		OrderExpr("updated_at DESC, id DESC").
		Scan(ctx)
	return projects, err
}
func (s *Store) CreateProject(ctx context.Context, userID int64, in ProjectInput) (Project, error) {
	p := Project{UserID: userID, Name: in.Name, Description: in.Description, Status: in.Status, Color: in.Color}
	_, err := s.DB.NewInsert().Model(&p).Returning("*").Exec(ctx)
	return p, err
}

// UpdateProject returns sql.ErrNoRows when the project does not exist or belongs to another user.
func (s *Store) UpdateProject(ctx context.Context, userID, id int64, in ProjectInput) (Project, error) {
	p := Project{Name: in.Name, Description: in.Description, Status: in.Status, Color: in.Color}
	err := s.DB.NewUpdate().Model(&p).
		Column("name", "description", "status", "color").
		Set("updated_at = now()").
		Where("id = ? AND user_id = ?", id, userID).
		Returning("*").
		Scan(ctx)
	return p, err
}
