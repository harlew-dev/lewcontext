package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var blockLinkPattern = regexp.MustCompile(`^http://localhost:3000/auth/email-blocks/([A-Za-z0-9_-]{43})$`)

// blockTokenIn is the token in a blockable email's block link.
func blockTokenIn(t *testing.T, m mail.Message) string {
	t.Helper()
	match := blockLinkPattern.FindStringSubmatch(m.Unsubscribe)
	if match == nil {
		t.Fatalf("no block link in %s (Unsubscribe %q)", m.Template, m.Unsubscribe)
	}
	return match[1]
}

// inviteTo has a new owner invite email to a new workspace, as a member.
func inviteTo(t *testing.T, h http.Handler, db bun.IDB, f *gofakeit.Faker, email string) {
	t.Helper()
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	body := fmt.Sprintf(`{"email":%q,"role":"member"}`, email)
	apitest.Call(t, h, "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), body, owner.CSRFToken, owner.Cookie(), 201)
}

func checkBlockLink(t *testing.T, h http.Handler, token string) store.EmailBlockLink {
	t.Helper()
	var link store.EmailBlockLink
	w := apitest.Call(t, h, "POST", "/api/email-blocks/check", apitest.TokenBody(token), "", nil, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	return link
}

func countBlocks(t *testing.T, db bun.IDB, email string) int {
	t.Helper()
	n, err := db.NewSelect().TableExpr("email_blocks").Where("email = ?", email).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// An invitation's block link checks out, blocks its address once, and stops
// every later email to it without telling the inviter; unblocking lets mail
// through again.
func TestBlockingFromAnInvitation(t *testing.T) {
	ctx := context.Background()
	s, db, box, deliver := mailServer(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	const email = "bob@example.com"

	inviteTo(t, h, db, f, email)
	deliver()
	if len(box.Sent) != 1 {
		t.Fatalf("sent %d", len(box.Sent))
	}
	token := blockTokenIn(t, box.Sent[0])
	if link := checkBlockLink(t, h, token); link != (store.EmailBlockLink{Email: email}) {
		t.Fatalf("check: %+v", link)
	}
	// Only the link's digest is stored, and no event holds the token.
	stored, err := db.NewSelect().TableExpr("email_block_links").Where("token_hash = ? AND email = ?", auth.Digest(token), email).Exists(ctx)
	if err != nil || !stored {
		t.Fatalf("link not stored by its digest (%v)", err)
	}
	if n, _ := db.NewSelect().TableExpr("events").Where("payload::text LIKE ?", "%"+token+"%").Count(ctx); n != 0 {
		t.Fatal("an event holds the block token")
	}

	apitest.Call(t, h, "POST", "/api/email-blocks", apitest.TokenBody(token), "", nil, 204)
	apitest.Call(t, h, "POST", "/api/email-blocks", apitest.TokenBody(token), "", nil, 204)
	blocked := apitest.OnlyEvent(t, db, "email.blocked")
	if blocked.Payload["email"] != email || blocked.IP != "127.0.0.1" || blocked.UserID.Valid || blocked.ActorID.Valid {
		t.Fatalf("email.blocked: %+v", blocked)
	}
	if link := checkBlockLink(t, h, token); !link.Blocked || link.Registered {
		t.Fatalf("check after blocking: %+v", link)
	}

	// Another invitation is created and listed as sent, like any other, but
	// nothing reaches the inbox and no .sent is recorded.
	sentBefore := len(apitest.EventsNamed(t, db, "workspace.invitation.sent"))
	inviteTo(t, h, db, f, email)
	deliver()
	unsent := apitest.Must(db.NewSelect().TableExpr("workspace_invitations").Where("email = ? AND sent_at IS NULL", email).Count(ctx))(t)
	if len(box.Sent) != 1 || len(apitest.EventsNamed(t, db, "workspace.invitation.sent")) != sentBefore || unsent != 0 {
		t.Fatalf("blocked invitation: sent %d, %d without sentAt", len(box.Sent), unsent)
	}

	// Unblocking from the same link lets the next invitation through.
	apitest.Call(t, h, "DELETE", "/api/email-blocks", apitest.TokenBody(token), "", nil, 204)
	apitest.Call(t, h, "DELETE", "/api/email-blocks", apitest.TokenBody(token), "", nil, 204)
	unblocked := apitest.OnlyEvent(t, db, "email.unblocked")
	if unblocked.Payload["email"] != email || unblocked.Payload["by"] != "link" || unblocked.IP != "127.0.0.1" {
		t.Fatalf("email.unblocked: %+v", unblocked)
	}
	inviteTo(t, h, db, f, email)
	deliver()
	if len(box.Sent) != 2 || box.Sent[1].To != email {
		t.Fatalf("after unblocking: sent %d", len(box.Sent))
	}
}

// Mail queued before a block isn't sent: Send checks the list every time.
func TestQueuedInvitationToABlockedAddress(t *testing.T) {
	ctx := context.Background()
	s, db, box, deliver := mailServer(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	inviteTo(t, h, db, f, "bob@example.com")
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("bob@example.com")).CreateOne(ctx, db, f))(t)
	deliver()
	if len(box.Sent) != 0 || len(apitest.EventsNamed(t, db, "workspace.invitation.sent")) != 0 {
		t.Fatalf("sent %d to a blocked address", len(box.Sent))
	}
	if n := apitest.Must(db.NewSelect().TableExpr("event_deliveries").Where("delivered_at IS NOT NULL").Count(ctx))(t); n != 1 {
		t.Fatalf("the delivery isn't done: %d delivered", n)
	}
}

// Sign-up with a blocked address, verification on, answers exactly as for
// any address and sends no code. A code sent before the block can't make
// the account.
func TestSignUpWithABlockedAddress(t *testing.T) {
	ctx := context.Background()
	s, db, box := verificationServer(t, false)
	h := router.New(s)
	f := factory.TestFaker(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("blocked@example.com")).CreateOne(ctx, db, f))(t)

	blocked := apitest.Call(t, h, "POST", "/api/auth/register", signUpBody("blocked@example.com"), "", nil, 202)
	deliverSignUp(t, s, box)
	if len(box.sent) != 0 || len(apitest.EventsNamed(t, db, "auth.registration.sent")) != 0 {
		t.Fatalf("sent %d to a blocked address", len(box.sent))
	}
	fresh := apitest.Call(t, h, "POST", "/api/auth/register", signUpBody("fresh@example.com"), "", nil, 202)
	if strings.ReplaceAll(blocked.Body.String(), "blocked@", "fresh@") != fresh.Body.String() || len(blocked.Result().Cookies()) != len(fresh.Result().Cookies()) {
		t.Fatalf("answers differ: %s vs %s", blocked.Body.String(), fresh.Body.String())
	}
	cookie := mustCookie(t, blocked, auth.RegistrationCookieName(false))
	// Its code page works as any other.
	apitest.Call(t, h, "POST", "/api/auth/register/check", "{}", "", cookie, 200)
	apitest.Call(t, h, "POST", "/api/auth/register/verify", apitest.CodeBody("123456"), "", cookie, 422)

	// Blocked after its code was sent: the right code ends the sign-up.
	pending := apitest.Must(factory.PendingRegistrations.State(factory.RegistrationFor("late@example.com")).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("late@example.com")).CreateOne(ctx, db, f))(t)
	w := apitest.Call(t, h, "POST", "/api/auth/register/verify", apitest.CodeBody(pending.Code), "", pending.Cookie(), 401)
	if !strings.Contains(w.Body.String(), "This address has been blocked.") {
		t.Fatalf("verify: %s", w.Body.String())
	}
	if countUsers(t, db, "late@example.com") != 0 {
		t.Fatal("a blocked address became an account")
	}
	if n := apitest.Must(db.NewSelect().TableExpr("pending_registrations").Where("email = ?", "late@example.com").Count(ctx))(t); n != 0 {
		t.Fatalf("%d sign-ups left", n)
	}
}

// With verification off, a blocked address is refused as a taken one is;
// unblocked, it signs up.
func TestSignUpWithABlockedAddressWithoutVerification(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	apitest.Must(factory.Users.State(func(u *factory.User) { u.Email = "taken@example.com" }).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("blocked@example.com")).CreateOne(ctx, db, f))(t)

	taken := apitest.Call(t, h, "POST", "/api/auth/register", signUpBody("taken@example.com"), "", nil, 409)
	blocked := apitest.Call(t, h, "POST", "/api/auth/register", signUpBody("blocked@example.com"), "", nil, 409)
	if taken.Body.String() != blocked.Body.String() {
		t.Fatalf("answers differ: %s vs %s", taken.Body.String(), blocked.Body.String())
	}
	if countUsers(t, db, "blocked@example.com") != 0 || len(apitest.EventsNamed(t, db, "auth.registered")) != 0 {
		t.Fatal("a blocked address became an account")
	}

	if err := s.Repo.UnblockEmailAddress(ctx, "blocked@example.com", nil); err != nil {
		t.Fatal(err)
	}
	apitest.Call(t, h, "POST", "/api/auth/register", signUpBody("blocked@example.com"), "", nil, 201)
}

