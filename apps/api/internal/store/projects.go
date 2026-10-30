package store

import "context"

func (s *Store) Projects(ctx context.Context, userID int64) ([]Project, error) {
	rows, err := s.DB.Query(ctx, "SELECT id,name,description,status,color,created_at,updated_at FROM projects WHERE user_id=$1 ORDER BY updated_at DESC,id DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}
func (s *Store) CreateProject(ctx context.Context, userID int64, in ProjectInput) (Project, error) {
	var p Project
	err := s.DB.QueryRow(ctx, "INSERT INTO projects(user_id,name,description,status,color) VALUES($1,$2,$3,$4,$5) RETURNING id,name,description,status,color,created_at,updated_at", userID, in.Name, in.Description, in.Status, in.Color).Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
func (s *Store) UpdateProject(ctx context.Context, userID, id int64, in ProjectInput) (Project, error) {
	var p Project
	err := s.DB.QueryRow(ctx, "UPDATE projects SET name=$1,description=$2,status=$3,color=$4,updated_at=now() WHERE id=$5 AND user_id=$6 RETURNING id,name,description,status,color,created_at,updated_at", in.Name, in.Description, in.Status, in.Color, id, userID).Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.Color, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
