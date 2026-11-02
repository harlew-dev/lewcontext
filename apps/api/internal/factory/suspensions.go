package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

type UserSuspension struct {
	bun.BaseModel `bun:"table:user_suspensions,alias:us"`
	UserID        int64     `bun:",pk"`
	Reason        string    `bun:",notnull"`
	SuspendedBy   int64     `bun:",nullzero"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// UserSuspensions suspend an account, as the command line does (no admin
// named). Set UserID, or use SuspensionOf; SuspendedBy for an admin's.
var UserSuspensions = Define(func(f *gofakeit.Faker) UserSuspension {
	return UserSuspension{Reason: "Sent invitation spam: " + f.Sentence(4)}
})

// SuspensionOf suspends the user.
func SuspensionOf(userID int64) func(*UserSuspension) {
	return func(s *UserSuspension) { s.UserID = userID }
}

// SuspendedBy names the admin who suspended it.
func SuspendedBy(adminID int64) func(*UserSuspension) {
	return func(s *UserSuspension) { s.SuspendedBy = adminID }
}