// An address with an account can't be blocked: its link says so, and
// blocking it is a 409 that changes nothing.
func TestBlockingARegisteredAddress(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	link := apitest.Must(factory.EmailBlockLinks.State(factory.BlockLinkFor(user.Email)).CreateOne(ctx, db, f))(t)
	if got := checkBlockLink(t, h, link.Token); got != (store.EmailBlockLink{Email: user.Email, Registered: true}) {
		t.Fatalf("check: %+v", got)
	}
	w := apitest.Call(t, h, "POST", "/api/email-blocks", apitest.TokenBody(link.Token), "", nil, 409)
	if !strings.Contains(w.Body.String(), "Delete your account first.") || countBlocks(t, db, user.Email) != 0 || len(apitest.EventsNamed(t, db, "email.blocked")) != 0 {
		t.Fatalf("blocked a registered address: %s", w.Body.String())
	}
}

// Expired, unknown and malformed links are refused alike by all three
// routes, and the cleanup deletes expired links.
func TestDeadBlockLinks(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	expired := apitest.Must(factory.EmailBlockLinks.State(factory.ExpiredBlockLink).CreateOne(ctx, db, f))(t)
	unknown := apitest.Must(auth.Token())(t)
	for _, token := range []string{expired.Token, unknown, "short", ""} {
		for _, route := range []struct{ method, path string }{{"POST", "/api/email-blocks/check"}, {"POST", "/api/email-blocks"}, {"DELETE", "/api/email-blocks"}} {
			w := apitest.Call(t, h, route.method, route.path, apitest.TokenBody(token), "", nil, 422)
			if apitest.ErrorField(t, w) != "token" {
				t.Fatalf("%s %s %q: %s", route.method, route.path, token, w.Body.String())
			}
		}
	}
	if countBlocks(t, db, expired.Email) != 0 {
		t.Fatal("a dead link blocked its address")
	}
	if err := s.Repo.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if n := apitest.Must(db.NewSelect().TableExpr("email_block_links").Count(ctx))(t); n != 0 {
		t.Fatalf("%d expired links left", n)
	}
}

