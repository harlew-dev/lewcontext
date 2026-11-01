package apitest

import (
	"context"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"sync"
	"testing"
)

// Mailbox is a mail driver that keeps what it's given.
type Mailbox struct {
	mu   sync.Mutex
	Sent []mail.Message
}

// Deliver keeps m.
func (b *Mailbox) Deliver(_ context.Context, m mail.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Sent = append(b.Sent, m)
	return nil
}

// Mail registers the reset link and invitation listeners on repo, sending
// into a Mailbox, and returns it with deliver, which runs the worker once.
func Mail(t *testing.T, repo *store.Store, origin string) (box *Mailbox, deliver func()) {
	t.Helper()
	box = &Mailbox{}
	bus := events.NewBus()
	mailer := Must(mail.New(mail.Options{Driver: box, App: "lewcontext", Origin: origin, Blocks: repo}))(t)
	events.Listen(bus, "mail.password_reset", mail.PasswordReset(repo, mailer, origin))
	events.Listen(bus, "mail.workspace_invitation", mail.SendInvitation(repo, mailer, origin))
	repo.Listeners = bus
	worker := &events.Worker{Bus: bus, Queue: repo}
	deliver = func() {
		t.Helper()
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return box, deliver
}
