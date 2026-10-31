package httpapi

import (
	"context"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/mail"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// mailbox is a mail driver that keeps what it's given.
type mailbox struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (b *mailbox) Deliver(_ context.Context, m mail.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, m)
	return nil
}

// resetServer is an integration server whose reset links go to a mailbox
// when deliver runs the worker once, as the server's worker would.
func resetServer(t *testing.T) (s *Server, db *bun.DB, box *mailbox, deliver func()) {
	t.Helper()
	s, db = integrationServer(t)
	box = &mailbox{}
	bus := events.NewBus()
	mailer := must(mail.New(box))(t)
	events.Listen(bus, "mail.password_reset", mail.PasswordReset(s.Store, mailer, s.Config.Origin))
	s.Store.Listeners = bus
	worker := &events.Worker{Bus: bus, Queue: s.Store}
	deliver = func() {
		t.Helper()
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return s, db, box, deliver
}

var resetLink = regexp.MustCompile(`href="http://localhost:3000/auth/forgot-password/([A-Za-z0-9_-]{43})"`)

// tokenIn is the token in a reset email's link.
func tokenIn(t *testing.T, m mail.Message) string {
	t.Helper()
	match := resetLink.FindStringSubmatch(m.HTML)
	if match == nil {
		t.Fatalf("no reset link in:\n%s", m.HTML)
	}
	return match[1]
}

// requestReset asks for a reset link for email and returns the response.
func requestReset(t *testing.T, h http.Handler, email string, want int) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, h, "POST", "/api/auth/password-reset", fmt.Sprintf(`{"email":%q}`, email), "", nil, want)
}

func resetBody(token, password string) string {
	return fmt.Sprintf(`{"token":%q,"newPassword":%q}`, token, password)
}

func checkBody(token string) string {
	return fmt.Sprintf(`{"token":%q}`, token)
}

// hasReset reports whether the user has a live or expired reset link stored.
func hasReset(t *testing.T, db bun.IDB, userID int64) bool {
	t.Helper()
	found, err := db.NewSelect().TableExpr("password_resets").Where("user_id = ?", userID).Exists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// The request answers the same whether or not an account uses the email;
// only the listener, later, finds out.
func TestPasswordResetRequestLooksTheSameForEveryEmail(t *testing.T) {
	s, db, box, deliver := resetServer(t)
	h := s.Routes()
	user := must(factory.Users.CreateOne(context.Background(), db, factory.TestFaker(t)))(t)
	known := requestReset(t, h, "  "+strings.ToUpper(user.Email)+" ", 202)
	unknown := requestReset(t, h, "nobody@example.com", 202)
	if known.Body.String() != unknown.Body.String() || known.Header().Get("Content-Type") != unknown.Header().Get("Content-Type") {
		t.Fatalf("responses differ: %q vs %q", known.Body.String(), unknown.Body.String())
	}
	requested := eventsNamed(t, db, "auth.password_reset_requested")
	if len(requested) != 2 || requested[0].Payload["email"] != user.Email || requested[1].Payload["email"] != "nobody@example.com" ||
		requested[0].Payload["ip"] != "127.0.0.1" || !is(requested[0].UserID, 0) || !is(requested[0].ActorID, 0) {
		t.Fatalf("requested events: %+v", requested)
	}
	if len(box.sent) != 0 {
		t.Fatal("mail sent before the worker ran")
	}

	deliver()
	if len(box.sent) != 1 || box.sent[0].To != user.Email || box.sent[0].Subject != "Reset your password" {
		t.Fatalf("sent: %+v", box.sent)
	}
	token := tokenIn(t, box.sent[0])
	var stored string
	if err := db.NewSelect().TableExpr("password_resets").Column("token_hash").Where("user_id = ?", user.ID).Scan(context.Background(), &stored); err != nil {
		t.Fatal(err)
	}
	if stored != auth.Digest(token) {
		t.Fatal("the stored digest isn't the link's token's")
	}
	sent := onlyEvent(t, db, "auth.password_reset_sent")
	if sent.Payload["user"] != user.PublicID || sent.Payload["email"] != user.Email || !validPublicID("evt", sent.Payload["request"].(string)) ||
		!is(sent.UserID, user.ID) || strings.Contains(sent.Raw, token) || strings.Contains(sent.Raw, stored) {
		t.Fatalf("sent event: %+v", sent)
	}
	call(t, h, "POST", "/api/auth/password-reset", `{"email":"not an email"}`, "", nil, 422)
}

func TestPasswordResetSetsANewPassword(t *testing.T) {
	s, db, box, deliver := resetServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := must(factory.Users.CreateOne(ctx, db, f))(t)
	session := must(factory.SessionFor(ctx, db, f, user.ID))(t)
	requestReset(t, h, user.Email, 202)
	deliver()
	token := tokenIn(t, box.sent[0])

	call(t, h, "POST", "/api/auth/password-reset/check", checkBody(token), "", nil, 204)
	short := call(t, h, "PUT", "/api/auth/password-reset", resetBody(token, "too short"), "", nil, 422)
	if errorField(t, short) != "newPassword" || !storedPasswordIs(t, db, user.ID, factory.DefaultPassword) {
		t.Fatal("a short password was accepted or changed something")
	}
	done := call(t, h, "PUT", "/api/auth/password-reset", resetBody(token, newPassword), "", nil, 200)
	if strings.TrimSpace(done.Body.String()) != fmt.Sprintf(`{"email":%q}`, user.Email) {
		t.Fatalf("reset response: %s", done.Body.String())
	}
	if !storedPasswordIs(t, db, user.ID, newPassword) || hasReset(t, db, user.ID) {
		t.Fatal("password not saved, or the link outlived its use")
	}
	// Every session ends, including any an attacker held.
	call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 401)
	completed := onlyEvent(t, db, "auth.password_reset_completed")
	if completed.Payload["user"] != user.PublicID || completed.Payload["otherSessionsRevoked"] != 1.0 || !is(completed.UserID, user.ID) {
		t.Fatalf("completed event: %+v", completed)
	}
	// The link works once.
	call(t, h, "POST", "/api/auth/password-reset/check", checkBody(token), "", nil, 422)
	again := call(t, h, "PUT", "/api/auth/password-reset", resetBody(token, "yet another passphrase"), "", nil, 422)
	if errorField(t, again) != "token" || !storedPasswordIs(t, db, user.ID, newPassword) {
		t.Fatal("a used link worked again")
	}
	call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, newPassword), "", nil, 200)
	call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword), "", nil, 401)
}

