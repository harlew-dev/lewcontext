package factory

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

type AppError struct {
	bun.BaseModel `bun:"table:app_errors,alias:ae"`
	ID            int64 `bun:",pk,autoincrement"`
	Fingerprint   string
	Source        string
	Route         string
	Message       string
	Stack         string
	Count         int64
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
	LastRequestID string
	LastVersion   string
	ResolvedAt    time.Time `bun:",nullzero"`
}

// AppErrors builds open request error groups, already scrubbed as the
// collector stores them, first seen in the last 30 days and last seen since,
// each with a fingerprint of its own. Seeders make none: fake failures
// would be misleading.
var AppErrors = Define(func(f *gofakeit.Faker) AppError {
	now := time.Now()
	first := f.DateRange(now.AddDate(0, 0, -30), now)
	message := "ERROR: " + f.HackerPhrase() + " (SQLSTATE 23505)"
	sum := sha256.Sum256([]byte(f.UUID() + message))
	return AppError{
		Fingerprint: hex.EncodeToString(sum[:]),
		Source:      "request",
		Route:       "GET /api/workspaces/{workspace}/projects",
		Message:     truncate(message, 1000),
		Count:       int64(f.IntRange(1, 500)),
		FirstSeenAt: first,
		LastSeenAt:  f.DateRange(first, now),
		LastVersion: "dev",
	}
})

// LastSeen sets when the group last happened (and first, if later).
func LastSeen(at time.Time) func(*AppError) {
	return func(e *AppError) {
		e.LastSeenAt = at
		if e.FirstSeenAt.After(at) {
			e.FirstSeenAt = at
		}
	}
}

// Resolved marks the group resolved, as an admin would.
func Resolved(e *AppError) {
	e.ResolvedAt = time.Now()
}
