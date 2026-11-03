package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

// IncidentCycle has its own model, with exactly one parent set by the test.
type IncidentCycle struct {
	bun.BaseModel       `bun:"table:incident_cycles,alias:ic"`
	ID                  int64  `bun:",pk,autoincrement"`
	PublicID            string `bun:",nullzero"`
	AlertID             int64  `bun:",nullzero"`
	AppErrorID          int64  `bun:",nullzero"`
	Number              int
	OpenedAt            time.Time
	ResolvedAt          *time.Time
	ResolutionSource    *string
	ResolvedBy          *int64
	ResolutionMessageID *int64
	// Per-cycle health and notification state: database defaults, read back only.
	State                  string     `bun:",scanonly"`
	Level                  string     `bun:",scanonly"`
	PeakValue              float64    `bun:",scanonly"`
	HealthySince           *time.Time `bun:",scanonly"`
	HealthyChecks          int        `bun:",scanonly"`
	RecoveredAt            *time.Time `bun:",scanonly"`
	LastCheckedAt          *time.Time `bun:",scanonly"`
	LastSuccessfulAt       *time.Time `bun:",scanonly"`
	CurrentLevel           string     `bun:",scanonly"`
	RawLevel               string     `bun:",scanonly"`
	LastBucket             *time.Time `bun:",scanonly"`
	Hourly                 bool       `bun:",scanonly"`
	RecoveryInterrupted    bool       `bun:",scanonly"`
	RecoveryPendingRelapse bool       `bun:",scanonly"`
	Revision               int64      `bun:",scanonly"`
	TransitionSequence     int64      `bun:",scanonly"`
	AcknowledgedAt         *time.Time `bun:",scanonly"`
	AcknowledgedBy         *int64     `bun:",scanonly"`
	Notified               string     `bun:",scanonly"`
	ReminderAnchor         time.Time  `bun:",scanonly"`
}

// IncidentCycles builds an initial open cycle; set AlertID or AppErrorID.
var IncidentCycles = Define(func(f *gofakeit.Faker) IncidentCycle {
	return IncidentCycle{Number: 1, OpenedAt: time.Now().Add(-time.Duration(f.IntRange(1, 60)) * time.Minute)}
})