// Links die when they expire, when a newer one is requested, and when the
// password is changed while signed in.
func TestPasswordResetLinkLifetime(t *testing.T) {
	s, db, box, deliver := resetServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	for _, token := range []string{"", "short", strings.Repeat("!", 43), strings.Repeat("a", 44)} {
		call(t, h, "POST", "/api/auth/password-reset/check", checkBody(token), "", nil, 422)
		call(t, h, "PUT", "/api/auth/password-reset", resetBody(token, newPassword), "", nil, 422)
	}
	expiredUser := must(factory.Users.CreateOne(ctx, db, f))(t)
	expired := must(factory.PasswordResets.State(factory.ResetFor(expiredUser.ID)).State(factory.ExpiredReset).CreateOne(ctx, db, f))(t)
	call(t, h, "POST", "/api/auth/password-reset/check", checkBody(expired.Token), "", nil, 422)
	call(t, h, "PUT", "/api/auth/password-reset", resetBody(expired.Token, newPassword), "", nil, 422)

	user := must(factory.Users.CreateOne(ctx, db, f))(t)
	requestReset(t, h, user.Email, 202)
	deliver()
	requestReset(t, h, user.Email, 202)
	deliver()
	first, second := tokenIn(t, box.sent[0]), tokenIn(t, box.sent[1])
	call(t, h, "POST", "/api/auth/password-reset/check", checkBody(first), "", nil, 422)
	call(t, h, "POST", "/api/auth/password-reset/check", checkBody(second), "", nil, 204)

	session := must(factory.SessionFor(ctx, db, f, user.ID))(t)
	call(t, h, "PUT", "/api/auth/password", changeBody(factory.DefaultPassword, newPassword), session.CSRFToken, session.Cookie(), 200)
	call(t, h, "PUT", "/api/auth/password-reset", resetBody(second, "an attacker's passphrase"), "", nil, 422)
	if !storedPasswordIs(t, db, user.ID, newPassword) {
		t.Fatal("a link from before the change undid it")
	}
}

