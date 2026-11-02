package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Delivered describes the event a listener is handling.
type Delivered struct {
	// EventID is the event's public ID (evt_…). Key side effects on it: a
	// listener can run more than once for the same event.
	EventID    string
	OccurredAt time.Time
	// Attempt counts from 1.
	Attempt int
}

// handler decodes a stored payload and calls a listener with it.
type handler func(ctx context.Context, payload []byte, d Delivered) error

type listener struct {
	event  string
	handle handler
}

// Bus is the set of listeners. Register them all with Listen while the
// server starts, before the worker runs; the bus isn't changed after that.
type Bus struct {
	listeners map[string]listener // by listener name
	byEvent   map[string][]string // event name → listener names
}

func NewBus() *Bus {
	return &Bus{listeners: map[string]listener{}, byEvent: map[string][]string{}}
}

// Listen registers fn under a stable name to run after every E. The name is
// stored with each delivery, so renaming a listener makes it a new one.
//
//	events.Listen(bus, "mail.password_changed", func(ctx context.Context, e events.PasswordChanged, d events.Delivered) error { … })
func Listen[E Event](b *Bus, name string, fn func(ctx context.Context, e E, d Delivered) error) {
	if name == "" || len(name) > 100 {
		panic("events: a listener needs a name of 1–100 characters")
	}
	if _, taken := b.listeners[name]; taken {
		panic(fmt.Sprintf("events: listener %q registered twice", name))
	}
	var zero E
	event := zero.EventName()
	b.listeners[name] = listener{event: event, handle: func(ctx context.Context, payload []byte, d Delivered) error {
		var e E
		if err := json.Unmarshal(payload, &e); err != nil {
			return fmt.Errorf("decode %s payload: %w", event, err)
		}
		return fn(ctx, e, d)
	}}
	b.byEvent[event] = append(b.byEvent[event], name)
}

// ListenersFor names the listeners of an event, for the store to queue a
// delivery to each when it records one.
func (b *Bus) ListenersFor(event string) []string {
	if b == nil {
		return nil
	}
	return b.byEvent[event]
}

// handler is the named listener's, or nil if none is registered (it was
// renamed or removed since the delivery was queued).
func (b *Bus) handler(name, event string) handler {
	l, ok := b.listeners[name]
	if !ok || l.event != event {
		return nil
	}
	return l.handle
}
