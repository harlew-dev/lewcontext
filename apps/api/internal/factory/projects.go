package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

// The values the API accepts (httpapi/projects.go); a test keeps them in step.
var (
	projectStatuses = []string{"Planned", "In progress", "Completed"}
	projectColors   = []string{"violet", "blue", "amber", "emerald", "rose"}
)

type Project struct {
	bun.BaseModel `bun:"table:projects,alias:p"`
	ID            int64 `bun:",pk,autoincrement"`
	UserID        int64 `bun:",notnull"`
	Name          string
	Description   string
	Status        string
	Color         string
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// Projects builds projects created in the last 90 days and updated since.
// Set UserID, or use ProjectsFor.
var Projects = Define(func(f *gofakeit.Faker) Project {
	now := time.Now()
	created := f.DateRange(now.AddDate(0, 0, -90), now)
	return Project{
		Name:        truncate(f.ProductName(), 100),
		Description: truncate(f.Sentence(), 500),
		Status:      f.RandomString(projectStatuses),
		Color:       f.RandomString(projectColors),
		CreatedAt:   created,
		UpdatedAt:   f.DateRange(created, now),
	}
})

// ProjectsFor builds projects owned by a user.
func ProjectsFor(userID int64) Factory[Project] {
	return Projects.State(func(p *Project) { p.UserID = userID })
}

// truncate cuts s to at most n Unicode characters.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
