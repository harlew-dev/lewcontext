package factory

import (
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

type IPBlock struct {
	bun.BaseModel `bun:"table:ip_blocks,alias:ipb"`
	ID            int64     `bun:",pk,autoincrement"`
	PublicID      string    `bun:",nullzero"`
	Network       string    `bun:",type:cidr"`
	Reason        string    `bun:",notnull"`
	CreatedBy     int64     `bun:",nullzero"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

// IPBlocks build blocks of a single documentation address (198.51.100.0/24,
// RFC 5737) for a day, with no admin named. Use BlockOf for a network,
// BlockedBy for the admin.
var IPBlocks = Define(func(f *gofakeit.Faker) IPBlock {
	return IPBlock{
		Network:   fmt.Sprintf("198.51.100.%d/32", f.IntRange(1, 254)),
		Reason:    "Credential stuffing: " + f.Sentence(4),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
})

// BlockOf blocks network (CIDR).
func BlockOf(network string) func(*IPBlock) {
	return func(b *IPBlock) { b.Network = network }
}

// BlockedBy names the admin who blocked it.
func BlockedBy(adminID int64) func(*IPBlock) {
	return func(b *IPBlock) { b.CreatedBy = adminID }
}

// ExpiredBlock ended a second ago (made two seconds ago, so the table's
// CHECK holds).
func ExpiredBlock(b *IPBlock) {
	b.CreatedAt = time.Now().Add(-2 * time.Second)
	b.ExpiresAt = time.Now().Add(-time.Second)
}