// The command line's store calls: unblocking by address records by "cli",
// an address that isn't blocked changes nothing, and the list is oldest
// first.
func TestUnblockingByAddress(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	f := factory.TestFaker(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("first@example.com")).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("second@example.com")).CreateOne(ctx, db, f))(t)
	list := apitest.Must(s.Repo.IndexEmailBlocks(ctx))(t)
	if len(list) != 2 || list[0].Email != "first@example.com" || list[1].Email != "second@example.com" {
		t.Fatalf("list: %+v", list)
	}
	if err := s.Repo.UnblockEmailAddress(ctx, "first@example.com", nil); err != nil {
		t.Fatal(err)
	}
	e := apitest.OnlyEvent(t, db, "email.unblocked")
	if e.Payload["email"] != "first@example.com" || e.Payload["by"] != "cli" || e.IP != "" || e.ActorID.Valid {
		t.Fatalf("email.unblocked: %+v", e)
	}
	if err := s.Repo.UnblockEmailAddress(ctx, "first@example.com", nil); !errors.Is(err, store.ErrEmailNotBlocked) {
		t.Fatalf("unblocking twice: %v", err)
	}
	if _, err := s.Repo.FindEmailBlock(ctx, "first@example.com"); !errors.Is(err, store.ErrEmailNotBlocked) {
		t.Fatalf("find: %v", err)
	}
	if len(apitest.EventsNamed(t, db, "email.unblocked")) != 1 {
		t.Fatal("unblocking an address that isn't blocked recorded an event")
	}
}

