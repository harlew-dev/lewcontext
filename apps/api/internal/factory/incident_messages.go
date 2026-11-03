package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

// IncidentMessage is the independent factory model, including tombstones.
type IncidentMessage struct {
	bun.BaseModel `bun:"table:incident_messages,alias:im"`
	ID            int64  `bun:",pk,autoincrement"`
	PublicID      string `bun:",nullzero"`
	CycleID       int64
	AuthorID      *int64
	Body          *string
	CreatedAt     time.Time
	RedactedAt    *time.Time
	RedactedBy    *int64
}

// IncidentMessages builds plain comments; set CycleID and optionally AuthorID.
var IncidentMessages = Define(func(f *gofakeit.Faker) IncidentMessage {
	body := f.Sentence(8)
	return IncidentMessage{Body: &body, CreatedAt: time.Now()}
})
