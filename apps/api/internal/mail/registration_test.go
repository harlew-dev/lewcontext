package mail

import (
	"bytes"
	"context"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"regexp"
	"strings"
	"testing"
	"time"
)

var testKeys = func() *auth.Keys {
	keys, err := auth.NewKeys(bytes.Repeat([]byte{3}, auth.AppKeySize))
	if err != nil {
		panic(err)
	}
	return keys
}()

// signUps is an in-memory RegistrationStore: requests by evt_ ID, the
// accounts by email, and a counter per limit key.
type signUps struct {
	requests map[string]string // request → email
	codes    map[string]string // request → code digest
	users    map[string]store.User
	hits     map[string]int
	recorded []events.Event
}

func newSignUps() *signUps {
	return &signUps{requests: map[string]string{}, codes: map[string]string{}, users: map[string]store.User{}, hits: map[string]int{}}
}

func (s *signUps) IssueRegistrationCode(_ context.Context, request, codeHash string) (string, *store.User, error) {
	email, ok := s.requests[request]
	if !ok {
		return "", nil, store.ErrNoRegistration
	}
	s.codes[request] = codeHash
	if u, ok := s.users[email]; ok {
		return email, &u, nil
	}
	return email, nil, nil
}

func (s *signUps) CountHit(_ context.Context, key string, window time.Duration) (int, time.Time, error) {
	s.hits[key]++
	return s.hits[key], time.Now().Add(window), nil
}

func (s *signUps) Record(_ context.Context, e events.Event, _ store.EventScope) error {
	s.recorded = append(s.recorded, e)
	return nil
}

var spacedCode = regexp.MustCompile(`\b(\d{3}) (\d{3})\b`)

func TestRegistrationListenerSendsACode(t *testing.T) {
	m, box := newMailer(t)
	s := newSignUps()
	s.requests["evt_00000000000001"] = "new@example.com"
	listen := Registration(s, m, testKeys, "http://localhost:3000", true)
	d := events.Delivered{EventID: "evt_00000000000001", OccurredAt: time.Now(), Attempt: 1}
	if err := listen(context.Background(), events.RegistrationRequested{Email: "new@example.com"}, d); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != 1 || box.sent[0].To != "new@example.com" || box.sent[0].Template != VerifyEmail {
		t.Fatalf("sent %+v", box.sent)
	}
	match := spacedCode.FindStringSubmatch(box.sent[0].Text)
	if match == nil || s.codes[d.EventID] != testKeys.EmailCodeDigest(match[1]+match[2]) {
		t.Fatalf("stored digest isn't the emailed code's:\n%s", box.sent[0].Text)
	}
	// A code to type, never a link to click.
	if strings.Contains(box.sent[0].HTML, "<a ") || strings.Contains(box.sent[0].Text, "http") {
		t.Fatalf("verify-email has a link:\n%s", box.sent[0].HTML)
	}
	sent, ok := s.recorded[0].(events.RegistrationSent)
	if len(s.recorded) != 1 || !ok || sent.ExistingAccount || sent.Request != d.EventID {
		t.Fatalf("recorded %+v", s.recorded)
	}

	// A sign-up that's gone, or asked for a newer code, gets nothing; so does
	// a request older than a code.
	gone := events.Delivered{EventID: "evt_00000000000009", OccurredAt: time.Now(), Attempt: 1}
	stale := events.Delivered{EventID: "evt_00000000000001", OccurredAt: time.Now().Add(-auth.EmailCodeLifetime - time.Minute), Attempt: 1}
	for _, d := range []events.Delivered{gone, stale} {
		if err := listen(context.Background(), events.RegistrationRequested{}, d); err != nil || len(box.sent) != 1 {
			t.Fatalf("%s: %v, sent %d", d.EventID, err, len(box.sent))
		}
	}
}

