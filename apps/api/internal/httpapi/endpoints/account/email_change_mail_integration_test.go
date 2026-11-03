package account_test

import (
	"context"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"sync"
	"testing"
	"time"
)

type retryEmailMailbox struct {
	mu             sync.Mutex
	messages       []mail.Message
	previousFailed bool
}

func (b *retryEmailMailbox) Deliver(_ context.Context, message mail.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if message.Template == mail.EmailChangedPrevious && !b.previousFailed {
		b.previousFailed = true
		return errors.New("simulated SMTP failure")
	}
	b.messages = append(b.messages, message)
	return nil
}

func TestEmailChangeWorkerRetriesNoticesIndependentlyAndHonoursErasure(t *testing.T) {
	s, db, _ := verificationServer(t, false)
	ctx, f := context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	original := apitest.EmailOf(t, db, session.UserID)
	const replacement = "worker-replacement@example.test"
	box := &retryEmailMailbox{}
	mailer := apitest.Must(mail.New(mail.Options{Driver: box, App: "lewcontext", Origin: s.Config.Origin, Blocks: s.Repo}))(t)
	keys := apitest.Must(auth.NewKeys(s.Config.AppKey))(t)
	bus := events.NewBus()
	events.Listen(bus, "mail.email_change", mail.EmailChange(s.Repo, mailer, keys, s.Config.Origin, true))
	events.Listen(bus, "mail.email_changed_previous", mail.EmailChangedNotice(s.Repo, mailer, s.Config.Origin, true))
	events.Listen(bus, "mail.email_changed_current", mail.EmailChangedNotice(s.Repo, mailer, s.Config.Origin, false))
	s.Repo.Listeners = bus
	worker := &events.Worker{Bus: bus, Queue: s.Repo}
	h := router.New(s)
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(replacement), session.CSRFToken, session.Cookie(), 202)
	if n, err := worker.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("code delivery: %d %v", n, err)
	}
	if len(box.messages) != 1 {
		t.Fatal("code not sent")
	}
	match := emailedCode.FindStringSubmatch(box.messages[0].Text)
	if match == nil {
		t.Fatal("missing code")
	}
	apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(match[1]+match[2]), session.CSRFToken, session.Cookie(), 200)
	changedID, occurred := newestEvent(t, db, "auth.email_changed")
	if n, err := worker.RunOnce(ctx); err != nil || n != 2 {
		t.Fatalf("notice deliveries: %d %v", n, err)
	}
	if len(box.messages) != 2 || box.messages[1].Template != mail.EmailChangedCurrent {
		t.Fatal("one failed notice blocked the other")
	}
	// Simulate a later address in the fixture before the old notice retries.
	// The retry must use the committed snapshot, never this later address.
	apitest.Must(db.NewUpdate().Model((*factory.User)(nil)).Set("email = ?", "later@example.test").Where("id = ?", session.UserID).Exec(ctx))(t)
	apitest.Must(db.NewUpdate().Model((*factory.EventDelivery)(nil)).Set("next_attempt_at = ?", time.Now().Add(-time.Second)).Where("listener = ? AND delivered_at IS NULL", "mail.email_changed_previous").Exec(ctx))(t)
	if n, err := worker.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("retry: %d %v", n, err)
	}
	counts := map[mail.Template]int{}
	for _, message := range box.messages {
		counts[message.Template]++
	}
	if counts[mail.EmailChangedCurrent] != 1 || counts[mail.EmailChangedPrevious] != 1 || box.messages[2].To != original {
		t.Fatalf("notice retry changed destinations or duplicated success: %v", counts)
	}
	// Events already decoded by a listener cannot resurrect erased addresses.
	if err := s.Repo.DeleteAccount(ctx, store.User{ID: session.UserID}, s.EmailPseudonym); err != nil {
		t.Fatal(err)
	}
	listener := mail.EmailChangedNotice(s.Repo, mailer, s.Config.Origin, true)
	if err := listener(ctx, events.EmailChanged{PreviousEmail: original}, events.Delivered{EventID: changedID, OccurredAt: occurred, Attempt: 2}); err != nil {
		t.Fatal(err)
	}
	if len(box.messages) != 3 {
		t.Fatal("erased event sent another notice")
	}
}

func TestEmailChangeNoticeSharesSignUpInformationalCap(t *testing.T) {
	s, db, box := verificationServer(t, false)
	ctx, f := context.Background(), factory.TestFaker(t)
	requester := apitest.SignedIn(t, db, f)
	recipient := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.RateLimits.State(factory.ForLimit(mail.AccountExistsPerAccount.Name, recipient.PublicID)).State(factory.Exhausted(4)).CreateOne(ctx, db, f))(t)
	h := router.New(s)
	signUp(t, h, recipient.Email)
	deliverSignUp(t, s, box)
	if len(box.sent) != 1 || box.sent[0].Template != mail.AccountExists {
		t.Fatal("fifth informational notice was not sent")
	}
	for range 2 {
		apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(recipient.Email), requester.CSRFToken, requester.Cookie(), 202)
		deliverEmailChange(t, s, box)
	}
	if len(box.sent) != 1 {
		t.Fatal("email changes bypassed the shared informational cap")
	}
	count := 0
	for _, event := range apitest.EventsNamed(t, db, "rate_limit.reached") {
		if event.Payload["limit"] == mail.AccountExistsPerAccount.Name+":block" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("informational refusal events: %d", count)
	}
}