// Blocking and creating an account with the same address at once: exactly
// one wins, every time. Without the email lock both can pass their check.
func TestBlockRacingSignUp(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	f := factory.TestFaker(t)
	for round := range 5 {
		email := fmt.Sprintf("race%d@example.com", round)
		link := apitest.Must(factory.EmailBlockLinks.State(factory.BlockLinkFor(email)).CreateOne(ctx, db, f))(t)
		pending := apitest.Must(factory.PendingRegistrations.State(factory.RegistrationFor(email)).CreateOne(ctx, db, f))(t)
		create := func() error {
			if round%2 == 0 {
				_, err := s.Repo.CreateUser(ctx, "Ada", email, "hash", store.PolicyVersions{})
				return err
			}
			result, err := s.Repo.CompleteRegistration(ctx, auth.Digest(pending.Token), factory.Keys.EmailCodeDigest(pending.Code))
			if err == nil && result.Blocked {
				return store.ErrEmailBlocked
			}
			return err
		}
		var blockErr, createErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); blockErr = s.Repo.BlockEmail(ctx, link.TokenHash) }()
		go func() { defer wg.Done(); createErr = create() }()
		wg.Wait()
		blocked, registered := countBlocks(t, db, email), countUsers(t, db, email)
		if blocked+registered != 1 {
			t.Fatalf("round %d: %d blocks, %d accounts (block: %v, create: %v)", round, blocked, registered, blockErr, createErr)
		}
		if (blocked == 1) != errors.Is(createErr, store.ErrEmailBlocked) || (registered == 1) != errors.Is(blockErr, store.ErrEmailRegistered) {
			t.Fatalf("round %d: block %v, create %v", round, blockErr, createErr)
		}
	}
}

// The routes take auth:per-ip and the usual Origin and JSON checks.
func TestBlockLinkProtections(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	link := apitest.Must(factory.EmailBlockLinks.CreateOne(ctx, db, f))(t)

	r := httptest.NewRequest("POST", "/api/email-blocks", strings.NewReader(apitest.TokenBody(link.Token)))
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin block: %d", w.Code)
	}
	r = httptest.NewRequest("DELETE", "/api/email-blocks", strings.NewReader(apitest.TokenBody(link.Token)))
	r.Header.Set("Origin", apitest.Origin)
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatalf("unblock without JSON: %d", w.Code)
	}
	apitest.Call(t, h, "POST", "/api/email-blocks", `{"token":"x","email":"eve@example.com"}`, "", nil, 400)
	if countBlocks(t, db, link.Email) != 0 {
		t.Fatal("a refused request blocked the address")
	}

	apitest.Must(factory.RateLimits.State(factory.ForLimit("auth:per-ip", "127.0.0.1")).State(factory.Exhausted(60)).CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "POST", "/api/email-blocks/check", apitest.TokenBody(link.Token), "", nil, 429)
	apitest.Call(t, h, "POST", "/api/email-blocks", apitest.TokenBody(link.Token), "", nil, 429)
	apitest.Reached(t, db, "auth:per-ip:block")
	if countBlocks(t, db, link.Email) != 0 {
		t.Fatal("auth:per-ip didn't hold")
	}
}