// Two tabs submitting the same link: exactly one sets the password.
func TestPasswordResetLinkWorksOnceUnderConcurrency(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := must(factory.Users.CreateOne(ctx, db, f))(t)
	reset := must(factory.PasswordResets.State(factory.ResetFor(user.ID)).CreateOne(ctx, db, f))(t)
	passwords := []string{"first concurrent passphrase", "second concurrent passphrase"}
	codes := make([]int, len(passwords))
	var wg sync.WaitGroup
	for i, password := range passwords {
		wg.Go(func() {
			r := httptest.NewRequest("PUT", "/api/auth/password-reset", strings.NewReader(resetBody(reset.Token, password)))
			r.Header.Set("Origin", "http://localhost:3000")
			r.Header.Set("Content-Type", "application/json")
			r.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			codes[i] = w.Code
		})
	}
	wg.Wait()
	if !(codes[0] == 200 && codes[1] == 422) && !(codes[0] == 422 && codes[1] == 200) {
		t.Fatalf("want one 200 and one 422, got %v", codes)
	}
	winner := passwords[0]
	if codes[1] == 200 {
		winner = passwords[1]
	}
	if !storedPasswordIs(t, db, user.ID, winner) || len(eventsNamed(t, db, "auth.password_reset_completed")) != 1 {
		t.Fatal("the winning password wasn't the one saved, or two resets were recorded")
	}
}

func TestPasswordResetLimits(t *testing.T) {
	ctx := context.Background()

	t.Run("per email, known or not", func(t *testing.T) {
		s, db := integrationServer(t)
		h := s.Routes()
		for range 3 {
			requestReset(t, h, "nobody@example.com", 202)
		}
		refused := requestReset(t, h, "nobody@example.com", 429)
		if refused.Header().Get("Retry-After") != "3600" || !strings.Contains(refused.Body.String(), "an hour") {
			t.Fatalf("refusal: %v %s", refused.Header(), refused.Body.String())
		}
		requestReset(t, h, "nobody@example.com", 429)
		if e := onlyEvent(t, db, "auth.rate_limited"); e.Payload["scope"] != "password-reset:per-email" {
			t.Fatalf("rate limited: %+v", e)
		}
		if n := len(eventsNamed(t, db, "auth.password_reset_requested")); n != 3 {
			t.Fatalf("%d requests recorded; refused ones count too", n)
		}
		// Another email from the same IP still gets through.
		requestReset(t, h, "somebody@example.com", 202)
	})

	t.Run("per IP", func(t *testing.T) {
		s, db := integrationServer(t)
		h := s.Routes()
		f := factory.TestFaker(t)
		must(factory.AuthAttempts.State(factory.ForLimit("password-reset:per-ip", "127.0.0.1")).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
		requestReset(t, h, "nobody@example.com", 429)
		if e := onlyEvent(t, db, "auth.rate_limited"); e.Payload["scope"] != "password-reset:per-ip" {
			t.Fatalf("rate limited: %+v", e)
		}
		// Refused early, it spent none of the later limits.
		later, err := db.NewSelect().TableExpr("auth_attempts").
			Where("key LIKE 'password-reset:per-email:%' OR key = 'password-reset:all-accounts'").
			Count(ctx)
		if err != nil || later != 0 {
			t.Fatalf("later limits counted %d (%v)", later, err)
		}
	})

	t.Run("all accounts", func(t *testing.T) {
		s, db := integrationServer(t)
		h := s.Routes()
		f := factory.TestFaker(t)
		must(factory.AuthAttempts.State(factory.ForLimit("password-reset:all-accounts", "")).State(factory.Exhausted(100)).CreateOne(ctx, db, f))(t)
		busy := requestReset(t, h, "nobody@example.com", 503)
		if !strings.Contains(busy.Body.String(), "Password reset is busy") {
			t.Fatalf("busy: %s", busy.Body.String())
		}
		if e := onlyEvent(t, db, "auth.rate_limited"); e.Payload["scope"] != "password-reset:all-accounts" {
			t.Fatalf("rate limited: %+v", e)
		}
	})

	t.Run("separate from sign-in", func(t *testing.T) {
		s, db := integrationServer(t)
		h := s.Routes()
		user := must(factory.Users.CreateOne(ctx, db, factory.TestFaker(t)))(t)
		for range 3 {
			requestReset(t, h, user.Email, 202)
		}
		requestReset(t, h, user.Email, 429)
		call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword), "", nil, 200)
	})
}

func TestPasswordResetRequestProtections(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	r := httptest.NewRequest("POST", "/api/auth/password-reset", strings.NewReader(`{"email":"a@example.com"}`))
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin request: %d", w.Code)
	}
	call(t, h, "POST", "/api/auth/password-reset", `{"email":"a@example.com","extra":1}`, "", nil, 400)

	// Without email delivery, nothing could send the link.
	s.Config.MailDriver = config.MailOff
	requestReset(t, s.Routes(), "a@example.com", 503)
	if found := eventsNamed(t, db, "auth.password_reset_requested"); len(found) != 0 {
		t.Fatalf("refused requests recorded: %+v", found)
	}
}