func TestRegistrationListenerTellsAnAccountItExists(t *testing.T) {
	m, box := newMailer(t)
	s := newSignUps()
	ada := store.User{ID: 7, PublicID: "usr_00000000000007", Name: "<b>Ada</b>", Email: "ada@example.com"}
	s.users[ada.Email] = ada
	listen := Registration(s, m, testKeys, "http://localhost:3000", true)
	ctx := context.Background()
	for i := range AccountExistsPerAccount.Limit + 2 {
		id := "evt_0000000000000" + string(rune('a'+i))
		s.requests[id] = ada.Email
		if err := listen(ctx, events.RegistrationRequested{Email: ada.Email}, events.Delivered{EventID: id, OccurredAt: time.Now(), Attempt: 1}); err != nil {
			t.Fatal(err)
		}
		// The sign-up still gets a code, just one nobody is sent.
		if s.codes[id] == "" {
			t.Fatal("no code stored for a taken email")
		}
	}
	// Five a day; the sixth and later send nothing, silently.
	if len(box.sent) != AccountExistsPerAccount.Limit {
		t.Fatalf("sent %d", len(box.sent))
	}
	msg := box.sent[0]
	if msg.Template != AccountExists || msg.To != ada.Email || spacedCode.MatchString(msg.Text) {
		t.Fatalf("sent %+v", msg)
	}
	for _, want := range []string{`href="http://localhost:3000/auth/login?notice=account-exists"`, `href="http://localhost:3000/auth/forgot-password"`, "&lt;b&gt;Ada", "two-factor"} {
		if !strings.Contains(msg.HTML, want) {
			t.Errorf("missing %q in:\n%s", want, msg.HTML)
		}
	}
	var sent, reached int
	for _, e := range s.recorded {
		switch e := e.(type) {
		case events.RegistrationSent:
			if !e.ExistingAccount || e.User != ada.PublicID {
				t.Errorf("recorded %+v", e)
			}
			sent++
		case events.RateLimitReached:
			if e.Limit != "account-exists:per-account:block" || e.User != ada.PublicID {
				t.Errorf("recorded %+v", e)
			}
			reached++
		}
	}
	if sent != AccountExistsPerAccount.Limit || reached != 1 {
		t.Fatalf("%d sent, %d reached", sent, reached)
	}

	// A retry of a delivery doesn't count again, and RATE_LIMITS=off counts
	// nothing.
	s.hits = map[string]int{}
	s.requests["evt_retry"] = ada.Email
	retry := events.Delivered{EventID: "evt_retry", OccurredAt: time.Now(), Attempt: 2}
	if err := listen(ctx, events.RegistrationRequested{}, retry); err != nil || len(s.hits) != 0 {
		t.Fatalf("a retry counted: %v %v", err, s.hits)
	}
	unlimited := Registration(s, m, testKeys, "http://localhost:3000", false)
	if err := unlimited(ctx, events.RegistrationRequested{}, events.Delivered{EventID: "evt_retry", OccurredAt: time.Now(), Attempt: 1}); err != nil || len(s.hits) != 0 {
		t.Fatalf("RATE_LIMITS=off counted: %v %v", err, s.hits)
	}
}

// verifications is an in-memory EmailVerificationStore.
type verifications struct {
	unverified map[string]store.User // public ID → account
	codes      map[string]string     // public ID → code digest
	recorded   []events.Event
}

func (v *verifications) IssueEmailVerification(_ context.Context, publicID, _, codeHash string, _, _ time.Time) (store.User, error) {
	u, ok := v.unverified[publicID]
	if !ok {
		return u, store.ErrAlreadyVerified
	}
	v.codes[publicID] = codeHash
	return u, nil
}

func (v *verifications) Record(_ context.Context, e events.Event, _ store.EventScope) error {
	v.recorded = append(v.recorded, e)
	return nil
}

func TestEmailVerificationListener(t *testing.T) {
	m, box := newMailer(t)
	ada := store.User{ID: 7, PublicID: "usr_00000000000007", Name: "Ada", Email: "ada@example.com"}
	v := &verifications{unverified: map[string]store.User{ada.PublicID: ada}, codes: map[string]string{}}
	listen := EmailVerification(v, m, testKeys)
	ctx := context.Background()
	d := events.Delivered{EventID: "evt_00000000000001", OccurredAt: time.Now(), Attempt: 1}
	if err := listen(ctx, events.EmailVerificationRequested{User: ada.PublicID}, d); err != nil {
		t.Fatal(err)
	}
	match := spacedCode.FindStringSubmatch(box.sent[0].Text)
	if len(box.sent) != 1 || box.sent[0].To != ada.Email || match == nil || v.codes[ada.PublicID] != testKeys.EmailCodeDigest(match[1]+match[2]) {
		t.Fatalf("sent %+v", box.sent)
	}
	if sent, ok := v.recorded[0].(events.EmailVerificationSent); !ok || sent.User != ada.PublicID || sent.Request != d.EventID {
		t.Fatalf("recorded %+v", v.recorded)
	}
	// Verified meanwhile: nothing.
	if err := listen(ctx, events.EmailVerificationRequested{User: "usr_00000000000008"}, d); err != nil || len(box.sent) != 1 {
		t.Fatalf("a verified account got a code: %v", err)
	}
}
