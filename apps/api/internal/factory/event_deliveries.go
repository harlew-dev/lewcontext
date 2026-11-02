package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

type EventDelivery struct {
	bun.BaseModel `bun:"table:event_deliveries,alias:d"`
	EventID       int64  `bun:",pk"`
	Listener      string `bun:",pk"`
	Attempts      int
	NextAttemptAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeliveredAt   time.Time `bun:",nullzero"`
	FailedAt      time.Time `bun:",nullzero"`
	LastError     string
}

// EventDeliveries builds pending deliveries, due now. Set EventID, or use
// DeliveryOf.
var EventDeliveries = Define(func(f *gofakeit.Faker) EventDelivery {
	return EventDelivery{Listener: "test." + f.Noun(), NextAttemptAt: time.Now()}
})

// DeliveryOf queues an event for a listener.
func DeliveryOf(eventID int64, listener string) Factory[EventDelivery] {
	return EventDeliveries.State(func(d *EventDelivery) { d.EventID, d.Listener = eventID, listener })
}
